package provider

// APIType identifies the wire protocol used by a provider.
type APIType string

const (
	APITypeChatCompletions APIType = "chat_completions"
	// Future: APITypeResponses, APITypeAnthropicMessages, etc.
)

// Config holds all configuration for connecting to an LLM provider.
// The APIKey and BaseURL define who to call; Model specifies which model;
// APIType determines how messages are serialized.
type Config struct {
	APIKey  string
	BaseURL string
	Model   string
	APIType APIType
}
