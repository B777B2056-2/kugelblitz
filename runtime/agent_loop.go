package runtime

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/core"
)

// HITLAgent is the interface for an agent that supports human-in-the-loop.
type HITLAgent interface {
	HumanLoopWaiting() bool
	ResumeWithHumanResponse(ctx context.Context, response string) error
}

// AgentLoop holds provider and hooks, ready to Run() for multiple goals/sessions.
type AgentLoop struct {
	provider   core.ILMProvider
	hooks      core.AgentEventHooks
	streamMode bool

	plannerOpts     []PlannerOption
	onLLMUsage      func(core.LLMUsageReport)
	enableThinking  *bool
	reasoningEffort string

	// Cached planner for session continuity across Run() calls.
	planner *Planner

	hitlAgent HITLAgent
	done      chan struct{}
	cancelFn  context.CancelFunc
}

// AgentLoopOption configures an AgentLoop at creation time.
type AgentLoopOption func(*AgentLoop)

// WithThinking enables thinking mode with the given effort level.
func WithThinking(enabled bool, effort string) AgentLoopOption {
	return func(a *AgentLoop) {
		a.enableThinking = &enabled
		a.reasoningEffort = effort
	}
}

// WithStreamMode enables streaming for provider calls.
func WithStreamMode(v bool) AgentLoopOption {
	return func(a *AgentLoop) { a.streamMode = v }
}

// WithUsageCallback registers a callback for every LLM call during execution.
func WithUsageCallback(fn func(core.LLMUsageReport)) AgentLoopOption {
	return func(a *AgentLoop) { a.onLLMUsage = fn }
}

// WithCachedPlanner reuses an existing Planner (preserves LTM/graph/pipeline).
func WithCachedPlanner(p *Planner) AgentLoopOption {
	return func(a *AgentLoop) { a.planner = p }
}

// CachedPlanner returns the cached Planner (may be nil).
func (a *AgentLoop) CachedPlanner() *Planner { return a.planner }

// NewAgentLoop creates an AgentLoop instance.
func NewAgentLoop(provider core.ILMProvider, hooks core.AgentEventHooks,
	opts ...AgentLoopOption) *AgentLoop {
	a := &AgentLoop{
		provider:   provider,
		hooks:      hooks,
		streamMode: true,
	}
	for _, opt := range opts {
		opt(a)
	}
	if a.onLLMUsage != nil {
		fn := a.onLLMUsage
		a.plannerOpts = append(a.plannerOpts, func(p *Planner) { p.onLLMUsage = fn })
	}
	return a
}

// Resume unblocks a pending HITL with the user's response.
func (a *AgentLoop) Resume(response string) error {
	if a.hitlAgent == nil {
		return fmt.Errorf("agent loop: not running")
	}
	return a.hitlAgent.ResumeWithHumanResponse(context.Background(), response)
}

// Cancel interrupts the running execution.
func (a *AgentLoop) Cancel() {
	if a.cancelFn != nil {
		a.cancelFn()
	}
}

// Done returns a channel that closes when the current execution completes.
func (a *AgentLoop) Done() <-chan struct{} { return a.done }

// WaitDone blocks until the current execution completes.
func (a *AgentLoop) WaitDone() { <-a.done }

// Run starts a full agent loop for the given goal and session.
func (a *AgentLoop) Run(ctx context.Context, goal string, sessionID string) {
	ctx, a.cancelFn = context.WithCancel(ctx)
	a.done = make(chan struct{})

	go func() {
		defer close(a.done)
		defer a.Cancel()
		a.execute(ctx, goal, sessionID)
	}()
}

// execute runs the agent pipeline. Intent recognition is handled by the
// PlannerStateMachine (Intent → Init or Direct).
func (a *AgentLoop) execute(ctx context.Context, goal string, sessionID string) {
	if a.planner == nil {
		a.planner = NewPlanner(a.provider, a.streamMode,
			append([]PlannerOption{WithExistingSessionID(sessionID)}, a.plannerOpts...)...)
	}

	userMsg := core.NewUserMessage("agent-loop", core.TextContent{Text: goal})
	a.planner.mem.AppendMessage(userMsg)
	_ = a.planner.mem.Persist()
	a.planner.RegisterEventHooks(a.hooks)
	if a.enableThinking != nil {
		a.planner.SetThinking(*a.enableThinking, a.reasoningEffort)
	}
	a.hitlAgent = a.planner
	_, err := a.planner.Execute(ctx, goal)

	if err != nil && a.hooks.ModelEventHandler != nil {
		a.hooks.ModelEventHandler.OnError(err)
	}
}
