package chat_completions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// Format implements the OpenAI Chat Completions API wire protocol.
// It handles the standard request/response cycle; provider-specific
// extensions (headers, extra params) are applied before calling Generate.
type Format struct {
	client    openai.Client
	model     string
	converter *Converter
}

// NewFormat creates a Chat Completions Format with the given configuration.
func NewFormat(apiKey, baseURL, model string) *Format {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &Format{
		client:    openai.NewClient(opts...),
		model:     model,
		converter: NewConverter(),
	}
}

// Generate builds a request and performs a Chat Completions API call.
// Extensions that need to modify the request before sending should override
// this method (e.g., to inject provider-specific params) and call Block/Stream
// with the modified request.
func (f *Format) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	if f.model == "" {
		return nil, errors.New("model not specified")
	}
	req, err := f.BuildRequest(params)
	if err != nil {
		return nil, err
	}
	// Debug: print raw request JSON
	if raw, jerr := json.MarshalIndent(req, "", "  "); jerr == nil {
		core.Info("=== LLM request ===\n" + string(raw))
	}
	if params.Stream {
		return f.Stream(ctx, req, params)
	}
	return f.Block(ctx, req, params)
}

// Block sends a non-streaming request and returns the parsed response.
// Non-chunk callbacks (OnFunctionCall, OnFinished, OnUsageUpdated) are
// triggered once after the complete response is received.
func (f *Format) Block(ctx context.Context, req openai.ChatCompletionNewParams, params core.GenerateParams) (*core.Message, error) {
	handler := params.EventHandler

	completion, err := f.client.Chat.Completions.New(ctx, req)
	if err != nil {
		if handler != nil {
			handler.OnError(err)
		}
		return nil, fmt.Errorf("chat completions: %w", wrapContextError(err))
	}
	if completion == nil || len(completion.Choices) == 0 {
		err := errors.New("no completion choices returned")
		if handler != nil {
			handler.OnError(err)
		}
		return nil, err
	}

	parentID := params.Messages[len(params.Messages)-1].ID
	msg, err := f.converter.ParseResponse(ctx, parentID, completion.Choices[0].Message)
	if err != nil {
		if handler != nil {
			handler.OnError(err)
		}
		return nil, err
	}
	if completion.Choices[0].FinishReason != "" {
		msg.FinishReason = completion.Choices[0].FinishReason
	}

	// Extract usage from the completion
	if completion.Usage.TotalTokens > 0 {
		msg.Usage = &core.Usage{
			TotalTokens:     completion.Usage.TotalTokens,
			InputTokens:     completion.Usage.PromptTokens,
			CachedTokens:    completion.Usage.PromptTokensDetails.CachedTokens,
			ReasoningTokens: completion.Usage.CompletionTokensDetails.ReasoningTokens,
			OutputTokens:    completion.Usage.CompletionTokens,
		}
	}

	// Trigger non-chunk callbacks (same set as stream mode, minus chunk callbacks)
	if handler != nil {
		// OnFunctionCall: once per tool call
		if tc, ok := msg.Content.(core.ToolCallContent); ok {
			for _, d := range tc.Details {
				handler.OnFunctionCall(d)
			}
		}
		if cc, ok := msg.Content.(core.CompositeContent); ok {
			for _, part := range cc.Parts {
				if tc, ok := part.(core.ToolCallContent); ok {
					for _, d := range tc.Details {
						handler.OnFunctionCall(d)
					}
				}
			}
		}
		if msg.FinishReason != "" {
			handler.OnFinished(msg.FinishReason)
		}
		if msg.Usage != nil {
			handler.OnUsageUpdated(*msg.Usage)
		}
	}

	return msg, nil
}

