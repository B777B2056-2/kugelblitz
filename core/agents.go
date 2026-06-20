package core

import "context"

// AgentEventHooks holds callbacks for agent-level events.
// ModelEventHandler is embedded for model response callbacks.
type AgentEventHooks struct {
	ModelEventHandler
	OnToolCallEnd func(toolCallResult ToolCallResult)
}

// IAgent is the interface all agents must implement.
type IAgent interface {
	RegisterEventHooks(hooks AgentEventHooks)
	Execute(ctx context.Context, systemMessage Message, userMessages []Message) (assistantMessages []Message, err error)
	Interrupt(ctx context.Context) error
}
