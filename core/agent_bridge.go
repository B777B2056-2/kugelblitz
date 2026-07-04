package core

import "github.com/B777B2056-2/kugelblitz/constants"

// AgentEventBridge implements ModelEventHandler by delegating each method to
// the corresponding AgentEventHooks callback with a fixed AgentIdentity.
type AgentEventBridge struct {
	Hooks    *AgentEventHooks
	Identity constants.AgentIdentity
}

// NewAgentEventBridge creates a bridge that dispatches model events to hooks
// with the given identity.
func NewAgentEventBridge(hooks *AgentEventHooks, id constants.AgentIdentity) *AgentEventBridge {
	return &AgentEventBridge{Hooks: hooks, Identity: id}
}

func (b *AgentEventBridge) OnThinkingChunk(chunk string) {
	if b.Hooks.OnThinkingChunk != nil {
		b.Hooks.OnThinkingChunk(b.Identity, chunk)
	}
}

func (b *AgentEventBridge) OnReplyChunk(chunk string) {
	if b.Hooks.OnReplyChunk != nil {
		b.Hooks.OnReplyChunk(b.Identity, chunk)
	}
}

func (b *AgentEventBridge) OnBlockThinking(reasoning string) {
	if b.Hooks.OnBlockThinking != nil {
		b.Hooks.OnBlockThinking(b.Identity, reasoning)
	}
}

func (b *AgentEventBridge) OnBlockReply(text string) {
	if b.Hooks.OnBlockReply != nil {
		b.Hooks.OnBlockReply(b.Identity, text)
	}
}

func (b *AgentEventBridge) OnFunctionCall(detail ToolCallDetail) {
	if b.Hooks.OnFunctionCall != nil {
		b.Hooks.OnFunctionCall(b.Identity, detail)
	}
}

func (b *AgentEventBridge) OnFinished(reason string) {
	if b.Hooks.OnModelFinished != nil {
		b.Hooks.OnModelFinished(b.Identity, reason)
	}
}

func (b *AgentEventBridge) OnUsageUpdated(usage Usage) {
	if b.Hooks.OnUsageUpdated != nil {
		b.Hooks.OnUsageUpdated(b.Identity, usage)
	}
}

func (b *AgentEventBridge) OnError(err error) {
	if b.Hooks.OnError != nil {
		b.Hooks.OnError(b.Identity, err)
	}
}
