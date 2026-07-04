package engine

import (
	"context"
	"fmt"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/dag"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/fsm"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
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
	mainReact.RegisterEventHooks(cfg.Hooks)
	if cfg.Model.EnableThinking {
		mainReact.SetThinking(true, cfg.Model.ReasoningEffort)
	}
	mainReact.EnableHumanInTheLoop()

	compressor := memory.NewCompressor(cfg.Model.Provider)
	dagExec := dag.NewDAGTaskExecutor(cfg.Model.Provider, cfg.Model.StreamMode)
	reviewer := infra.NewReviewer(cfg.Model.Provider)

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

// RegisterEventHooks forwards hooks to mainReact and DAG.
func (sm *Kernel) RegisterEventHooks(hooks core.AgentEventHooks) {
	sm.cfg.Hooks = hooks
	sm.mainReact.RegisterEventHooks(hooks)
	sm.dagExec.Hooks = hooks
}

// Compressor returns the session memory compressor.
func (sm *Kernel) Compressor() *memory.Compressor {
	return sm.compressor
}

// Run executes the state machine main loop.
func (sm *Kernel) Run(ctx context.Context, goal string) ([]core.Message, error) {
	return sm.machine.Run(ctx, goal)
}

// Cancel stops the main ReAct loop, all workers, and marks the current plan as cancelled.
func (sm *Kernel) Cancel(ctx context.Context) {
	sm.mainReact.Interrupt(ctx)
	sm.dagExec.Cancel()
	// The machine's current plan is managed internally; we can't access planID
	// directly anymore. The plan will be marked as failed when Run returns.
}

// HumanLoopWaiting reports whether mainReact or any DAG worker is waiting for human input.
func (sm *Kernel) HumanLoopWaiting() bool {
	return sm.mainReact.HumanLoopWaiting() || sm.dagExec.AnyWorkerInHumanLoopWaiting()
}

// ResumeWithHumanResponse delivers a human response to whichever agent is waiting
// (mainReact first, then DAG workers).
func (sm *Kernel) ResumeWithHumanResponse(ctx context.Context, response string) error {
	if sm.mainReact.HumanLoopWaiting() {
		return sm.mainReact.ResumeWithHumanResponse(ctx, response)
	}
	return sm.dagExec.ResumeAnyWorkerWithHumanResponse(ctx, response)
}

// Ensure unused imports are kept for backward compatibility.
var _ = working.PutPlan
