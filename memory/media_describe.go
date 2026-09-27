package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
	coretypes "github.com/B777B2056-2/kugelblitz/core/types"
	"github.com/B777B2056-2/kugelblitz/llm"
)

// MediaDescriber generates text descriptions for multimedia content.
// It supports per-type callers: image descriptions use the image caller,
// audio descriptions use the audio caller. nil caller → metadata-only.
type MediaDescriber struct {
	imageCaller *llm.Caller // nil = metadata only for image
	audioCaller *llm.Caller // nil = metadata only for audio
	prompts     map[constants.MultiModalType]string
}

// NewMediaDescriber creates a MediaDescriber. Both callers may be nil;
// if a type's caller is nil, only the free metadata summary is returned.
func NewMediaDescriber(imageCaller, audioCaller *llm.Caller) *MediaDescriber {
	return &MediaDescriber{
		imageCaller: imageCaller,
		audioCaller: audioCaller,
		prompts:     DefaultDescribePrompts(),
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
func (d *MediaDescriber) Describe(ctx context.Context, detail coretypes.MultiModalDetail) string {
	meta := d.metaSummary(detail)

	caller := d.callerFor(detail.Type)
	if caller == nil {
		return meta
	}

	prompt, ok := d.prompts[detail.Type]
	if !ok || prompt == "" {
		return meta
	}

	llmDesc, err := d.callLLM(ctx, caller, detail, prompt)
	if err == nil && llmDesc != "" {
		return llmDesc
	}

	return meta
}

// callerFor returns the appropriate caller for the given media type.
func (d *MediaDescriber) callerFor(t constants.MultiModalType) *llm.Caller {
	switch t {
	case constants.MultiModalTypeImage:
		return d.imageCaller
	case constants.MultiModalTypeAudio:
		return d.audioCaller
	default:
		return nil
	}
}

// metaSummary builds a free-form text summary from metadata.
func (d *MediaDescriber) metaSummary(detail coretypes.MultiModalDetail) string {
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

// callLLM sends the media + prompt to the given caller for enhanced description.
func (d *MediaDescriber) callLLM(ctx context.Context, caller *llm.Caller, detail coretypes.MultiModalDetail, prompt string) (string, error) {
	imgMsg := coretypes.NewUserMessage(coretypes.MultiModalContent{Detail: detail})
	promptMsg := coretypes.NewUserMessage(coretypes.TextContent{Text: prompt})

	res, err := caller.Call(ctx, llm.Request{
		Messages: []coretypes.Message{imgMsg, promptMsg},
		Mode:     llm.ModeText,
		SpanName: "media.describe",
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Text), nil
}

// BuildMediaMessage wraps a MultiModalDetail into a Message with both
// the text description and the media content. This should be called
// before the message enters SessionMemory.
func BuildMediaMessage(ctx context.Context, d *MediaDescriber, detail coretypes.MultiModalDetail) coretypes.Message {
	desc := d.Describe(ctx, detail)
	return coretypes.NewUserMessage(coretypes.CompositeContent{
		Parts: []coretypes.Content{
			coretypes.TextContent{Text: desc},
			coretypes.MultiModalContent{Detail: detail},
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
