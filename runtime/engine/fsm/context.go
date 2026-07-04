package fsm

import (
	"context"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/dag"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
)

// Context holds the per-run mutable state shared across all states during
// a single Machine.Run invocation.
type Context struct {
	Ctx      context.Context
	Goal     string
	Results  []core.Message

	Plan     *working.Plan
	PlanID   string
	WorkMode string

	StepCount int
	TaskFails int

	Deps Dependencies
}

// Dependencies holds all concrete external dependencies injected into the
// state machine. Uses concrete types (no interfaces) because package
// restructuring eliminates circular dependencies.
type Dependencies struct {
	React       *infra.ReactAgent
	DAG         *dag.DAGTaskExecutor
	Reviewer    *infra.Reviewer
	Session     *memory.SessionMemory
	Compressor  *memory.Compressor
	Config      MachineConfig
	HandleDrift func(ctx *Context, reason string) // set by Machine
}

// MachineConfig is the subset of config.Config needed by the FSM.
type MachineConfig struct {
	MaxCycles               int
	CompressMaxAttempts     int
	ReviewInterval          int
	MaxFailuresBeforeReview int
}
