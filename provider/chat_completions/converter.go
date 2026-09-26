//nolint:staticcheck
package chat_completions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/utils"

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

func (c *Converter) ConvertMessages(messages []coretypes.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
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

func (c *Converter) convertMessage(message coretypes.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch message.Role {
	case constants.RoleSystem:
		text, err := extractText(message.Content)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("system message: %w", err)
		}
		return openai.SystemMessage(text), nil

	case constants.RoleUser:
		return c.convertUserMessage(message)

	case constants.RoleAssistant:
		return c.convertAssistantMessage(message)

	default:
		return openai.ChatCompletionMessageParamUnion{}, fmt.Errorf("unknown role: %s", message.Role)
	}
}

func extractText(content coretypes.Content) (string, error) {
	if content == nil {
		return "", fmt.Errorf("content is nil")
	}
	switch ct := content.(type) {
	case coretypes.TextContent:
		return ct.Text, nil
	default:
		return "", fmt.Errorf("expected TextContent, got %T", content)
	}
}

// convertUserMessage converts a user message, supporting text, multimodal, and composite content.
func (c *Converter) convertUserMessage(message coretypes.Message) (openai.ChatCompletionMessageParamUnion, error) {
	switch ct := message.Content.(type) {
	case coretypes.TextContent:
		return openai.UserMessage(ct.Text), nil

	case coretypes.MultiModalContent:
		part, err := c.mediaContentPart(ct.Detail)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.ChatCompletionMessageParamUnion{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfArrayOfContentParts: []openai.ChatCompletionContentPartUnionParam{part},
				},
			},
		}, nil

	case coretypes.CompositeContent:
		parts, err := c.convertUserContentParts(ct.Parts)
		if err != nil {
			return openai.ChatCompletionMessageParamUnion{}, err
		}
		return openai.ChatCompletionMessageParamUnion{
			OfUser: &openai.ChatCompletionUserMessageParam{
				Content: openai.ChatCompletionUserMessageParamContentUnion{
					OfArrayOfContentParts: parts,
				},
			},
		}, nil

	default:
		return openai.ChatCompletionMessageParamUnion{},
			fmt.Errorf("unsupported user content type: %T", message.Content)
	}
}

// mediaContentPart maps a MultiModalDetail to the correct OpenAI ContentPart based on media type.
func (c *Converter) mediaContentPart(detail coretypes.MultiModalDetail) (openai.ChatCompletionContentPartUnionParam, error) {
	dataURL := fmt.Sprintf("data:%s;base64,%s", detail.MimeType, detail.Base64)

	switch detail.Type {
	case constants.MultiModalTypeImage:
		return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL:    dataURL,
			Detail: "auto",
		}), nil

	case constants.MultiModalTypeAudio:
		return openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
			Data:   detail.Base64,
			Format: audioFormat(detail.MimeType),
		}), nil

	case constants.MultiModalTypeVideo:
		// No native video ContentPart — send first frame as image
		return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL:    dataURL,
			Detail: "low",
		}), nil

	default:
		return openai.ChatCompletionContentPartUnionParam{},
			fmt.Errorf("unsupported media type: %s", detail.Type)
	}
}

// convertUserContentParts converts a slice of Content to OpenAI ContentPart array.
func (c *Converter) convertUserContentParts(parts []coretypes.Content) ([]openai.ChatCompletionContentPartUnionParam, error) {
	var result []openai.ChatCompletionContentPartUnionParam
	for _, p := range parts {
		switch pt := p.(type) {
		case coretypes.TextContent:
			result = append(result, openai.TextContentPart(pt.Text))
		case coretypes.MultiModalContent:
			part, err := c.mediaContentPart(pt.Detail)
			if err != nil {
				return nil, err
			}
			result = append(result, part)
		default:
			return nil, fmt.Errorf("unsupported content part type: %T", p)
		}
	}
	return result, nil
}

// audioFormat extracts the format suffix from a MIME type for OpenAI audio input.
// e.g. "audio/wav" → "wav", "audio/mpeg" → "mpeg".
func audioFormat(mimeType string) string {
	if after, ok := strings.CutPrefix(mimeType, "audio/"); ok {
		return after
	}
	return "wav" // safe fallback
}

