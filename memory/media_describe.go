package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"
)

// MediaDescriber generates text descriptions for multimedia content.
// It supports per-type providers: image descriptions use the image provider,
// audio descriptions use the audio provider. nil provider → metadata-only.
type MediaDescriber struct {
	imageProvider core.ILMProvider // nil = metadata only for image
	audioProvider core.ILMProvider // nil = metadata only for audio
	prompts       map[constants.MultiModalType]string
}

// NewMediaDescriber creates a MediaDescriber. Both providers may be nil;
// if a type's provider is nil, only the free metadata summary is returned.
func NewMediaDescriber(imageProvider, audioProvider core.ILMProvider) *MediaDescriber {
	return &MediaDescriber{
		imageProvider: imageProvider,
		audioProvider: audioProvider,
		prompts:       DefaultDescribePrompts(),
	}
}

// RegisterPrompt sets a custom prompt for a media type. An empty prompt
// removes the entry (causing Describe to always use metaSummary).
func (d *MediaDescriber) RegisterPrompt(t constants.MultiModalType, prompt string) {
	if prompt == "" {
		delete(d.prompts, t)
		return
	}
	d.prompts[t] = prompt
}

// Describe produces a text description of the media.
// Layer 1 (always, free): metadata summary → "[image: image/png 1920×1080]"
// Layer 2 (optional, LLM cost): type-specific provider is called with the prompt.
// Falls back to layer 1 if the type's provider is nil, prompt is not registered,
// or the LLM call fails.
func (d *MediaDescriber) Describe(ctx context.Context, detail core.MultiModalDetail) string {
	meta := d.metaSummary(detail)

	provider := d.providerFor(detail.Type)
	if provider == nil {
		return meta
	}

	prompt, ok := d.prompts[detail.Type]
	if !ok || prompt == "" {
		return meta
	}

	llmDesc, err := d.callLLM(ctx, provider, detail, prompt)
	if err == nil && llmDesc != "" {
		return llmDesc
	}

	return meta
}

// providerFor returns the appropriate provider for the given media type.
func (d *MediaDescriber) providerFor(t constants.MultiModalType) core.ILMProvider {
	switch t {
	case constants.MultiModalTypeImage:
		return d.imageProvider
	case constants.MultiModalTypeAudio:
		return d.audioProvider
	default:
		return nil
	}
}

// metaSummary builds a free-form text summary from metadata.
func (d *MediaDescriber) metaSummary(detail core.MultiModalDetail) string {
	sb := strings.Builder{}
	fmt.Fprintf(&sb, "[%s: %s", detail.Type, detail.MimeType)

	if detail.Meta != nil {
		if w, ok := intFromMeta(detail.Meta, "width"); ok {
			if h, ok := intFromMeta(detail.Meta, "height"); ok {
				fmt.Fprintf(&sb, " %d×%d", w, h)
			}
		}
		if dur, ok := floatFromMeta(detail.Meta, "duration_sec"); ok {
			fmt.Fprintf(&sb, " %.1fs", dur)
		}
	}

	sb.WriteString("]")
	return sb.String()
}

// callLLM sends the media + prompt to the given provider for enhanced description.
func (d *MediaDescriber) callLLM(ctx context.Context, provider core.ILMProvider, detail core.MultiModalDetail, prompt string) (string, error) {
	imgMsg := core.NewUserMessage(core.MultiModalContent{Detail: detail})
	promptMsg := core.NewUserMessage(core.TextContent{Text: prompt})

	resp, err := provider.Generate(ctx, core.GenerateParams{
		Messages: []core.Message{imgMsg, promptMsg},
		Stream:   false,
	})
	if err != nil {
		return "", err
	}

	if tc, ok := resp.Content.(core.TextContent); ok {
		return strings.TrimSpace(tc.Text), nil
	}
	return "", fmt.Errorf("media_describe: unexpected response type %T", resp.Content)
}

// BuildMediaMessage wraps a MultiModalDetail into a Message with both
// the text description and the media content. This should be called
// before the message enters SessionMemory.
func BuildMediaMessage(ctx context.Context, d *MediaDescriber, detail core.MultiModalDetail) core.Message {
	desc := d.Describe(ctx, detail)
	return core.NewUserMessage(core.CompositeContent{
		Parts: []core.Content{
			core.TextContent{Text: desc},
			core.MultiModalContent{Detail: detail},
		},
	})
}

// DefaultDescribePrompts returns the built-in prompts for image, audio, and video.
func DefaultDescribePrompts() map[constants.MultiModalType]string {
	return map[constants.MultiModalType]string{
		constants.MultiModalTypeImage: "请用中文简要描述这张图片的内容和关键信息，不超过200字。",
		constants.MultiModalTypeAudio: "请用中文总结这段音频的内容要点，包括说话人意图和关键信息，不超过200字。",
		constants.MultiModalTypeVideo: "请用中文概述这个视频的画面内容和关键场景，不超过200字。",
	}
}

// ---- helpers ----

func intFromMeta(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func floatFromMeta(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	default:
		return 0, false
	}
}
