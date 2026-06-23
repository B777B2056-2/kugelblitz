package provider

import (
	"github.com/B777B2056-2/kugelblitz/provider/chat_completions"
)

const openaiBaseURL = "https://api.openai.com/v1"

// OpenAI creates a Provider configured for the OpenAI API.
// It uses the standard Chat Completions protocol with no extensions.
func OpenAI(apiKey, baseURL, model string) *Provider {
	if baseURL == "" {
		baseURL = openaiBaseURL
	}
	format := chat_completions.NewFormat(apiKey, baseURL, model)
	return New(Config{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
		APIType: APITypeChatCompletions,
	}, format)
}
