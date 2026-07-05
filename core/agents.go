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

// AsModelEventHandler returns a ModelEventHandler that delegates each callback
// to this AgentEventHooks with the given AgentIdentity.
func (h *AgentEventHooks) AsModelEventHandler(id constants.AgentIdentity) ModelEventHandler {
	return &agentEventBridge{hooks: h, identity: id}
}

// agentEventBridge adapts AgentEventHooks to the ModelEventHandler interface.
type agentEventBridge struct {
	hooks    *AgentEventHooks
	identity constants.AgentIdentity
}

func (b *agentEventBridge) OnThinkingChunk(chunk string) {
	if b.hooks.OnThinkingChunk != nil {
		b.hooks.OnThinkingChunk(b.identity, chunk)
	}
}
func (b *agentEventBridge) OnReplyChunk(chunk string) {
	if b.hooks.OnReplyChunk != nil {
		b.hooks.OnReplyChunk(b.identity, chunk)
	}
}
func (b *agentEventBridge) OnBlockThinking(reasoning string) {
	if b.hooks.OnBlockThinking != nil {
		b.hooks.OnBlockThinking(b.identity, reasoning)
	}
}
func (b *agentEventBridge) OnBlockReply(text string) {
	if b.hooks.OnBlockReply != nil {
		b.hooks.OnBlockReply(b.identity, text)
	}
}
func (b *agentEventBridge) OnFunctionCall(detail ToolCallDetail) {
	if b.hooks.OnFunctionCall != nil {
		b.hooks.OnFunctionCall(b.identity, detail)
	}
}
func (b *agentEventBridge) OnFinished(reason string) {
	if b.hooks.OnModelFinished != nil {
		b.hooks.OnModelFinished(b.identity, reason)
	}
}
func (b *agentEventBridge) OnUsageUpdated(usage Usage) {
	if b.hooks.OnUsageUpdated != nil {
		b.hooks.OnUsageUpdated(b.identity, usage)
	}
}
func (b *agentEventBridge) OnError(err error) {
	if b.hooks.OnError != nil {
		b.hooks.OnError(b.identity, err)
	}
}
