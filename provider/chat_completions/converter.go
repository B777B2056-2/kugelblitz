package chat_completions

import (
	"context"
	"encoding/json"
	"fmt"
	"kugelblitz/constants"
	"kugelblitz/core"
	"kugelblitz/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

// Converter converts between core types and OpenAI Chat Completions API types.
// It handles the standard Chat Completions wire format; provider-specific
// extensions (e.g., reasoning_content) are applied at the provider level.
type Converter struct{}

func NewConverter() *Converter {
	return &Converter{}
}

// --- Messages (core → API) ---

func (c *Converter) ConvertMessages(messages []core.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	var result []openai.ChatCompletionMessageParamUnion
	for _, msg := range messages {
		if msg.Role == constants.RoleTool {
			// Expand multi-result tool messages into one OpenAI message per result
			expanded, err := c.convertToolResults(msg)
			if err != nil {
				return nil, err
			}
			result = append(result, expanded...)
		} else {
			param, err := c.convertMessage(msg)
			if err != nil {
				return nil, err
			}
			result = append(result, param)
		}
	}
	return result, nil
}

func (c *Converter) convertMessage(message core.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case constants.RoleSystem:
		text, err := extractText(message.Content)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("system message: %w", err)
		}
		return openai.SystemMessage(text), nil

	case constants.RoleUser:
		text, err := extractText(message.Content)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("user message: %w", err)
		}
		return openai.UserMessage(text), nil

	case constants.RoleAssistant:
		return c.convertAssistantMessage(message)

	case constants.RoleTool:
		return c.convertToolMessage(message)

	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("unknown role: %s", message.Role)
	}
}

func extractText(content core.Content) (string, error) {
	if content == nil {
		return "", fmt.Errorf("content is nil")
	}
	switch ct := content.(type) {
	case core.TextContent:
		return ct.Text, nil
	default:
		return "", fmt.Errorf("expected TextContent, got %T", content)
	}
}

func (c *Converter) convertAssistantMessage(message core.Message) (openai.ChatCompletionMessageParamUnion, error) {
	if message.Content == nil {
		return openai.AssistantMessage(""), nil
	}

	switch ct := message.Content.(type) {
	case core.TextContent:
		return openai.AssistantMessage(ct.Text), nil

	case core.ReasoningContent:
		return openai.AssistantMessage(ct.Reasoning), nil

	case core.ToolCallContent:
		return buildToolCallParam(ct.Details, ""), nil

	case core.CompositeContent:
		return c.convertCompositeAssistant(ct)

	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("unsupported assistant content: %T", message.Content)
	}
}

// ApplyReasoningContent adds reasoning_content to an assistant message param.
// Provider extensions (e.g., DeepSeek) call this after building the base param.
func ApplyReasoningContent(param *openai.ChatCompletionAssistantMessageParam, reasoning string) {
	if reasoning != "" {
		param.SetExtraFields(map[string]any{
			"reasoning_content": reasoning,
		})
	}
}

func buildToolCallParam(details []core.ToolCallDetail, reasoningContent string) openai.ChatCompletionMessageParamUnion {
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
	for _, d := range details {
		toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
				ID: d.ID,
				Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      d.ToolName,
					Arguments: utils.ConvertMapToJSONString(d.Args),
				},
			},
		})
	}
	assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
	if reasoningContent != "" {
		assistant.SetExtraFields(map[string]any{"reasoning_content": reasoningContent})
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}
}

func (c *Converter) convertCompositeAssistant(ct core.CompositeContent) (openai.ChatCompletionMessageParamUnion, error) {
	var textContent string
	var reasoningContent string
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam

	for _, part := range ct.Parts {
		switch p := part.(type) {
		case core.TextContent:
			textContent = p.Text
		case core.ReasoningContent:
			reasoningContent = p.Reasoning
		case core.ToolCallContent:
			for _, d := range p.Details {
				toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: d.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      d.ToolName,
							Arguments: utils.ConvertMapToJSONString(d.Args),
						},
					},
				})
			}
		}
	}

	if len(toolCalls) > 0 {
		assistant := openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
		if textContent != "" {
			assistant.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: param.NewOpt(textContent),
			}
		}
		if reasoningContent != "" {
			assistant.SetExtraFields(map[string]any{"reasoning_content": reasoningContent})
		}
		return openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant}, nil
	}

	return openai.AssistantMessage(textContent), nil
}

func (c *Converter) convertToolMessage(message core.Message) (openai.ChatCompletionMessageParamUnion, error) {
	ct, ok := message.Content.(core.ToolResultContent)
	if !ok {
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("expected ToolResultContent, got %T", message.Content)
	}
	if len(ct.Results) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("tool message has no results")
	}
	// Multiple tool results need separate tool messages per OpenAI/DeepSeek API spec.
	// This method returns only the first result; the caller (ConvertMessages) is
	// expected to call this once per result.
	r := ct.Results[0]
	return openai.ToolMessage(utils.ConvertMapToJSONString(r.Outputs), r.ToolCallID), nil
}

// convertToolResults converts multiple tool results into separate OpenAI tool messages.
func (c *Converter) convertToolResults(message core.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	ct, ok := message.Content.(core.ToolResultContent)
	if !ok {
		return nil, fmt.Errorf("expected ToolResultContent, got %T", message.Content)
	}
	var results []openai.ChatCompletionMessageParamUnion
	for _, r := range ct.Results {
		results = append(results, openai.ToolMessage(utils.ConvertMapToJSONString(r.Outputs), r.ToolCallID))
	}
	return results, nil
}

