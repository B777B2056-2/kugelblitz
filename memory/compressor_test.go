package memory

import (
	"context"
	"testing"

	"kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSummarizePrompt_NoExistingSummary(t *testing.T) {
	msgs := []core.Message{
		core.NewUserMessage("r", core.TextContent{Text: "hello"}),
		core.NewAssistantMessage("r", core.TextContent{Text: "world"}),
	}
	prompt := buildSummarizePrompt(msgs, "")
	assert.Contains(t, prompt, "Summarize the following conversation")
	assert.Contains(t, prompt, "hello")
	assert.Contains(t, prompt, "world")
	assert.NotContains(t, prompt, "EXISTING SUMMARY")
	assert.NotContains(t, prompt, "previous summary")
}

func TestBuildSummarizePrompt_WithExistingSummary(t *testing.T) {
	msgs := []core.Message{
		core.NewUserMessage("r", core.TextContent{Text: "new info"}),
	}
	existing := "User likes Go programming."
	prompt := buildSummarizePrompt(msgs, existing)
	assert.Contains(t, prompt, "EXISTING SUMMARY")
	assert.Contains(t, prompt, existing)
	assert.Contains(t, prompt, "CONSOLIDATED")
	assert.Contains(t, prompt, "PREFER the new information")
	assert.Contains(t, prompt, "new info")
}

func TestBuildSummarizePrompt_ToolCalls(t *testing.T) {
	msgs := []core.Message{
		{
			Role: "assistant",
			Content: core.ToolCallContent{
				Details: []core.ToolCallDetail{
					{ID: "t1", ToolName: "search"},
					{ID: "t2", ToolName: "calculate"},
				},
			},
		},
	}
	prompt := buildSummarizePrompt(msgs, "")
	assert.Contains(t, prompt, "[tool calls: search, calculate]")
}

func TestBuildSummarizePrompt_ToolResults(t *testing.T) {
	msgs := []core.Message{
		{
			Role: "tool",
			Content: core.ToolResultContent{
				Results: []core.ToolCallResult{
					{ToolCallID: "t1"},
					{ToolCallID: "t2"},
					{ToolCallID: "t3"},
				},
			},
		},
	}
	prompt := buildSummarizePrompt(msgs, "")
	assert.Contains(t, prompt, "[tool results: 3]")
}

func TestTruncate_Short(t *testing.T) {
	assert.Equal(t, "hi", truncate("hi", 500))
}

func TestTruncate_Long(t *testing.T) {
	long := ""
	for i := 0; i < 600; i++ {
		long += "x"
	}
	result := truncate(long, 500)
	assert.Len(t, result, 503) // 500 chars + "..."
	assert.True(t, result[len(result)-3:] == "...")
}


type mockCompressProvider struct {
	generate func(ctx context.Context, params core.GenerateParams) (*core.Message, error)
}

func (m *mockCompressProvider) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	return m.generate(ctx, params)
}

func TestCompressor_Summarize_ReturnsUsage(t *testing.T) {
	usage := &core.Usage{InputTokens: 200, OutputTokens: 150, TotalTokens: 350}
	mp := &mockCompressProvider{
		generate: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return &core.Message{
				Role:    "assistant",
				Content: core.TextContent{Text: "compressed summary"},
				Usage:   usage,
			}, nil
		},
	}
	c := NewCompressor(mp)
	summary, gotUsage, err := c.Summarize(context.Background(), []core.Message{
		core.NewUserMessage("u", core.TextContent{Text: "hello"}),
	}, "")
	assert.NoError(t, err)
	assert.Equal(t, "compressed summary", summary)
	require.NotNil(t, gotUsage)
	assert.Equal(t, usage.InputTokens, gotUsage.InputTokens)
	assert.Equal(t, usage.OutputTokens, gotUsage.OutputTokens)
	assert.Equal(t, usage.TotalTokens, gotUsage.TotalTokens)
}

func TestCompressor_Summarize_NilUsageWhenError(t *testing.T) {
	mp := &mockCompressProvider{
		generate: func(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
			return nil, assert.AnError
		},
	}
	c := NewCompressor(mp)
	_, gotUsage, err := c.Summarize(context.Background(), []core.Message{
		core.NewUserMessage("u", core.TextContent{Text: "hello"}),
	}, "")
	assert.Error(t, err)
	assert.Nil(t, gotUsage)
}
