package fsm

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
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
