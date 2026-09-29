package fsm

import (
	"context"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/prompts"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/types"
)

// Context holds the per-run mutable state shared across all states during
// a single Machine.Run invocation.
type Context struct {
	Ctx     context.Context
	Input   coretypes.AgentInput
	Results []coretypes.Message

	Plan     *working.Plan
	PlanID   string
	WorkMode string

	StepCount int
	TaskFails int

	Deps Dependencies
}

// ReactExecutor is the minimal surface of a ReAct agent the FSM needs. It is an
// interface (not *infra.ReactAgent) so the FSM no longer depends on the infra
// package and can be driven by test doubles.
type ReactExecutor interface {
	ExecuteWithTools(ctx context.Context, systemMessage coretypes.Message, userMessages []coretypes.Message, tools []string) ([]coretypes.Message, error)
}

// DAGExecutor is the minimal surface of the DAG executor the FSM needs.
type DAGExecutor interface {
	ExecuteBatch(ctx context.Context, plan *working.Plan, onTaskFailed func(taskID, goal, reason string)) types.BatchResult
	Cancel()
}

// DriftReviewer is the minimal surface of the drift reviewer the FSM needs.
type DriftReviewer interface {
	Review(ctx context.Context, originalGoal, planSummary, recentActivity string) types.ReviewResult
}

// SessionStore is the minimal surface of session memory the FSM needs.
type SessionStore interface {
	AppendMessage(message coretypes.Message)
	SessionID() string
	GetHistoryMessages() []coretypes.Message
	Compress(ctx context.Context, s memory.Summarizer, keepLastN, minToCompress int) (*coretypes.Usage, error)
}

// Dependencies holds every external dependency injected into the state machine.
// Behaviors are injected as minimal interfaces or plain functions rather than
// concrete types, so the FSM no longer reaches into process-wide singletons
// (working/persist/core/prompts globals) and stays decoupled from dag/infra.
type Dependencies struct {
	React       ReactExecutor
	DAG         DAGExecutor
	Reviewer    DriftReviewer
	Session     SessionStore
	Summarizer  memory.Summarizer // replaces the concrete *memory.Compressor
	Config      MachineConfig
	HandleDrift func(ctx *Context, reason string) // set by Machine

	// Bus is the shared per-loop event bus the FSM emits PlanRollback /
	// BeforeCompress onto. It may be nil (tests that do not observe these
	// signals), in which case the emits are no-ops.
	Bus *events.Bus

	// Injected hidden globals (formerly reached directly from working/persist/
	// core/prompts singletons). Wired by the composition root (kernel.go).
	GetPlan          func(id string) (*working.Plan, bool)
	PutPlan          func(p *working.Plan) error
	LoadCheckpoint   func(planID string, version int, dst any) error
	CustomToolNames  func() []string
	LoadAgentContext func() string
	RenderPlanPrompt func(pt prompts.Type, params any) (string, error)
}

// MachineConfig is the subset of config.Config needed by the FSM.
type MachineConfig struct {
	MaxCycles               int
	CompressMaxAttempts     int
	KeepLastN               int
	MinMessagesToCompress   int
	ReviewInterval          int
	MaxFailuresBeforeReview int

	// ForceMode skips the Intent phase and starts directly in the given mode.
	// "" or "auto" → intent classification; "plan" → init; "simple" → direct.
	ForceMode string
}

// emitBus delivers ev onto bus when a bus is configured. FSM unit tests that
// build a Dependencies without a Bus rely on this no-op, and it keeps the emit
// sites free of nil checks.
func emitBus[T any](bus *events.Bus, ev T) {
	if bus == nil {
		return
	}
	events.Emit(bus, ev)
}
