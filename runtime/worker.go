package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/runtime/prompts"
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
	provider    core.ILMProvider
	streamMode  bool
	customTools []string
	maxSteps    int                   // safety limit on ReAct loop iterations
	hooks       core.AgentEventHooks  // set by DAG executor; relayed to worker's ReactAgent
	pauseGate   *sync.RWMutex        // shared DAG pause gate; nil = no pausing
	onHITL      func(agent *ReactAgent, reason, prompt string) // fire on worker HITL
}

// NewWorkerAgent creates a WorkerAgent with built-in execution tools plus custom tools.
func NewWorkerAgent(provider core.ILMProvider, streamMode bool, customTools ...string) *WorkerAgent {
	return &WorkerAgent{
		provider:    provider,
		streamMode:  streamMode,
		customTools: customTools,
		maxSteps:    10,
	}
}

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
	systemMsg := core.NewSystemMessage("root", core.TextContent{Text: sysPrompt})

	userMsg := core.NewUserMessage("root", core.TextContent{
		Text: fmt.Sprintf("Execute the task: %s", goal),
	})

	handler := &workerEventHandler{result: result}

	agent := NewReactAgent(w.provider, w.streamMode)
	agent.WithTools(append(workerTools, w.customTools...)...)
	agent.EnableHumanInTheLoop()
	if w.pauseGate != nil {
		agent.WithPauseGate(w.pauseGate)
	}

	// Compose hooks: start from parent, overlay local handler, OnToolCallEnd, OnWaitForHumanAction.
	hooks := w.hooks
	hooks.ModelEventHandler = handler
	prevOnToolCallEnd := hooks.OnToolCallEnd
	hooks.OnToolCallEnd = func(r core.ToolCallResult) {
		if errMsg, ok := r.Outputs["error"]; ok {
			result.write(fmt.Sprintf("[tool error: %s → %v]", r.ToolName, errMsg))
		}
		if prevOnToolCallEnd != nil {
			prevOnToolCallEnd(r)
		}
	}
	prevOnWaitForHuman := hooks.OnWaitForHumanAction
	hooks.OnWaitForHumanAction = func(reason, prompt string) {
		if w.onHITL != nil {
			w.onHITL(agent, reason, prompt)
		}
		if prevOnWaitForHuman != nil {
			prevOnWaitForHuman(reason, prompt)
		}
	}
	agent.RegisterEventHooks(hooks)

	go func() {
		<-ctx.Done()
		agent.Interrupt(context.Background())
	}()

	messages, err := agent.Execute(ctx, systemMsg, []core.Message{userMsg})
	if err != nil {
		result.setErr(err)
	}

	for _, msg := range messages {
		if tc, ok := msg.Content.(core.TextContent); ok {
			result.write(tc.Text)
		}
		if msg.Usage != nil {
			result.addUsage(*msg.Usage)
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

// workerEventHandler collects output from streaming chunks.
type workerEventHandler struct {
	result *workerResult
}

func (h *workerEventHandler) OnThinkingChunk(chunk string)              {}
func (h *workerEventHandler) OnReplyChunk(chunk string)                 { h.result.write(chunk) }
func (h *workerEventHandler) OnFunctionCall(detail core.ToolCallDetail) {}
func (h *workerEventHandler) OnFinished(reason string)                  {}
func (h *workerEventHandler) OnUsageUpdated(usage core.Usage)           { h.result.addUsage(usage) }
func (h *workerEventHandler) OnError(err error)                         { h.result.setErr(err) }
