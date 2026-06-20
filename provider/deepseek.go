package provider

import (
	"context"

	"kugelblitz/core"
	"kugelblitz/provider/chat_completions"

	"github.com/openai/openai-go/v3/shared"
)

const deepseekBaseURL = "https://api.deepseek.com"

// DeepSeek creates a Provider configured for the DeepSeek API.
// DeepSeek extends the standard Chat Completions protocol with:
//   - thinking: {type: "enabled"|"disabled"} in the request body
//   - reasoning_content in assistant messages and stream deltas
//
// Use GenerateParams.EnableThinking and ReasoningEffort to control
// the thinking behavior.
func DeepSeek(apiKey, baseURL, model string) *Provider {
	if baseURL == "" {
		baseURL = deepseekBaseURL
	}
	format := &deepSeekFormat{
		Format: chat_completions.NewFormat(apiKey, baseURL, model),
	}
	return New(Config{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		APIType: APITypeChatCompletions,
	}, format)
}

// deepSeekFormat wraps chat_completions.Format to inject DeepSeek-specific
// request parameters (thinking) before each API call.
type deepSeekFormat struct {
	*chat_completions.Format
}

func (f *deepSeekFormat) Generate(ctx context.Context, params core.GenerateParams) (*core.Message, error) {
	req, err := f.Format.BuildRequest(params)
	if err != nil {
		return nil, err
	}

	// Inject DeepSeek-specific: thinking parameter
	if params.EnableThinking != nil {
		thinkingType := "disabled"
		if *params.EnableThinking {
			thinkingType = "enabled"
		}
		req.SetExtraFields(map[string]any{
			"thinking": map[string]any{"type": thinkingType},
		})
	}

	// Map reasoning_effort (DeepSeek supports "high" and "max")
	if params.ReasoningEffort != "" {
		req.ReasoningEffort = shared.ReasoningEffort(params.ReasoningEffort)
	}

	// Delegate to the Chat Completions format with the modified request
	if params.Stream {
		return f.Format.Stream(ctx, req, params)
	}
	return f.Format.Block(ctx, req, params)
}

// Ensure deepSeekFormat satisfies APIFormat.
var _ APIFormat = (*deepSeekFormat)(nil)
