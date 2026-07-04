package longterm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"
)

// ExtractionContext bundles all information the LLM needs for fact extraction.
type ExtractionContext struct {
	SessionID       string         // Current session identifier
	UserMessage     string         // Original user goal/request
	Conversation    []core.Message // Full conversation including tool calls and results
	SessionSummary  string         // Current session summary (from SessionMemory)
	ExistingItems   []MemoryItem         // Existing LTM items for dedup/conflict awareness
	CheckpointGoals []string       // Active plan goals from checkpoints
}

// MemoryItemCandidate is a raw fact produced by the LLM before conflict resolution.
type MemoryItemCandidate struct {
	Section             string  `json:"section"`
	Key                 string  `json:"key"`
	Value               string  `json:"value"`
	SourceEvidence      string  `json:"source_evidence"`
	SuggestedConfidence float64 `json:"suggested_confidence"`
}

// Extractor uses an LLM provider to extract long-term memories from conversations.
type Extractor struct {
	provider core.ILMProvider
}

// NewExtractor creates an Extractor with the given LLM provider.
func NewExtractor(provider core.ILMProvider) *Extractor {
	return &Extractor{provider: provider}
}

// Extract runs the LLM extraction and returns fact candidates.
// All memory types (items, episodic, lessons, patterns) are extracted as
// MemoryItemCandidate entries with different section names.
func (e *Extractor) Extract(ctx context.Context, ec *ExtractionContext) ([]MemoryItemCandidate, *core.Usage, error) {
	prompt := e.buildPrompt(ec)

	msg := core.NewUserMessage(core.TextContent{Text: prompt})
	resp, err := e.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{msg},
		Stream:   false,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("extract: %w", err)
	}

	usage := resp.Usage
	text := ""
	if tc, ok := resp.Content.(core.TextContent); ok {
		text = tc.Text
	}

	candidates, err := e.parseResponse(text)
	if err != nil {
		return nil, usage, fmt.Errorf("extract parse: %w", err)
	}
	return candidates, usage, nil
}

// buildPrompt builds the LLM extraction prompt.
func (e *Extractor) buildPrompt(ec *ExtractionContext) string {
	var sb strings.Builder

	sb.WriteString(`You are a memory extraction system. From the conversation, extract long-term memories AND entity relationships.

Output ONLY valid JSON:
{
  "items": [
    {"section":"...","key":"...","value":"...","source_evidence":"...","suggested_confidence":0.9}
  ],
  "entities": [
    {"name":"EntityName","type":"language|file|concept|person|project|bug|tool","labels":["tag1","tag2"]}
  ],
  "relationships": [
    {"from":"EntityName","to":"OtherEntity","type":"uses|depends_on|mentions|causes|contains|implements","weight":1.0}
  ]
}

Sections for items: user_preferences, project_facts, episodic, lessons, patterns
Entity types: language, file, concept, person, project, bug, tool
Relationship weight: 1.0 = explicitly stated; < 1.0 = inferred

Rules:
- Be concise. Only include things clearly stated. Do not fabricate.
- key = short label; value = detailed content
`)

	// Session context
	if ec.SessionSummary != "" {
		sb.WriteString("\n## Session Context (summary)\n")
		sb.WriteString(ec.SessionSummary)
		sb.WriteString("\n")
	}

	// Existing items for dedup awareness
	if len(ec.ExistingItems) > 0 {
		sb.WriteString("\n## Existing Memories (avoid duplicates)\n")
		for _, f := range ec.ExistingItems {
			sb.WriteString(fmt.Sprintf("- [%s] %s: %s (c%.2f)\n", f.Section, f.Key, f.Value, f.Confidence))
		}
	}

	// Active plan goals
	if len(ec.CheckpointGoals) > 0 {
		sb.WriteString("\n## Active Plan Goals\n")
		for _, g := range ec.CheckpointGoals {
			sb.WriteString(fmt.Sprintf("- %s\n", g))
		}
	}

	// User message
	if ec.UserMessage != "" {
		sb.WriteString("\n## User Request\n")
		sb.WriteString(ec.UserMessage)
		sb.WriteString("\n")
	}

	// Full conversation with tool summaries
	sb.WriteString("\n## Conversation\n")
	sb.WriteString(e.summarizeConversation(ec.Conversation))

	return sb.String()
}

// summarizeConversation builds a compact representation of the conversation.
func (e *Extractor) summarizeConversation(messages []core.Message) string {
	var sb strings.Builder

	for _, msg := range messages {
		switch c := msg.Content.(type) {
		case core.TextContent:
			if len(c.Text) > 20 {
				sb.WriteString(truncate(c.Text, 500))
				sb.WriteString("\n")
			}
		case core.ToolCallContent:
			sb.WriteString(e.summarizeToolCalls(c.Details))
		case core.ToolResultContent:
			sb.WriteString(e.summarizeToolResults(c.Results))
		}
	}
	return sb.String()
}

func (e *Extractor) summarizeToolCalls(details []core.ToolCallDetail) string {
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

func (e *Extractor) summarizeToolResults(results []core.ToolCallResult) string {
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
func (e *Extractor) ExtractFull(ctx context.Context, ec *ExtractionContext) (*ExtractionFullResult, *core.Usage, error) {
	prompt := e.buildPrompt(ec)
	msg := core.NewUserMessage(core.TextContent{Text: prompt})
	resp, err := e.provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{msg},
		Stream:   false,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("extract: %w", err)
	}
	usage := resp.Usage
	text := ""
	if tc, ok := resp.Content.(core.TextContent); ok {
		text = tc.Text
	}
	result, err := e.parseFullResponse(text)
	if err != nil {
		// Fallback: try parsing as items-only array
		candidates, err2 := e.parseResponse(text)
		if err2 != nil {
			return nil, usage, fmt.Errorf("extract parse: %w", err)
		}
		return &ExtractionFullResult{Items: candidates}, usage, nil
	}
	return result, usage, nil
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
