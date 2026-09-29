package core

import (
	"context"
	"sync"

	"github.com/B777B2056-2/kugelblitz/constants"
	types "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
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
	OnFunctionCall  func(identity constants.AgentIdentity, detail types.ToolCallDetail)
	OnModelFinished func(identity constants.AgentIdentity, reason string)
	OnError         func(identity constants.AgentIdentity, err error)
	OnUsageUpdated  func(identity constants.AgentIdentity, usage types.Usage)

	// ── Agent-level hooks (receive AgentIdentity) ──

	OnToolCallEnd        func(identity constants.AgentIdentity, result types.ToolCallResult)
	OnWaitForHumanAction func(identity constants.AgentIdentity, reason string, prompt string)
	OnPlanRollback       func(identity constants.AgentIdentity, planID string, targetVersion int, planName string)
	OnTaskUpdated        func(identity constants.AgentIdentity, taskID string, goal string, status string, output string)
	OnBeforeCompress     func(identity constants.AgentIdentity)

	// unsub tracks this value's active subscription so repeated Subscribe calls
	// on the same value replace (rather than accumulate) the prior one. It is a
	// pointer so value copies of AgentEventHooks share a single guard.
	unsub *unsubState
}

// unsubState holds a hooks value's current unsubscribe function under a mutex,
// giving Subscribe overwrite semantics independent of the RegisterEventHooks
// callers that already guard against re-registration.
type unsubState struct {
	mu sync.Mutex
	fn func()
}

// Subscribe registers every non-nil callback on bus and returns a single
// unsubscribe function that removes all of them. Each callback's identity
// parameter is filled from the event's Identity field; the event's Instance
// field is dropped (consumers that need instance scoping should subscribe via
// events.On directly). This makes AgentEventHooks a thin declarative sugar over
// the bus, preserving the existing RegisterEventHooks(AgentEventHooks{...}) API.
func (h *AgentEventHooks) Subscribe(bus *events.Bus) func() {
	var unsubs []func()

	if h.OnThinkingChunk != nil {
		cb := h.OnThinkingChunk
		unsubs = append(unsubs, events.On(bus, func(ev events.ThinkingChunk) { cb(ev.Identity, ev.Chunk) }))
	}
	if h.OnReplyChunk != nil {
		cb := h.OnReplyChunk
		unsubs = append(unsubs, events.On(bus, func(ev events.ReplyChunk) { cb(ev.Identity, ev.Chunk) }))
	}
	if h.OnBlockThinking != nil {
		cb := h.OnBlockThinking
		unsubs = append(unsubs, events.On(bus, func(ev events.BlockThinking) { cb(ev.Identity, ev.Reasoning) }))
	}
	if h.OnBlockReply != nil {
		cb := h.OnBlockReply
		unsubs = append(unsubs, events.On(bus, func(ev events.BlockReply) { cb(ev.Identity, ev.Text) }))
	}
	if h.OnFunctionCall != nil {
		cb := h.OnFunctionCall
		unsubs = append(unsubs, events.On(bus, func(ev events.FunctionCall) { cb(ev.Identity, ev.Detail) }))
	}
	if h.OnModelFinished != nil {
		cb := h.OnModelFinished
		unsubs = append(unsubs, events.On(bus, func(ev events.ModelFinished) { cb(ev.Identity, ev.Reason) }))
	}
	if h.OnError != nil {
		cb := h.OnError
		unsubs = append(unsubs, events.On(bus, func(ev events.AgentError) { cb(ev.Identity, ev.Err) }))
	}
	if h.OnUsageUpdated != nil {
		cb := h.OnUsageUpdated
		unsubs = append(unsubs, events.On(bus, func(ev events.UsageUpdated) { cb(ev.Identity, ev.Usage) }))
	}
	if h.OnToolCallEnd != nil {
		cb := h.OnToolCallEnd
		unsubs = append(unsubs, events.On(bus, func(ev events.ToolCallEnd) { cb(ev.Identity, ev.Result) }))
	}
	if h.OnWaitForHumanAction != nil {
		cb := h.OnWaitForHumanAction
		unsubs = append(unsubs, events.On(bus, func(ev events.WaitForHumanAction) { cb(ev.Identity, ev.Reason, ev.Prompt) }))
	}
	if h.OnPlanRollback != nil {
		cb := h.OnPlanRollback
		unsubs = append(unsubs, events.On(bus, func(ev events.PlanRollback) { cb(ev.Identity, ev.PlanID, ev.TargetVersion, ev.PlanName) }))
	}
	if h.OnTaskUpdated != nil {
		cb := h.OnTaskUpdated
		unsubs = append(unsubs, events.On(bus, func(ev events.TaskUpdated) { cb(ev.Identity, ev.TaskID, ev.Goal, ev.Status, ev.Output) }))
	}
	if h.OnBeforeCompress != nil {
		cb := h.OnBeforeCompress
		unsubs = append(unsubs, events.On(bus, func(ev events.BeforeCompress) { cb(ev.Identity) }))
	}

	combined := func() {
		for _, u := range unsubs {
			u()
		}
	}

	// Overwrite semantics: a second Subscribe on the same value tears down the
	// previous subscription before installing the new one. This defends against
	// callers that call Subscribe directly (bypassing RegisterEventHooks) and
	// discard the returned unsub, which would otherwise accumulate duplicate
	// deliveries.
	if h.unsub == nil {
		h.unsub = &unsubState{}
	}
	h.unsub.mu.Lock()
	if h.unsub.fn != nil {
		h.unsub.fn()
	}
	h.unsub.fn = combined
	h.unsub.mu.Unlock()

	return combined
}

