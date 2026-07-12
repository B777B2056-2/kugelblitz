package infra

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// WorkerAgent is a lightweight agent that executes a single task with a
// restricted tool set. It is spawned by a Planner via the worker_spawn tool.
//
// The Worker runs a ReAct loop internally:
//
//	think → tool_call → observe → think → ... → done
//
// It returns the final result to the caller.
// workerTools are the execution tools available to every WorkerAgent.
var workerTools = []string{
	"shell_exec",
	"web_fetch", "web_search",
	"file_read", "file_write", "file_copy", "file_delete",
	"dir_create", "dir_copy",
	"skill_use",
	"task_status_update",
	"ask_human",
}

type WorkerAgent struct {
	provider   core.ILMProvider
	streamMode bool
	maxSteps   int                                            // safety limit on ReAct loop iterations
	hooks      core.AgentEventHooks                           // set by DAG executor; relayed to worker's ReactAgent
	pauseGate  *sync.RWMutex                                  // shared DAG pause gate; nil = no pausing
	onHITL     func(agent *ReactAgent, reason, prompt string) // fire on worker HITL
	stepTracer *observability.StepTracer                      // per-step OTel instrumentation (shared from DAG)
}

// NewWorkerAgent creates a WorkerAgent with built-in execution tools plus custom tools.
func NewWorkerAgent(provider core.ILMProvider, streamMode bool) *WorkerAgent {
	return &WorkerAgent{
		provider:   provider,
		streamMode: streamMode,
		maxSteps:   10,
	}
}

// SetHooks sets the event hooks relayed to the worker's ReactAgent.
func (w *WorkerAgent) SetHooks(hooks core.AgentEventHooks) { w.hooks = hooks }

// SetPauseGate sets the shared DAG pause gate.
func (w *WorkerAgent) SetPauseGate(g *sync.RWMutex) { w.pauseGate = g }

// SetProvider replaces the LLM provider used for subsequent task execution.
func (w *WorkerAgent) SetProvider(p core.ILMProvider) { w.provider = p }

// SetStepTracer attaches a StepTracer for per-step OTel instrumentation.
func (w *WorkerAgent) SetStepTracer(st *observability.StepTracer) { w.stepTracer = st }

// SetOnHITL sets the callback fired when the worker enters HITL.
func (w *WorkerAgent) SetOnHITL(fn func(agent *ReactAgent, reason, prompt string)) { w.onHITL = fn }

// workerResult collects the WorkerAgent's output and usage safely from callbacks.
type workerResult struct {
	mu     sync.Mutex
	output strings.Builder
	usage  core.Usage
	err    error
}

func (r *workerResult) write(chunk string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.output.WriteString(chunk)
}

func (r *workerResult) addUsage(u core.Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.InputTokens += u.InputTokens
	r.usage.OutputTokens += u.OutputTokens
	r.usage.ReasoningTokens += u.ReasoningTokens
	r.usage.CachedTokens += u.CachedTokens
	r.usage.TotalTokens += u.TotalTokens
}

func (r *workerResult) setErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

// ExecuteTask runs the worker to complete a single task.
func (w *WorkerAgent) ExecuteTask(ctx context.Context, goal, action string) (string, *core.Usage, error) {
	result := &workerResult{}

	sysPrompt := prompts.DefaultFactory.MustRender(prompts.TypeWorker, prompts.WorkerParams{
		Goal: goal, Action: action,
	})
	systemMsg := core.NewSystemMessage(core.TextContent{Text: sysPrompt})

	userMsg := core.NewUserMessage(core.TextContent{
		Text: fmt.Sprintf("Execute the task: %s", goal),
	})

	agent := NewReactAgent(w.provider, w.streamMode)

	// Wire per-step OTel tracing for this task
	if w.stepTracer != nil {
		agent.SetStepTracer(w.stepTracer)
	}

	agent.WithTools(append(workerTools, core.GetToolRegistry().CustomToolNames()...)...)
	agent.EnableHumanInTheLoop()
	if w.pauseGate != nil {
		agent.WithPauseGate(w.pauseGate)
	}

	// Compose hooks: Chain preserves user callbacks.
	hooks := core.Chain(w.hooks, core.AgentEventHooks{
		OnReplyChunk: func(id constants.AgentIdentity, chunk string) {
			result.write(chunk)
		},
		OnBlockReply: func(id constants.AgentIdentity, text string) {
			result.write(text)
		},
		OnUsageUpdated: func(id constants.AgentIdentity, usage core.Usage) {
			result.addUsage(usage)
		},
		OnError: func(id constants.AgentIdentity, err error) {
			result.setErr(err)
		},
		OnToolCallEnd: func(id constants.AgentIdentity, r core.ToolCallResult) {
			if errMsg, ok := r.Outputs["error"]; ok {
				result.write(fmt.Sprintf("[tool error: %s → %v]", r.ToolName, errMsg))
			}
		},
		OnWaitForHumanAction: func(id constants.AgentIdentity, reason, prompt string) {
			if w.onHITL != nil {
				w.onHITL(agent, reason, prompt)
			}
		},
	})
	agent.SetAgentIdentity(constants.AgentWorker)
	agent.RegisterEventHooks(hooks)

	go func() {
		<-ctx.Done()
		_ = agent.Interrupt(context.Background())
	}()

	messages, err := agent.Execute(ctx, systemMsg, []core.Message{userMsg})
	if err != nil {
		result.setErr(err)
	}

	for _, msg := range messages {
		if tc, ok := msg.Content.(core.TextContent); ok {
			result.write(tc.Text)
		}
	}

	usage := &core.Usage{
		InputTokens:     result.usage.InputTokens,
		OutputTokens:    result.usage.OutputTokens,
		ReasoningTokens: result.usage.ReasoningTokens,
		CachedTokens:    result.usage.CachedTokens,
		TotalTokens:     result.usage.TotalTokens,
	}
	if result.err != nil {
		return result.output.String(), usage, result.err
	}
	return result.output.String(), usage, nil
}
