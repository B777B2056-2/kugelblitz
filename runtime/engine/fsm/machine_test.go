package fsm

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/memory/working"
	"github.com/B777B2056-2/kugelblitz/runtime/engine/types"
	"github.com/stretchr/testify/assert"
)

func TestMachine_InitialState_ForceMode(t *testing.T) {
	tests := []struct {
		name      string
		forceMode string
		want      constants.PlanState
	}{
		{"empty defaults to intent", "", constants.PlanStateIntent},
		{"auto defaults to intent", "auto", constants.PlanStateIntent},
		{"plan skips intent to init", "plan", constants.PlanStateInit},
		{"simple skips intent to direct", "simple", constants.PlanStateDirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMachine(Dependencies{Config: MachineConfig{ForceMode: tt.forceMode}})
			assert.Equal(t, tt.want, m.currentState)
		})
	}
}

func TestMachine_Reset_UsesForceMode(t *testing.T) {
	m := NewMachine(Dependencies{Config: MachineConfig{ForceMode: "plan"}})
	m.currentState = constants.PlanStateDone
	m.reset()
	assert.Equal(t, constants.PlanStateInit, m.currentState)
}

// fakeDAG is a minimal DAGExecutor whose Cancel is a no-op (handleDrift cancels
// in-flight workers before rolling back).
type fakeDAG struct{}

func (fakeDAG) ExecuteBatch(context.Context, *working.Plan, func(string, string, string)) types.BatchResult {
	return types.BatchResult{}
}
func (fakeDAG) Cancel() {}

func TestMachine_HandleDrift_EmitsPlanRollback(t *testing.T) {
	core.GetWorkspace().SetDir(t.TempDir())

	bus := events.NewBus()
	var got events.PlanRollback
	events.On(bus, func(ev events.PlanRollback) { got = ev })

	ctx := &Context{
		Plan: &working.Plan{ID: "p1", Name: "original", Version: 2},
		Deps: Dependencies{
			Session: &fakeSession{},
			DAG:     fakeDAG{},
			LoadCheckpoint: func(_ string, _ int, dst any) error {
				cp := dst.(*working.Checkpoint)
				cp.Plan = &working.Plan{ID: "p1", Name: "v1", Version: 1}
				return nil
			},
			Bus: bus,
		},
	}

	(&Machine{}).handleDrift(ctx, "drifted")

	assert.Equal(t, constants.AgentMain, got.Identity)
	assert.Equal(t, "p1", got.PlanID)
	assert.Equal(t, 1, got.TargetVersion)
	assert.Equal(t, "v1", got.PlanName)
}
