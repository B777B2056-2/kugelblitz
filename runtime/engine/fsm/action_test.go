package fsm

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/events"
	"github.com/B777B2056-2/kugelblitz/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSession records the Compress arguments so the test can assert the policy
// values that reach the compression primitive.
type fakeSession struct {
	keepLastN     int
	minToCompress int
	compressCalls int
	sequence      *[]string
}

func (f *fakeSession) AppendMessage(m coretypes.Message)       {}
func (f *fakeSession) SessionID() string                       { return "test" }
func (f *fakeSession) GetHistoryMessages() []coretypes.Message { return nil }
func (f *fakeSession) Compress(_ context.Context, _ memory.Summarizer, keepLastN, minToCompress int) (*coretypes.Usage, error) {
	f.keepLastN = keepLastN
	f.minToCompress = minToCompress
	f.compressCalls++
	if f.sequence != nil {
		*f.sequence = append(*f.sequence, "compress")
	}
	return nil, nil
}

// fakeReact returns ErrContextLengthExceeded until it has been called
// failUntil times, then succeeds — modelling one compress-and-retry cycle.
type fakeReact struct {
	failUntil int
	calls     int
}

func (f *fakeReact) ExecuteWithTools(_ context.Context, _ coretypes.Message, _ []coretypes.Message, _ []string) ([]coretypes.Message, error) {
	f.calls++
	if f.calls < f.failUntil {
		return nil, coretypes.ErrContextLengthExceeded
	}
	return []coretypes.Message{coretypes.NewAssistantMessage(coretypes.TextContent{Text: "done"})}, nil
}

// fakeSummarizer implements memory.Summarizer without hitting an LLM.
type fakeSummarizer struct{}

func (fakeSummarizer) Summarize(_ context.Context, _ []coretypes.Message, _ string) (string, *coretypes.Usage, error) {
	return "summary", nil, nil
}

func TestHandleContextExceeded_UsesConfigCompressPolicy(t *testing.T) {
	session := &fakeSession{}
	ctx := &Context{
		Ctx: context.Background(),
		Deps: Dependencies{
			Session:    session,
			React:      &fakeReact{failUntil: 1}, // succeed on the first retry inside handleContextExceeded
			Summarizer: fakeSummarizer{},
			Config: MachineConfig{
				CompressMaxAttempts:   1,
				KeepLastN:             7,
				MinMessagesToCompress: 3,
			},
		},
	}

	result, err := handleContextExceeded(ctx, coretypes.NewSystemMessage(coretypes.TextContent{Text: "sys"}), nil)
	require.NoError(t, err)
	assert.NotEmpty(t, result, "compress-then-retry should succeed")
	assert.Equal(t, 1, session.compressCalls)
	assert.Equal(t, 7, session.keepLastN, "KeepLastN should come from MachineConfig, not be hardcoded")
	assert.Equal(t, 3, session.minToCompress, "MinMessagesToCompress should come from MachineConfig, not be hardcoded")
}

func TestHandleContextExceeded_FiresBeforeCompress(t *testing.T) {
	bus := events.NewBus()
	var order []string
	var identity constants.AgentIdentity
	events.On(bus, func(ev events.BeforeCompress) {
		identity = ev.Identity
		order = append(order, "before")
	})
	session := &fakeSession{sequence: &order}
	ctx := &Context{
		Ctx: context.Background(),
		Deps: Dependencies{
			Session:    session,
			React:      &fakeReact{failUntil: 1},
			Summarizer: fakeSummarizer{},
			Config:     MachineConfig{CompressMaxAttempts: 1, KeepLastN: 2, MinMessagesToCompress: 1},
			Bus:        bus,
		},
	}

	_, err := handleContextExceeded(ctx, coretypes.NewSystemMessage(coretypes.TextContent{Text: "sys"}), nil)
	require.NoError(t, err)

	assert.Equal(t, []string{"before", "compress"}, order,
		"BeforeCompress must fire before the session is compressed")
	assert.Equal(t, constants.AgentMain, identity)
}
