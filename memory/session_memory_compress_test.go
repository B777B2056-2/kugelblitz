package memory

import (
	"context"
	"fmt"
	"testing"

	"github.com/B777B2056-2/kugelblitz/core"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
)

// TestSessionMemory_Compress_PreservesMessagesAppendedDuringSummarize verifies
// that messages appended while the LLM summarization call is in flight are not
// silently dropped when the compressed history replaces the snapshot (B6).
func TestSessionMemory_Compress_PreservesMessagesAppendedDuringSummarize(t *testing.T) {
	mem := newSessionMemory("compress-race")
	for i := 0; i < 6; i++ {
		mem.AppendMessage(core.NewUserMessage(core.TextContent{Text: fmt.Sprintf("m%d", i)}))
	}

	entered := make(chan struct{})
	release := make(chan struct{})

	mp := &mockCompressProvider{
		generate: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			close(entered) // snapshot has been taken; summarization is now "in flight"
			<-release
			msg := core.NewAssistantMessage(core.TextContent{Text: "summary"})
			return &msg, nil
		},
	}
	c := NewCompressor(mp, otel.Tracer("test"))

	done := make(chan error, 1)
	go func() {
		_, err := mem.Compress(context.Background(), c, 2, 1)
		done <- err
	}()

	<-entered
	mem.AppendMessage(core.NewUserMessage(core.TextContent{Text: "during-compress"}))
	close(release)

	require.NoError(t, <-done)

	texts := make([]string, 0, 4)
	for _, m := range mem.GetHistoryMessages() {
		if tc, ok := m.Content.(core.TextContent); ok {
			texts = append(texts, tc.Text)
		}
	}
	require.Contains(t, texts, "during-compress", "message appended during compression was lost")
}
