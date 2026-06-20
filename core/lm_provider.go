package core

import "context"

// ModelEventHandler receives model response events during both streaming and
// non-streaming generation. OnThinkingChunk and OnReplyChunk are only called
// in streaming mode; all other methods are called in both modes.
//
// Implementations may provide only the methods they care about;
// nil interface checks are done by the provider before calling.
type ModelEventHandler interface {
	OnThinkingChunk(chunk string)
	OnReplyChunk(chunk string)
	OnFunctionCall(toolCallDetail ToolCallDetail)
	OnFinished(finishReason string)
	OnUsageUpdated(usage Usage)
	OnError(err error)
}

// Reasoning effort levels for models that support thinking mode.
// These map to provider-specific values where applicable.
const (
	ReasoningEffortNone    = "none"
	ReasoningEffortMinimal = "minimal"
	ReasoningEffortLow     = "low"
	ReasoningEffortMedium  = "medium"
	ReasoningEffortHigh    = "high"
	ReasoningEffortXHigh   = "xhigh"
	ReasoningEffortMax     = "max"
)

// GenerateParams bundles all inputs for a single provider call.
type GenerateParams struct {
	Messages      []Message
	Tools         []ToolDefinition
	Stream        bool
	EventHandler  ModelEventHandler // event callbacks for both stream and block modes

	// EnableThinking controls whether the model spends tokens on internal reasoning.
	// nil = provider default; true = enabled; false = disabled.
	// Only supported by models with thinking/CoT capability (DeepSeek V3.1+, o-series).
	EnableThinking *bool

	// ReasoningEffort controls the intensity of reasoning when thinking is enabled.
	// Valid values: "none", "minimal", "low", "medium", "high", "xhigh", "max".
	// Use the ReasoningEffort* constants. Empty string = provider default.
	ReasoningEffort string
}

// ILMProvider is the interface all language model providers must implement.
type ILMProvider interface {
	Generate(ctx context.Context, params GenerateParams) (*Message, error)
}
