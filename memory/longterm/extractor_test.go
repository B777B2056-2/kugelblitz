package longterm

import (
	"context"
	"strings"
	"testing"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/llm"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExtractProvider struct{ response string }

func (m *mockExtractProvider) Generate(ctx context.Context, params coretypes.GenerateParams) (*coretypes.Message, error) {
	return &coretypes.Message{
		Content: coretypes.TextContent{Text: m.response},
		Usage:   &coretypes.Usage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
	}, nil
}

func newTestExtractor(p coretypes.ILMProvider) *Extractor {
	return NewExtractor(llm.NewCaller(p, nil))
}

func TestBuildPrompt_IncludesToolCalls(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	ec := &ExtractionContext{
		UserMessage: "Fix the bug",
		Conversation: []coretypes.Message{
			{Role: "assistant", Content: coretypes.ToolCallContent{
				Details: []coretypes.ToolCallDetail{
					{ID: "t1", ToolName: "file_read", Args: map[string]any{"path": "main.go"}},
					{ID: "t2", ToolName: "web_search", Args: map[string]any{"query": "Go generics"}},
				},
			}},
			{Role: "tool", Content: coretypes.ToolResultContent{
				Results: []coretypes.ToolCallResult{
					{ToolCallID: "t1", ToolName: "file_read", Outputs: map[string]any{"content": "package main..."}},
					{ToolCallID: "t2", ToolName: "web_search", Outputs: map[string]any{"results": "3 items"}},
				},
			}},
		},
	}
	prompt := ext.buildPrompt(ec)
	assert.Contains(t, prompt, "[Tool calls:")
	assert.Contains(t, prompt, "file_read")
	assert.Contains(t, prompt, "[Tool results:")
}

func TestBuildPrompt_IncludesExistingFacts(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	ec := &ExtractionContext{
		ExistingItems: []memorytypes.MemoryItem{{Section: "prefs", Key: "lang", Value: "Go", Confidence: 0.95}},
	}
	assert.Contains(t, ext.buildPrompt(ec), "Existing Memories")
	assert.Contains(t, ext.buildPrompt(ec), "Go")
}

func TestBuildPrompt_IncludesSessionSummary(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	prompt := ext.buildPrompt(&ExtractionContext{SessionSummary: "user prefers Go"})
	assert.Contains(t, prompt, "user prefers Go")
}

func TestParseResponse_ValidJSON(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	candidates, err := ext.parseResponse(`[{"section":"prefs","key":"lang","value":"Go"},{"section":"episodic","key":"debug","value":"found nil pointer"}]`)
	require.NoError(t, err)
	assert.Len(t, candidates, 2)
	assert.Equal(t, "Go", candidates[0].Value)
	assert.Equal(t, "episodic", candidates[1].Section)
}

func TestParseResponse_JSONWithExtraText(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	candidates, err := ext.parseResponse("Here:\n```json\n[{\"section\":\"prefs\",\"key\":\"lang\",\"value\":\"Go\"}]\n```")
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
}

func TestParseResponse_MalformedJSON(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	_, err := ext.parseResponse("no json here")
	assert.Error(t, err)
}

func TestExtract_Integration(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{response: `[{"section":"prefs","key":"lang","value":"Go","source_evidence":"user said","suggested_confidence":0.9}]`})
	candidates, usage, err := ext.Extract(context.Background(), &ExtractionContext{UserMessage: "I want Go"})
	require.NoError(t, err)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "Go", candidates[0].Value)
	assert.NotNil(t, usage)
}

func TestCompactArgs_LongValues(t *testing.T) {
	ext := newTestExtractor(&mockExtractProvider{})
	longStr := strings.Repeat("x", 200)
	result := ext.compactArgs(map[string]any{"content": longStr})
	assert.Less(t, len(result), 150)
	assert.Contains(t, result, "...")
}