func (c *Converter) convertAssistantMessage(message coretypes.Message) (openai.ChatCompletionMessageParamUnion, error) {
	if message.Content == nil {
		return openai.AssistantMessage(""), nil
	}

	switch ct := message.Content.(type) {
	case coretypes.TextContent:
		return openai.AssistantMessage(ct.Text), nil

	case coretypes.ReasoningContent:
		return openai.AssistantMessage(ct.Reasoning), nil

	case coretypes.ToolCallContent:
		return buildToolCallParam(ct.Details, ""), nil

	case coretypes.CompositeContent:
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

func buildToolCallParam(details []coretypes.ToolCallDetail, reasoningContent string) openai.ChatCompletionMessageParamUnion {
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

func (c *Converter) convertCompositeAssistant(ct coretypes.CompositeContent) (openai.ChatCompletionMessageParamUnion, error) {
	var textContent string
	var reasoningContent string
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam

	for _, part := range ct.Parts {
		switch p := part.(type) {
		case coretypes.TextContent:
			textContent = p.Text
		case coretypes.ReasoningContent:
			reasoningContent = p.Reasoning
		case coretypes.ToolCallContent:
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

// convertToolResults converts multiple tool results into separate OpenAI tool messages.
func (c *Converter) convertToolResults(message coretypes.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	ct, ok := message.Content.(coretypes.ToolResultContent)
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

func (c *Converter) ConvertTools(tools []coretypes.ToolDefinition) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	var result []openai.ChatCompletionToolUnionParam
	for _, tool := range tools {
		desc := tool.Description
		if tool.OutputSchema != nil {
			desc += "\nReturns: " + formatOutputSchema(tool.OutputSchema)
		}
		// Structured outputs ("Strict") are only valid when the schema satisfies
		// OpenAI's requirements. Forcing Strict:true on a non-compliant schema
		// makes the API reject the request, so enable it selectively (B18).
		strict := param.Opt[bool]{}
		if isStrictCompliant(tool.JSONSchema) {
			strict = param.NewOpt(true)
		}
		result = append(result, openai.ChatCompletionFunctionTool(
			openai.FunctionDefinitionParam{
				Name:        tool.Name,
				Strict:      strict,
				Description: param.NewOpt(desc),
				Parameters:  openai.FunctionParameters(tool.JSONSchema),
			},
		))
	}
	return result, nil
}

// isStrictCompliant reports whether a JSON schema satisfies OpenAI's structured
// outputs ("Strict") requirements: root type object, additionalProperties set
// to false, and every property listed in "required".
func isStrictCompliant(schema map[string]any) bool {
	if typ, _ := schema["type"].(string); typ != "object" {
		return false
	}
	if ap, ok := schema["additionalProperties"].(bool); !ok || ap {
		return false
	}
	props, _ := schema["properties"].(map[string]any)
	requiredSet := map[string]bool{}
	if required, _ := schema["required"].([]any); required != nil {
		for _, r := range required {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}
	for name := range props {
		if !requiredSet[name] {
			return false
		}
	}
	return true
}

// formatOutputSchema produces a concise summary of an output schema.
// e.g., {"type":"object","properties":{"status":{"type":"string"}}} → "{status: string}"
func formatOutputSchema(schema map[string]any) string {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return "object"
	}
	var fields []string
	for name, raw := range props {
		if prop, ok := raw.(map[string]any); ok {
			typ, _ := prop["type"].(string)
			desc, _ := prop["description"].(string)
			f := fmt.Sprintf("%s: %s", name, typ)
			if desc != "" {
				f += fmt.Sprintf(" (%s)", desc)
			}
			fields = append(fields, f)
		}
	}
	result := "{" + strings.Join(fields, ", ") + "}"
	// All tools may return an error field on failure
	result += " | on error: {error: string}"
	return result
}

// --- Response parsing (API → core) ---

func (c *Converter) ParseResponse(ctx context.Context, parentID string, raw openai.ChatCompletionMessage) (*coretypes.Message, error) {
	reasoningContent := parseReasoningFromRaw(raw.RawJSON())
	msg := coretypes.NewAssistantMessage(nil)

	hasText := raw.Content != ""
	hasToolCalls := len(raw.ToolCalls) > 0
	hasReasoning := reasoningContent != ""
	hasAudio := raw.Audio.ID != ""

	switch {
	case hasToolCalls:
		var parts []coretypes.Content
		if hasReasoning {
			parts = append(parts, coretypes.ReasoningContent{Reasoning: reasoningContent})
		}
		if hasText {
			parts = append(parts, coretypes.TextContent{Text: raw.Content})
		}
		var details []coretypes.ToolCallDetail
		for _, tc := range raw.ToolCalls {
			details = append(details, coretypes.ToolCallDetail{
				ID:       tc.ID,
				ToolName: tc.Function.Name,
				Args:     utils.ConvertJSONStringToMap(tc.Function.Arguments),
			})
		}
		parts = append(parts, coretypes.ToolCallContent{Details: details})
		if len(parts) > 1 {
			msg.Content = coretypes.CompositeContent{Parts: parts}
		} else {
			msg.Content = parts[0]
		}

	case hasText && hasReasoning:
		msg.Content = coretypes.CompositeContent{
			Parts: []coretypes.Content{
				coretypes.ReasoningContent{Reasoning: reasoningContent},
				coretypes.TextContent{Text: raw.Content},
			},
		}

	case hasText:
		msg.Content = coretypes.TextContent{Text: raw.Content}

	case hasAudio:
		msg.Content = coretypes.MultiModalContent{
			Detail: coretypes.MultiModalDetail{
				ID:     raw.Audio.ID,
				Type:   constants.MultiModalTypeAudio,
				Base64: raw.Audio.Data,
			},
		}

	case hasReasoning:
		msg.Content = coretypes.ReasoningContent{Reasoning: reasoningContent}

	default:
		return nil, fmt.Errorf("empty completion message")
	}

	return &msg, nil
}

// --- Stream chunk parsing ---

func (c *Converter) ParseStreamChunk(ctx context.Context, parentID string, raw openai.ChatCompletionChunk) (*coretypes.Message, error) {
	if len(raw.Choices) == 0 {
		return nil, nil
	}

	reasoningDelta := parseReasoningFromChunkRaw(raw.RawJSON())
	msg := coretypes.NewAssistantMessage(nil)

	if raw.Usage.TotalTokens > 0 {
		msg.Usage = &coretypes.Usage{
			TotalTokens:     raw.Usage.TotalTokens,
			InputTokens:     raw.Usage.PromptTokens,
			CachedTokens:    raw.Usage.PromptTokensDetails.CachedTokens,
			ReasoningTokens: raw.Usage.CompletionTokensDetails.ReasoningTokens,
			OutputTokens:    raw.Usage.CompletionTokens,
		}
	}
	if raw.Choices[0].FinishReason != "" {
		msg.FinishReason = raw.Choices[0].FinishReason
	}

	ch := raw.Choices[0]
	hasText := ch.Delta.Content != ""
	hasToolCall := len(ch.Delta.ToolCalls) > 0
	hasReasoning := reasoningDelta != ""

	switch {
	case hasReasoning && hasText:
		msg.Content = coretypes.CompositeContent{
			Parts: []coretypes.Content{
				coretypes.ReasoningContent{Reasoning: reasoningDelta},
				coretypes.TextContent{Text: ch.Delta.Content},
			},
		}
	case hasReasoning:
		msg.Content = coretypes.ReasoningContent{Reasoning: reasoningDelta}
	case hasText:
		msg.Content = coretypes.TextContent{Text: ch.Delta.Content}
	case hasToolCall:
		var details []coretypes.ToolCallDetail
		for _, tc := range ch.Delta.ToolCalls {
			details = append(details, coretypes.ToolCallDetail{
				ID:       tc.ID,
				ToolName: tc.Function.Name,
				Args:     utils.ConvertJSONStringToMap(tc.Function.Arguments),
			})
		}
		msg.Content = coretypes.ToolCallContent{Details: details}
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
