package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/B777B2056-2/kugelblitz/config"
	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCompressProvider answers every Generate with a fixed summary, so
// CompressContext exercises the full compress path without a real LLM.
type mockCompressProvider struct{}

func (mockCompressProvider) Generate(_ context.Context, _ coretypes.GenerateParams) (*coretypes.Message, error) {
	msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "compressed summary"})
	msg.Usage = &coretypes.Usage{TotalTokens: 10, InputTokens: 6, OutputTokens: 4}
	return &msg, nil
}

// newTestKernel creates a Kernel for tests with a minimal session memory.
func newTestKernel() *Kernel {
	sessionMem := memory.GetSessionMemoryManager().CreateSessionMemory("test-session")
	return NewKernel(sessionMem,
		config.Config{
			Runtime:         config.RuntimeConfig{MaxStateMachineCycles: 30},
			ContextCompress: config.ContextCompressConfig{MaxAttempts: 1},
			TargetDrift:     config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
		},
		events.NewBus(),
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
		NewKernel(nil, config.Config{}, nil)
	})
}

func TestKernel_Compressor(t *testing.T) {
	k := newTestKernel()
	assert.NotNil(t, k.Compressor())
}

func TestKernel_RegisterEventHooks(t *testing.T) {
	k := newTestKernel()

	called := false
	k.RegisterEventHooks(core.AgentEventHooks{
		OnToolCallEnd: func(id constants.AgentIdentity, result coretypes.ToolCallResult) { called = true },
	})

	// Verify hooks are subscribed onto the kernel's shared bus.
	events.Emit(k.bus, events.ToolCallEnd{Identity: constants.AgentMain, Result: coretypes.ToolCallResult{}})
	assert.True(t, called)
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

func TestKernel_CompressContext(t *testing.T) {
	memory.ResetSessionMemoryManager()
	sessionMem := memory.GetSessionMemoryManager().CreateSessionMemory("compress-test")
	for i := 0; i < 4; i++ {
		sessionMem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("msg %d", i)}))
	}

	k := NewKernel(sessionMem, config.Config{
		Model:   config.ModelConfig{Provider: mockCompressProvider{}},
		Runtime: config.RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: config.ContextCompressConfig{
			MaxAttempts: 1, KeepLastN: 2, MinMessagesToCompress: 1,
		},
		TargetDrift: config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
	}, events.NewBus())

	usage, err := k.CompressContext(context.Background())
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, "compressed summary", sessionMem.Summary())
	// history is truncated to KeepLastN=2, plus the prepended summary message
	assert.Len(t, sessionMem.GetHistoryMessages(), 3)
}

func TestKernel_CompressContext_FiresBeforeCompressHook(t *testing.T) {
	memory.ResetSessionMemoryManager()
	sessionMem := memory.GetSessionMemoryManager().CreateSessionMemory("compress-hook-test")
	for i := 0; i < 4; i++ {
		sessionMem.AppendMessage(coretypes.NewUserMessage(coretypes.TextContent{Text: fmt.Sprintf("msg %d", i)}))
	}

	k := NewKernel(sessionMem, config.Config{
		Model:   config.ModelConfig{Provider: mockCompressProvider{}},
		Runtime: config.RuntimeConfig{MaxStateMachineCycles: 30},
		ContextCompress: config.ContextCompressConfig{
			MaxAttempts: 1, KeepLastN: 2, MinMessagesToCompress: 1,
		},
		TargetDrift: config.TargetDriftConfig{ReviewInterval: 12, MaxFailuresBeforeReview: 5},
	}, events.NewBus())

	fired := false
	k.RegisterEventHooks(core.AgentEventHooks{
		OnBeforeCompress: func(id constants.AgentIdentity) { fired = true },
	})

	_, err := k.CompressContext(context.Background())
	require.NoError(t, err)
	assert.True(t, fired, "CompressContext should fire OnBeforeCompress before compressing")
}