// --- Tools (core → API) ---

func (c *Converter) ConvertTools(tools []core.ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	var result []openai.ChatCompletionToolUnionParam
	for _, tool := range tools {
		result = append(result, openai.ChatCompletionFunctionTool(
			openai.FunctionDefinitionParam{
				Name:        tool.Name,
				Strict:      param.NewOpt(true),
				Description: param.NewOpt(tool.Description),
				Parameters:  openai.FunctionParameters(tool.JsonSchema),
			},
		))
	}
	return result, nil
}

// --- Response parsing (API → core) ---

func (c *Converter) ParseResponse(ctx context.Context, parentID string, raw openai.ChatCompletionMessage) (*core.Message, error) {
	reasoningContent := parseReasoningFromRaw(raw.RawJSON())
	msg := core.NewAssistantMessage(parentID, nil)

	hasText := raw.Content != ""
	hasToolCalls := len(raw.ToolCalls) > 0
	hasReasoning := reasoningContent != ""
	hasAudio := raw.Audio.ID != ""

	switch {
	case hasToolCalls:
		var parts []core.Content
		if hasReasoning {
			parts = append(parts, core.ReasoningContent{Reasoning: reasoningContent})
		}
		if hasText {
			parts = append(parts, core.TextContent{Text: raw.Content})
		}
		var details []core.ToolCallDetail
		for _, tc := range raw.ToolCalls {
			details = append(details, core.ToolCallDetail{
				ID:       tc.ID,
				ToolName: tc.Function.Name,
				Args:     utils.ConvertJSONStringToMap(tc.Function.Arguments),
			})
		}
		parts = append(parts, core.ToolCallContent{Details: details})
		if len(parts) > 1 {
			msg.Content = core.CompositeContent{Parts: parts}
		} else {
			msg.Content = parts[0]
		}

	case hasText && hasReasoning:
		msg.Content = core.CompositeContent{
			Parts: []core.Content{
				core.ReasoningContent{Reasoning: reasoningContent},
				core.TextContent{Text: raw.Content},
			},
		}

	case hasText:
		msg.Content = core.TextContent{Text: raw.Content}

	case hasAudio:
		msg.Content = core.MultiModalContent{
			Detail: core.MultiModalDetail{
				ID:     raw.Audio.ID,
				Type:   constants.MultiModalTypeAudio,
				Base64: raw.Audio.Data,
			},
		}

	case hasReasoning:
		msg.Content = core.ReasoningContent{Reasoning: reasoningContent}

	default:
		return nil, fmt.Errorf("empty completion message")
	}

	return &msg, nil
}

// --- Stream chunk parsing ---

func (c *Converter) ParseStreamChunk(ctx context.Context, parentID string, raw openai.ChatCompletionChunk) (*core.Message, error) {
	if len(raw.Choices) == 0 {
		return nil, nil
	}

	reasoningDelta := parseReasoningFromChunkRaw(raw.RawJSON())
	msg := core.NewAssistantMessage(parentID, nil)

	if raw.Usage.TotalTokens > 0 {
		msg.Usage = &core.Usage{
			TotalTokens:     raw.Usage.TotalTokens,
			InputTokens:     raw.Usage.PromptTokens,
			CachedTokens:    raw.Usage.PromptTokensDetails.CachedTokens,
			ReasoningTokens: raw.Usage.CompletionTokensDetails.ReasoningTokens,
			OutputTokens:    raw.Usage.CompletionTokens,
		}
	}
	if raw.Choices[0].FinishReason != "" {
		msg.FinishReason = string(raw.Choices[0].FinishReason)
	}

	ch := raw.Choices[0]
	hasText := ch.Delta.Content != ""
	hasToolCall := len(ch.Delta.ToolCalls) > 0
	hasReasoning := reasoningDelta != ""

	switch {
	case hasReasoning && hasText:
		msg.Content = core.CompositeContent{
			Parts: []core.Content{
				core.ReasoningContent{Reasoning: reasoningDelta},
				core.TextContent{Text: ch.Delta.Content},
			},
		}
	case hasReasoning:
		msg.Content = core.ReasoningContent{Reasoning: reasoningDelta}
	case hasText:
		msg.Content = core.TextContent{Text: ch.Delta.Content}
	case hasToolCall:
		var details []core.ToolCallDetail
		for _, tc := range ch.Delta.ToolCalls {
			details = append(details, core.ToolCallDetail{
				ID:       tc.ID,
				ToolName: tc.Function.Name,
				Args:     utils.ConvertJSONStringToMap(tc.Function.Arguments),
			})
		}
		msg.Content = core.ToolCallContent{Details: details}
	default:
		return nil, nil
	}

	return &msg, nil
}

// --- Raw JSON reasoning_content extraction ---

func parseReasoningFromRaw(rawJSON string) string {
	if rawJSON == "" {
		return ""
	}
	var m struct {
		ReasoningContent string `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return ""
	}
	return m.ReasoningContent
}

func parseReasoningFromChunkRaw(rawJSON string) string {
	if rawJSON == "" {
		return ""
	}
	var m struct {
		Choices []struct {
			Delta struct {
				ReasoningContent string `json:"reasoning_content"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &m); err != nil {
		return ""
	}
	if len(m.Choices) == 0 {
		return ""
	}
	return m.Choices[0].Delta.ReasoningContent
}
