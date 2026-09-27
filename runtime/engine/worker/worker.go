// Package worker defines the execution contract the DAG orchestrator needs from
// a task-execution layer. It is a leaf package (imports only core, core/types,
// observability and tools) so the dag package can depend on it without reaching
// into the concrete infra implementations.
package worker

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/observability"
	"github.com/B777B2056-2/kugelblitz/tools"
)

// PauseGate coordinates pausing across a group of workers. Pause blocks
// subsequent WaitIfPaused callers until Resume is called.
type PauseGate interface {
	Pause()
	Resume()
	WaitIfPaused()
}

// HitlAgent is the minimal surface of an agent waiting for human input. The DAG
// executor stores these to detect and resume HITL waits without knowing the
// concrete agent type.
type HitlAgent interface {
	HumanLoopWaiting() bool
	ResumeWithHumanResponse(ctx context.Context, response string) error
}

// HumanToolFactory builds the local ask_human tool for a specific agent gate.
// Injected by the composition root so the execution layer stays decoupled from
// the concrete tool implementation in tools/internals.
type HumanToolFactory func(gate core.HumanGate) tools.Tool

// Worker is the minimal surface of a task-execution unit the DAG orchestrator
// needs: configure it, then run a single task.
type Worker interface {
	SetHooks(hooks core.AgentEventHooks)
	SetStepTracer(st *observability.StepTracer)
	SetPauseGate(g PauseGate)
	SetHumanToolFactory(f HumanToolFactory)
	SetOnHITL(fn func(agent HitlAgent, reason, prompt string))
	ExecuteTask(ctx context.Context, goal, action string) (string, *coretypes.Usage, error)
}

// WorkerFactory creates a Worker for a single task. Injected by the composition
// root so the DAG owns construction wiring without hardcoding a concrete
// implementation.
type WorkerFactory func(provider coretypes.ILMProvider, streamMode bool) Worker