// IAgent is the interface all agents must implement.
type IAgent interface {
	RegisterEventHooks(hooks AgentEventHooks)
	Execute(ctx context.Context, systemMessage types.Message, userMessages []types.Message) (assistantMessages []types.Message, err error)
	Interrupt(ctx context.Context) error
	ResumeWithHumanResponse(ctx context.Context, response string) error
}

// NewBusModelHandler returns a ModelEventHandler that emits model events onto
// bus, tagged with the given agent identity and instance. It is the emitter-side
// counterpart to AgentEventHooks.Subscribe: providers push into this handler and
// the events fan out to every bus subscriber.
func NewBusModelHandler(bus *events.Bus, identity constants.AgentIdentity, instance string) types.ModelEventHandler {
	return &busModelHandler{bus: bus, identity: identity, instance: instance}
}

// busModelHandler adapts a Bus into a types.ModelEventHandler by emitting one
// typed event per model callback.
type busModelHandler struct {
	bus      *events.Bus
	identity constants.AgentIdentity
	instance string
}

func (b *busModelHandler) OnThinkingChunk(chunk string) {
	events.Emit(b.bus, events.ThinkingChunk{Identity: b.identity, Instance: b.instance, Chunk: chunk})
}
func (b *busModelHandler) OnReplyChunk(chunk string) {
	events.Emit(b.bus, events.ReplyChunk{Identity: b.identity, Instance: b.instance, Chunk: chunk})
}
func (b *busModelHandler) OnBlockThinking(reasoning string) {
	events.Emit(b.bus, events.BlockThinking{Identity: b.identity, Instance: b.instance, Reasoning: reasoning})
}
func (b *busModelHandler) OnBlockReply(text string) {
	events.Emit(b.bus, events.BlockReply{Identity: b.identity, Instance: b.instance, Text: text})
}
func (b *busModelHandler) OnFunctionCall(detail types.ToolCallDetail) {
	events.Emit(b.bus, events.FunctionCall{Identity: b.identity, Instance: b.instance, Detail: detail})
}
func (b *busModelHandler) OnFinished(reason string) {
	events.Emit(b.bus, events.ModelFinished{Identity: b.identity, Instance: b.instance, Reason: reason})
}
func (b *busModelHandler) OnUsageUpdated(usage types.Usage) {
	events.Emit(b.bus, events.UsageUpdated{Identity: b.identity, Instance: b.instance, Usage: usage})
}
func (b *busModelHandler) OnError(err error) {
	events.Emit(b.bus, events.AgentError{Identity: b.identity, Instance: b.instance, Err: err})
}
