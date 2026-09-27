package infra

import (
	"context"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/stretchr/testify/require"
)

// TestWorkerAgent_ExecuteTask_NoDuplicateOutput_StreamMode guards B17: in stream
// mode the reply text must be captured from a single source. Before the fix the
// reply was written both by the OnReplyChunk hook and by the final-message loop,
// producing "Hello WorldHello World".
func TestWorkerAgent_ExecuteTask_NoDuplicateOutput_StreamMode(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnReplyChunk("Hello")
				params.EventHandler.OnReplyChunk(" World")
			}
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "Hello World"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}
	w := NewWorkerAgent(provider, true)
	out, _, err := w.ExecuteTask(context.Background(), "goal", "action")
	require.NoError(t, err)
	require.Equal(t, "Hello World", out)
}

// TestWorkerAgent_ExecuteTask_BlockMode_CapturesOutput guards B17 for the
// non-streaming path: with no chunk events fired, the reply must still be
// captured from the returned message.
func TestWorkerAgent_ExecuteTask_BlockMode_CapturesOutput(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			msg := coretypes.NewAssistantMessage(coretypes.TextContent{Text: "Hello World"})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}
	w := NewWorkerAgent(provider, false)
	out, _, err := w.ExecuteTask(context.Background(), "goal", "action")
	require.NoError(t, err)
	require.Equal(t, "Hello World", out)
}

// TestWorkerAgent_ExecuteTask_CompositeReply guards B17 for composite replies
// (reasoning + text): the text portion must be captured without duplication.
func TestWorkerAgent_ExecuteTask_CompositeReply(t *testing.T) {
	provider := &MockProvider{
		GenerateFn: func(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
			if params.EventHandler != nil {
				params.EventHandler.OnReplyChunk("final answer")
			}
			msg := coretypes.NewAssistantMessage(coretypes.CompositeContent{
				Parts: []coretypes.Content{
					coretypes.ReasoningContent{Reasoning: "thinking"},
					coretypes.TextContent{Text: "final answer"},
				},
			})
			msg.FinishReason = "stop"
			return &msg, nil
		},
	}
	w := NewWorkerAgent(provider, true)
	out, _, err := w.ExecuteTask(context.Background(), "goal", "action")
	require.NoError(t, err)
	require.Equal(t, "final answer", out)
}
