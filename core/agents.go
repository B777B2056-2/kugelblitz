package core

import "context"

// AgentEventHooks holds callbacks for agent-level events.
// ModelEventHandler is embedded for model response callbacks.
type AgentEventHooks struct {
	ModelEventHandler
	OnToolCallEnd        func(toolCallResult ToolCallResult)
	OnWaitForHumanAction func(reason string, prompt string)
	OnPlanRollback       func(planID string, targetVersion int, planName string)
	OnTaskUpdated        func(taskID string, goal string, status string, output string)
}

// LLMUsageReport is sent to the Planner's usage callback for every LLM call
// made during execution, regardless of source (main loop, compressor, reviewer, worker).
type LLMUsageReport struct {
	Identity string // "planner.step-1", "compressor", "reviewer", "worker.<taskID>"
	Usage    Usage
}

// IAgent is the interface all agents must implement.
type IAgent interface {
	RegisterEventHooks(hooks AgentEventHooks)
	Execute(ctx context.Context, systemMessage Message, userMessages []Message) (assistantMessages []Message, err error)
	Interrupt(ctx context.Context) error
	ResumeWithHumanResponse(ctx context.Context, response string) error
}
