package core

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/constants"
)

// AgentEventHooks holds callbacks for agent-level events. Every callback
// receives an AgentIdentity as the first argument so consumers can distinguish
// which agent produced the event.
type AgentEventHooks struct {
	// ── Model event callbacks (receive AgentIdentity) ──

	OnThinkingChunk func(identity constants.AgentIdentity, chunk string)
	OnReplyChunk    func(identity constants.AgentIdentity, chunk string)
	OnBlockThinking func(identity constants.AgentIdentity, reasoning string)
	OnBlockReply    func(identity constants.AgentIdentity, text string)
	OnFunctionCall  func(identity constants.AgentIdentity, detail ToolCallDetail)
	OnModelFinished func(identity constants.AgentIdentity, reason string)
	OnError         func(identity constants.AgentIdentity, err error)
	OnUsageUpdated  func(identity constants.AgentIdentity, usage Usage)

	// ── Agent-level hooks (receive AgentIdentity) ──

	OnToolCallEnd        func(identity constants.AgentIdentity, result ToolCallResult)
	OnWaitForHumanAction func(identity constants.AgentIdentity, reason string, prompt string)
	OnPlanRollback       func(identity constants.AgentIdentity, planID string, targetVersion int, planName string)
	OnTaskUpdated        func(identity constants.AgentIdentity, taskID string, goal string, status string, output string)
	OnBeforeCompress     func(identity constants.AgentIdentity)
}

// IAgent is the interface all agents must implement.
type IAgent interface {
	RegisterEventHooks(hooks AgentEventHooks)
	Execute(ctx context.Context, systemMessage Message, userMessages []Message) (assistantMessages []Message, err error)
	Interrupt(ctx context.Context) error
	ResumeWithHumanResponse(ctx context.Context, response string) error
}
