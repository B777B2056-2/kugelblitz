package engine

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/dag"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/fsm"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
	"go.opentelemetry.io/otel"
)

// Kernel is the public entry point for the agent runtime. It assembles all
// dependencies and delegates plan lifecycle orchestration to fsm.Machine.
type Kernel struct {
	machine    *fsm.Machine
	mainReact  *infra.ReactAgent
	dagExec    *dag.DAGTaskExecutor
	sessionMem *memory.SessionMemory
	compressor *memory.Compressor
	reviewer   *infra.Reviewer
	cfg        config.Config
}

// NewKernel creates a Kernel with all dependencies wired up.
func NewKernel(
	sessionMem *memory.SessionMemory,
	cfg config.Config,
) *Kernel {
	if sessionMem == nil {
		panic(fmt.Errorf("nil session memory"))
	}

	mainReact := infra.NewReactAgent(cfg.Model.Provider, cfg.Model.StreamMode)
	if cfg.Model.EnableThinking {
		mainReact.SetThinking(true, cfg.Model.ReasoningEffort)
	}
	mainReact.EnableHumanInTheLoop()

	tracer := otel.Tracer("kugelblitz")
	compressor := memory.NewCompressor(cfg.Model.Provider, tracer)
	dagExec := dag.NewDAGTaskExecutor(cfg.Model.Provider, cfg.Model.StreamMode)
	reviewer := infra.NewReviewer(cfg.Model.Provider, tracer)

	machine := fsm.NewMachine(fsm.Dependencies{
		React:      mainReact,
		DAG:        dagExec,
		Reviewer:   reviewer,
		Session:    sessionMem,
		Compressor: compressor,
		Config: fsm.MachineConfig{
			MaxCycles:               cfg.Runtime.MaxStateMachineCycles,
			CompressMaxAttempts:     cfg.ContextCompress.MaxAttempts,
			ReviewInterval:          cfg.TargetDrift.ReviewInterval,
			MaxFailuresBeforeReview: cfg.TargetDrift.MaxFailuresBeforeReview,
		},
	})

	return &Kernel{
		machine:    machine,
		mainReact:  mainReact,
		dagExec:    dagExec,
		sessionMem: sessionMem,
		compressor: compressor,
		reviewer:   reviewer,
		cfg:        cfg,
	}
}

// RegisterEventHooks forwards hooks to all sub-agents.
func (sm *Kernel) RegisterEventHooks(hooks core.AgentEventHooks) {
	sm.mainReact.SetAgentIdentity(constants.AgentMain)
	sm.mainReact.RegisterEventHooks(hooks)

	sm.dagExec.SetWorkerHooks(hooks)
	sm.reviewer.SetHooks(hooks)
}

// Compressor returns the session memory compressor.
func (sm *Kernel) Compressor() *memory.Compressor {
	return sm.compressor
}

// Run executes the state machine main loop.
func (sm *Kernel) Run(ctx context.Context, input core.AgentInput) ([]core.Message, error) {
	return sm.machine.Run(ctx, input)
}

// SetStepTracer propagates the shared StepTracer to the main ReAct agent and DAG workers.
func (sm *Kernel) SetStepTracer(st *observability.StepTracer) {
	sm.mainReact.SetStepTracer(st)
	sm.dagExec.SetStepTracer(st)
}

// SetProvider replaces the LLM provider for the main ReAct loop, DAG workers,
// and reviewer. Call before Run() to dynamically switch models per input type.
func (sm *Kernel) SetProvider(p core.ILMProvider) {
	sm.mainReact.SetProvider(p)
	sm.dagExec.SetProvider(p)
	sm.reviewer.SetProvider(p)
}

// Cancel stops the main ReAct loop, all workers, and marks the current plan as cancelled.
func (sm *Kernel) Cancel(ctx context.Context) {
	_ = sm.mainReact.Interrupt(ctx)
	sm.dagExec.Cancel()
	// The machine's current plan is managed internally; we can't access planID
	// directly anymore. The plan will be marked as failed when Run returns.
}

// HumanLoopWaiting reports whether mainReact or any DAG worker is waiting for human input.
func (sm *Kernel) HumanLoopWaiting() bool {
	return sm.mainReact.HumanLoopWaiting() || sm.dagExec.AnyWorkerInHumanLoopWaiting()
}

// Agent returns the underlying IAgent for external consumers (e.g. ACP server).
func (sm *Kernel) Agent() core.IAgent { return sm.mainReact }

// ResumeWithHumanResponse delivers a human response to whichever agent is waiting
// (mainReact first, then DAG workers).
func (sm *Kernel) ResumeWithHumanResponse(ctx context.Context, response string) error {
	if sm.mainReact.HumanLoopWaiting() {
		return sm.mainReact.ResumeWithHumanResponse(ctx, response)
	}
	return sm.dagExec.ResumeAnyWorkerWithHumanResponse(ctx, response)
}