// Stream sends a streaming request, aggregates all chunks, and returns the
// parsed result. eventHandler callbacks are invoked per chunk and on completion.
func (f *Format) Stream(ctx context.Context, req openai.ChatCompletionNewParams, params core.GenerateParams) (*core.Message, error) {

	streamResp := f.client.Chat.Completions.NewStreaming(ctx, req)
	parentID := params.Messages[len(params.Messages)-1].ID
	aggregated := core.NewAssistantMessage(nil)

	var textBuilder, reasoningBuilder strings.Builder
	toolCallAccum := make(map[string]*toolCallEntry) // ID → accumulated entry
	indexToID := make(map[int]string)                // array index → call ID
	handler := params.EventHandler

	for streamResp.Next() {
		if err := streamResp.Err(); err != nil {
			if handler != nil {
				handler.OnError(err)
			}
			return nil, fmt.Errorf("stream: %w", wrapContextError(err))
		}

		// Extract raw tool call deltas BEFORE passing to converter
		// (converter loses raw args strings since they come as JSON fragments)
		rawTCs := extractRawToolCalls(streamResp.Current().RawJSON())
		for _, rtc := range rawTCs {
			if rtc.ID != "" {
				indexToID[rtc.Index] = rtc.ID
			}
			// Resolve ID via index map for deltas that only have index
			if rtc.ID == "" {
				if id, ok := indexToID[rtc.Index]; ok {
					rtc.ID = id
				}
			}
			// Accumulate by resolved ID (skip empty IDs — stray deltas)
			if rtc.ID != "" {
				entry, ok := toolCallAccum[rtc.ID]
				if !ok {
					entry = &toolCallEntry{}
					toolCallAccum[rtc.ID] = entry
				}
				if rtc.Name != "" && entry.Detail.ToolName == "" {
					entry.Detail.ToolName = rtc.Name
					entry.Detail.ID = rtc.ID
					if handler != nil && !entry.notified {
						handler.OnFunctionCall(core.ToolCallDetail{ID: rtc.ID, ToolName: rtc.Name})
						entry.notified = true
					}
				}
				if rtc.Args != "" {
					entry.rawArgs.WriteString(rtc.Args)
				}
			}
		}

		chunk, err := f.converter.ParseStreamChunk(ctx, parentID, streamResp.Current())
		if err != nil {
			if handler != nil {
				handler.OnError(err)
			}
			continue
		}
		if chunk == nil {
			continue
		}

		switch ct := chunk.Content.(type) {
		case core.TextContent:
			textBuilder.WriteString(ct.Text)
			if handler != nil {
				handler.OnReplyChunk(ct.Text)
			}
		case core.ReasoningContent:
			reasoningBuilder.WriteString(ct.Reasoning)
			if handler != nil {
				handler.OnThinkingChunk(ct.Reasoning)
			}
		case core.ToolCallContent, core.CompositeContent:
			// Already handled above via raw JSON extraction
		}

		if chunk.FinishReason != "" {
			aggregated.FinishReason = chunk.FinishReason
			if handler != nil {
				handler.OnFinished(chunk.FinishReason)
			}
		}
		if chunk.Usage != nil {
			aggregated.Usage = chunk.Usage
			if handler != nil {
				handler.OnUsageUpdated(*chunk.Usage)
			}
		}
	}

	if err := streamResp.Err(); err != nil {
		if handler != nil {
			handler.OnError(err)
		}
		return nil, fmt.Errorf("stream: %w", wrapContextError(err))
	}

	// Parse accumulated raw arguments JSON strings into parsed maps
	for _, entry := range toolCallAccum {
		if entry.rawArgs.Len() > 0 {
			entry.Detail.Args = convertArgsJSON(entry.rawArgs.String())
		}
	}

	// Convert accumulated tool call map to ordered slice
	toolCallDetails := make([]core.ToolCallDetail, 0, len(toolCallAccum))
	for _, entry := range toolCallAccum {
		if entry.Detail.ToolName != "" {
			toolCallDetails = append(toolCallDetails, entry.Detail)
		}
	}

	// Build aggregated content
	reasoningText := reasoningBuilder.String()
	text := textBuilder.String()
	switch {
	case len(toolCallDetails) > 0 && reasoningText != "":
		aggregated.Content = core.CompositeContent{
			Parts: []core.Content{
				core.ReasoningContent{Reasoning: reasoningText},
				core.ToolCallContent{Details: toolCallDetails},
			},
		}
	case len(toolCallDetails) > 0:
		aggregated.Content = core.ToolCallContent{Details: toolCallDetails}
	case reasoningText != "" && text != "":
		aggregated.Content = core.CompositeContent{
			Parts: []core.Content{
				core.ReasoningContent{Reasoning: reasoningText},
				core.TextContent{Text: text},
			},
		}
	case reasoningText != "":
		aggregated.Content = core.ReasoningContent{Reasoning: reasoningText}
	default:
		aggregated.Content = core.TextContent{Text: text}
	}

	return &aggregated, nil
}

