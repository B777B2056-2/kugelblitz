package runtime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"kugelblitz/core"
)

// WorkerAgent is a lightweight agent that executes a single task with a
// restricted tool set. It is spawned by a Planner via the worker_spawn tool.
//
// The Worker runs a ReAct loop internally:
//
//	think → tool_call → observe → think → ... → done
//
// It returns the final result to the caller.
type WorkerAgent struct {
	provider   core.ILMProvider
	streamMode bool
	toolNames  []string
	maxSteps   int // safety limit on ReAct loop iterations
}

// NewWorkerAgent creates a WorkerAgent.
// toolNames is the whitelist of tool names the worker can use.
func NewWorkerAgent(provider core.ILMProvider, streamMode bool, toolNames []string) *WorkerAgent {
	return &WorkerAgent{
		provider:   provider,
		streamMode: streamMode,
		toolNames:  toolNames,
		maxSteps:   10,
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
// goal describes what to achieve; action gives a concrete instruction.
// Returns (output text, accumulated token usage, error).
func (w *WorkerAgent) ExecuteTask(ctx context.Context, goal, action string) (string, *core.Usage, error) {
	result := &workerResult{}

	// Build system prompt from goal + action
	systemPrompt := fmt.Sprintf(
		"You are a task executor. Complete the following task.\n\nGoal: %s\nAction: %s\n\nUse available tools as needed. When done, report the result clearly.",
		goal, action,
	)
	systemMsg := core.NewUserMessage("root", core.TextContent{Text: systemPrompt})
	systemMsg.Role = "system"

	// User message: kick off the task
	userMsg := core.NewUserMessage("root", core.TextContent{
		Text: fmt.Sprintf("Execute the task: %s", goal),
	})

	// Build the handler that collects output
	handler := &workerEventHandler{result: result}

	// Create a temporary ReactAgent for this task
	agent := NewReactAgent(w.provider, w.streamMode)
	agent.WithTools(w.toolNames...)
	agent.RegisterEventHooks(core.AgentEventHooks{
		ModelEventHandler: handler,
		OnToolCallEnd: func(r core.ToolCallResult) {
			// Collect tool call errors
			if errMsg, ok := r.Outputs["error"]; ok {
				result.write(fmt.Sprintf("[tool error: %s → %v]", r.ToolName, errMsg))
			}
		},
	})

	messages, err := agent.Execute(ctx, systemMsg, []core.Message{userMsg})
	if err != nil {
		result.setErr(err)
	}

	// Extract text + usage from the final assistant messages
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

func (h *workerEventHandler) OnThinkingChunk(chunk string)  {}
func (h *workerEventHandler) OnReplyChunk(chunk string)     { h.result.write(chunk) }
func (h *workerEventHandler) OnFunctionCall(detail core.ToolCallDetail) {}
func (h *workerEventHandler) OnFinished(reason string)      {}
func (h *workerEventHandler) OnUsageUpdated(usage core.Usage) { h.result.addUsage(usage) }
func (h *workerEventHandler) OnError(err error)             { h.result.setErr(err) }
