package engine

import (
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/B777B2056-2/kugelblitz/memory"

	"github.com/stretchr/testify/assert"
)

// newTestKernel creates a Kernel for tests with a minimal session memory.
func newTestKernel() *Kernel {
	sessionMem := memory.GetSessionMemoryManager().CreateSessionMemory("test-session")
	return NewKernel(sessionMem,
		config.Config{
			Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
			ContextCompress: config.ContextCompressConfig{MaxAttempts: 1},
			TargetDrift:     config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
		},
	)
}

func TestNewKernel_CreatesWithValidDeps(t *testing.T) {
	k := newTestKernel()
	assert.NotNil(t, k)
	assert.NotNil(t, k.machine)
	assert.NotNil(t, k.mainReact)
	assert.NotNil(t, k.dagExec)
	assert.NotNil(t, k.sessionMem)
	assert.NotNil(t, k.compressor)
	assert.NotNil(t, k.reviewer)
}

func TestNewKernel_PanicsOnNilSession(t *testing.T) {
	assert.Panics(t, func() {
		NewKernel(nil, config.Config{})
	})
}

func TestKernel_Compressor(t *testing.T) {
	k := newTestKernel()
	assert.NotNil(t, k.Compressor())
}

func TestKernel_RegisterEventHooks(t *testing.T) {
	k := newTestKernel()

	hooks := core.AgentEventHooks{
		OnToolCallEnd: func(id constants.AgentIdentity, result core.ToolCallResult) {},
	}
	k.RegisterEventHooks(hooks)

	// Verify hooks were forwarded
	assert.NotNil(t, k.mainReact.EventHooks.OnToolCallEnd)
}

func TestKernel_HumanLoopWaiting_InitiallyFalse(t *testing.T) {
	k := newTestKernel()
	assert.False(t, k.HumanLoopWaiting())
}

func TestKernel_DependenciesAreWired(t *testing.T) {
	k := newTestKernel()
	// Verify all dependency fields are non-nil after construction
	assert.NotNil(t, k.mainReact)
	assert.NotNil(t, k.dagExec)
	assert.NotNil(t, k.reviewer)
}
