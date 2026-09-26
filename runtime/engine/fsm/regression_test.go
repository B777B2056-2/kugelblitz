package fsm

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/infra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopProvider answers every Generate with a plain text message (no tool
// calls), which drives the FSM through its fallback paths deterministically.
type noopProvider struct{}

func (noopProvider) Generate(_ context.Context, _ core.GenerateParams) (*core.Message, error) {
	msg := core.NewAssistantMessage(core.TextContent{Text: "ok"})
	return &msg, nil
}

// newTestMachine builds a Machine with a noop React provider and a fresh
// in-memory session.
func newTestMachine(forceMode string, maxCycles int) *Machine {
	return NewMachine(Dependencies{
		React:   infra.NewReactAgent(noopProvider{}, false),
		Session: memory.GetSessionMemoryManager().CreateSessionMemory("fsm-test"),
		Config:  MachineConfig{ForceMode: forceMode, MaxCycles: maxCycles},
	})
}

// ---- B12: hitting MaxCycles surfaces a sentinel error instead of silently
// returning nil. With MaxCycles=1 and ForceMode=plan, the Init state falls back
// to Direct (a non-terminal transition) and the loop must exit via the bound.

func TestMachine_Run_MaxCyclesExceeded(t *testing.T) {
	m := newTestMachine("plan", 1)

	_, err := m.Run(context.Background(), core.AgentInput{Text: "do it"})
	require.Error(t, err)
	assert.ErrorIs(t, err, core.ErrMaxCyclesExceeded)
}

// ---- B13: an Intent phase that produces no valid work mode falls back to
// Direct explicitly (no error, no crash) rather than silently.

func TestMachine_Run_IntentFallsBackToDirect(t *testing.T) {
	m := newTestMachine("", 10)

	msgs, err := m.Run(context.Background(), core.AgentInput{Text: "do it"})
	require.NoError(t, err)
	assert.NotEmpty(t, msgs, "direct execution should produce results")
}

// ---- B15: drift review cadence is keyed on task failure count, not the
// FSM step counter. ReviewInterval=N fires every N failures.

func TestShouldReview_UsesTaskFailsNotStepCount(t *testing.T) {
	ctx := &Context{
		StepCount: 100, // a large step count must not trigger a review
		Deps: Dependencies{
			Reviewer: infra.NewReviewer(nil, nil),
			Config:   MachineConfig{ReviewInterval: 2},
		},
	}

	ctx.TaskFails = 1 // first failure — interval not yet met
	assert.False(t, shouldReview(ctx))

	ctx.TaskFails = 2 // second failure — interval met
	assert.True(t, shouldReview(ctx))

	ctx.TaskFails = 3 // third failure — interval not met again
	assert.False(t, shouldReview(ctx))
}

func TestShouldReview_MaxFailuresBeforeReview(t *testing.T) {
	ctx := &Context{
		Deps: Dependencies{
			Reviewer: infra.NewReviewer(nil, nil),
			Config:   MachineConfig{MaxFailuresBeforeReview: 3},
		},
	}

	ctx.TaskFails = 2
	assert.False(t, shouldReview(ctx))

	ctx.TaskFails = 3
	assert.True(t, shouldReview(ctx))
}