// ---- Streaming tool call delta accumulation ----

// toolCallEntry accumulates partial deltas for a single streaming tool call.
// Streaming APIs (DeepSeek, OpenAI) send tool calls across multiple chunks:
// first chunk has id+name, subsequent chunks have arguments fragments.
type toolCallEntry struct {
	Detail     core.ToolCallDetail
	rawArgs    strings.Builder // concatenated raw JSON arguments (no reallocation per fragment)
	notified   bool            // OnFunctionCall already fired
}

// rawToolCall represents a single tool call delta from a raw chunk JSON.
// It captures index, id, name, and arguments. Arguments are raw string
// fragments that must be concatenated across chunks to form valid JSON.
type rawToolCall struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Type  string `json:"type"`
	Name  string `json:"-"`
	Args  string `json:"-"`
}

// extractRawToolCalls parses tool call deltas from a raw streaming chunk JSON.
// Returns raw fragments with index/id/name/args for accumulation.
func extractRawToolCalls(rawJSON string) []rawToolCall {
	if rawJSON == "" {
		return nil
	}
	var m struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Index    int    `json:"index"`
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return nil
	}
	var result []rawToolCall
	for _, ch := range m.Choices {
		for _, tc := range ch.Delta.ToolCalls {
			result = append(result, rawToolCall{
				Index: tc.Index,
				ID:    tc.ID,
				Type:  tc.Type,
				Name:  tc.Function.Name,
				Args:  tc.Function.Arguments,
			})
		}
	}
	return result
}

// convertArgsJSON parses a complete JSON arguments string into a map.
func convertArgsJSON(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil
	}
	return m
}

// buildRequest constructs a ChatCompletionNewParams from GenerateParams.
// It does NOT apply provider-specific extensions; those are done by the
// provider layer before calling Generate.
func (f *Format) buildRequest(params core.GenerateParams) (openai.ChatCompletionNewParams, error) {
	messages, err := f.converter.ConvertMessages(params.Messages)
	if err != nil {
		return openai.ChatCompletionNewParams{}, fmt.Errorf("converting messages: %w", err)
	}

	req := openai.ChatCompletionNewParams{
		Messages: messages,
		Model:    openai.ChatModel(f.model),
	}

	if len(params.Tools) > 0 {
		tools, err := f.converter.ConvertTools(params.Tools)
		if err != nil {
			return openai.ChatCompletionNewParams{}, fmt.Errorf("converting tools: %w", err)
		}
		req.Tools = tools
	}

	if params.ReasoningEffort != "" {
		req.ReasoningEffort = shared.ReasoningEffort(params.ReasoningEffort)
	}

	return req, nil
}

// BuildRequest exposes the request builder so provider extensions can
// modify the request before sending (e.g., adding thinking params).
func (f *Format) BuildRequest(params core.GenerateParams) (openai.ChatCompletionNewParams, error) {
	return f.buildRequest(params)
}

// wrapContextError detects context-length errors from the API and wraps them
// with core.ErrContextLengthExceeded so callers can react (compress + retry).
func wrapContextError(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if strings.Contains(s, "context_length_exceeded") ||
		strings.Contains(s, "maximum context length") ||
		strings.Contains(s, "reduce the length of the messages") {
		return fmt.Errorf("%w: %w", core.ErrContextLengthExceeded, err)
	}
	return err
}
