package longterm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/llm"
	memorytypes "github.com/B777B2056-2/kugelblitz/memory/types"
	"github.com/B777B2056-2/kugelblitz/prompts"
)

// ExtractionContext bundles all information the LLM needs for fact extraction.
type ExtractionContext struct {
	SessionID       string                   // Current session identifier
	UserMessage     string                   // Original user goal/request
	Conversation    []coretypes.Message      // Full conversation including tool calls and results
	SessionSummary  string                   // Current session summary (from SessionMemory)
	ExistingItems   []memorytypes.MemoryItem // Existing LTM items for dedup/conflict awareness
	CheckpointGoals []string                 // Active plan goals from checkpoints
}

// MemoryItemCandidate is a raw fact produced by the LLM before conflict resolution.
type MemoryItemCandidate struct {
	Section             string  `json:"section"`
	Key                 string  `json:"key"`
	Value               string  `json:"value"`
	SourceEvidence      string  `json:"source_evidence"`
	SuggestedConfidence float64 `json:"suggested_confidence"`
}

// Extractor uses a unified LLM caller to extract long-term memories from conversations.
type Extractor struct {
	caller *llm.Caller
}

// NewExtractor creates an Extractor with the given LLM caller.
func NewExtractor(caller *llm.Caller) *Extractor {
	return &Extractor{caller: caller}
}

// Extract runs the LLM extraction and returns fact candidates.
// All memory types (items, episodic, lessons, patterns) are extracted as
// MemoryItemCandidate entries with different section names.
func (e *Extractor) Extract(ctx context.Context, ec *ExtractionContext) ([]MemoryItemCandidate, *coretypes.Usage, error) {
	prompt := e.buildPrompt(ec)

	res, err := e.caller.Call(ctx, llm.Request{
		Prompt:   prompt,
		Mode:     llm.ModeText,
		SpanName: "extract",
	})
	if err != nil {
		return nil, res.Usage, fmt.Errorf("extract: %w", err)
	}

	candidates, err := e.parseResponse(res.Text)
	if err != nil {
		return nil, res.Usage, fmt.Errorf("extract parse: %w", err)
	}
	return candidates, res.Usage, nil
}

// buildPrompt builds the LLM extraction prompt.
func (e *Extractor) buildPrompt(ec *ExtractionContext) string {
	return prompts.DefaultFactory.MustRender(prompts.TypeExtract, prompts.ExtractParams{
		SessionSummary:  ec.SessionSummary,
		ExistingItems:   e.formatExistingItems(ec.ExistingItems),
		CheckpointGoals: e.formatCheckpointGoals(ec.CheckpointGoals),
		UserMessage:     ec.UserMessage,
		Conversation:    e.summarizeConversation(ec.Conversation),
	})
}

// formatExistingItems renders existing memories for dedup awareness.
func (e *Extractor) formatExistingItems(items []memorytypes.MemoryItem) string {
	var sb strings.Builder
	for _, f := range items {
		fmt.Fprintf(&sb, "- [%s] %s: %s (c%.2f)\n", f.Section, f.Key, f.Value, f.Confidence)
	}
	return sb.String()
}

// formatCheckpointGoals renders active plan goals as a bullet list.
func (e *Extractor) formatCheckpointGoals(goals []string) string {
	var sb strings.Builder
	for _, g := range goals {
		fmt.Fprintf(&sb, "- %s\n", g)
	}
	return sb.String()
}

// summarizeConversation builds a compact representation of the conversation.
func (e *Extractor) summarizeConversation(messages []coretypes.Message) string {
	var sb strings.Builder

	for _, msg := range messages {
		switch c := msg.Content.(type) {
		case coretypes.TextContent:
			if len(c.Text) > 20 {
				sb.WriteString(truncate(c.Text, 500))
				sb.WriteString("\n")
			}
		case coretypes.ToolCallContent:
			sb.WriteString(e.summarizeToolCalls(c.Details))
		case coretypes.ToolResultContent:
			sb.WriteString(e.summarizeToolResults(c.Results))
		}
	}
	return sb.String()
}

func (e *Extractor) summarizeToolCalls(details []coretypes.ToolCallDetail) string {
	if len(details) == 0 {
		return ""
	}
	parts := make([]string, len(details))
	for i, d := range details {
		args := e.compactArgs(d.Args)
		parts[i] = fmt.Sprintf("%s(%s)", d.ToolName, args)
	}
	return fmt.Sprintf("[Tool calls: %s]\n", strings.Join(parts, ", "))
}

func (e *Extractor) summarizeToolResults(results []coretypes.ToolCallResult) string {
	if len(results) == 0 {
		return ""
	}
	parts := make([]string, len(results))
	for i, r := range results {
		if errMsg, isErr := r.Outputs["error"]; isErr {
			parts[i] = fmt.Sprintf("%s → ERROR: %v", r.ToolName, errMsg)
		} else {
			parts[i] = fmt.Sprintf("%s → %d output fields", r.ToolName, len(r.Outputs))
		}
	}
	return fmt.Sprintf("[Tool results: %s]\n", strings.Join(parts, ", "))
}

func (e *Extractor) compactArgs(args map[string]any) string {
	var parts []string
	for k, v := range args {
		s := fmt.Sprintf("%v", v)
		if len(s) > 80 {
			s = s[:77] + "..."
		}
		parts = append(parts, fmt.Sprintf("%s=%q", k, s))
	}
	return strings.Join(parts, ", ")
}

// parseResponse extracts JSON from the LLM response.
func (e *Extractor) parseResponse(text string) ([]MemoryItemCandidate, error) {
	start := strings.Index(text, "[")
	end := strings.LastIndex(text, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found in response")
	}
	jsonStr := text[start : end+1]

	var candidates []MemoryItemCandidate
	if err := json.Unmarshal([]byte(jsonStr), &candidates); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}
	return candidates, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// ExtractionFullResult is the LLM's full extraction output including graph data.
type ExtractionFullResult struct {
	Items         []MemoryItemCandidate `json:"items"`
	Entities      []EntityCandidate     `json:"entities"`
	Relationships []RelCandidate        `json:"relationships"`
}

// ExtractFull runs the LLM extraction and returns the full result including entities and relationships.
func (e *Extractor) ExtractFull(ctx context.Context, ec *ExtractionContext) (*ExtractionFullResult, *coretypes.Usage, error) {
	prompt := e.buildPrompt(ec)
	res, err := e.caller.Call(ctx, llm.Request{
		Prompt:   prompt,
		Mode:     llm.ModeText,
		SpanName: "extract",
	})
	if err != nil {
		return nil, res.Usage, fmt.Errorf("extract: %w", err)
	}
	text := res.Text
	result, err := e.parseFullResponse(text)
	if err != nil {
		// Fallback: try parsing as items-only array
		candidates, err2 := e.parseResponse(text)
		if err2 != nil {
			return nil, res.Usage, fmt.Errorf("extract parse: %w", err)
		}
		return &ExtractionFullResult{Items: candidates}, res.Usage, nil
	}
	return result, res.Usage, nil
}

func (e *Extractor) parseFullResponse(text string) (*ExtractionFullResult, error) {
	start := strings.Index(text, "{\"items\"")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	jsonStr := text[start : end+1]
	var result ExtractionFullResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("json decode: %w", err)
	}
	return &result, nil
}
