package memory

import (
	"context"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMediaDescriber(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	require.NotNil(t, d)
	assert.NotNil(t, d.prompts)

	// Should have prompts for registered types
	assert.Contains(t, d.prompts, constants.MultiModalTypeImage)
	assert.Contains(t, d.prompts, constants.MultiModalTypeAudio)
	assert.Contains(t, d.prompts, constants.MultiModalTypeVideo)
}

func TestMediaDescriber_MetaSummaryImage(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeImage,
		MimeType: "image/png",
		Meta:     map[string]any{"width": 1920, "height": 1080},
	})

	assert.Contains(t, desc, "image")
	assert.Contains(t, desc, "image/png")
	// metadata-based description should include dimensions (formatted in a readable way)
}

func TestMediaDescriber_MetaSummaryAudio(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeAudio,
		MimeType: "audio/mp3",
		Meta:     map[string]any{"duration_sec": 120.5, "sample_rate": float64(44100), "channels": 2},
	})

	assert.Contains(t, desc, "audio")
	assert.Contains(t, desc, "audio/mp3")
}

func TestMediaDescriber_MetaSummaryVideo(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeVideo,
		MimeType: "video/mp4",
		Meta:     map[string]any{"width": 1920, "height": 1080, "duration_sec": 45.0, "fps": float64(30)},
	})

	assert.Contains(t, desc, "video")
	assert.Contains(t, desc, "video/mp4")
}

func TestMediaDescriber_MetaSummaryMinimalMeta(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	// No Meta at all — should still produce a minimal description
	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeImage,
		MimeType: "image/jpeg",
	})

	assert.Contains(t, desc, "image/jpeg")
	assert.NotEmpty(t, desc)
}

func TestMediaDescriber_MetaSummaryNilMeta(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeImage,
		MimeType: "image/webp",
		Meta:     nil,
	})
	assert.Contains(t, desc, "image/webp")
}

func TestMediaDescriber_WithProvider_NoPrompt(t *testing.T) {
	// Provider present but no prompt registered for this type → falls back to metaSummary
	d := NewMediaDescriber(nil, nil)
	d.RegisterPrompt(constants.MultiModalTypeImage, "") // empty prompt → fall through

	desc := d.Describe(context.Background(), core.MultiModalDetail{
		Type:     constants.MultiModalTypeImage,
		MimeType: "image/png",
	})
	assert.Contains(t, desc, "image/png") // meta summary fallback
}

func TestMediaDescriber_RegisterPrompt(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	d.RegisterPrompt(constants.MultiModalTypeImage, "自定义图片描述 prompt")
	assert.Equal(t, "自定义图片描述 prompt", d.prompts[constants.MultiModalTypeImage])
}

func TestBuildMediaMessage(t *testing.T) {
	d := NewMediaDescriber(nil, nil)
	detail := core.MultiModalDetail{
		ID:       "img-1",
		Type:     constants.MultiModalTypeImage,
		MimeType: "image/png",
		Meta:     map[string]any{"width": 100, "height": 100},
	}

	msg := BuildMediaMessage(context.Background(), d, detail)

	assert.NotEmpty(t, msg.ID)
	assert.Equal(t, constants.RoleUser, msg.Role)

	cc, ok := msg.Content.(core.CompositeContent)
	require.True(t, ok)
	require.Len(t, cc.Parts, 2)

	// First part: text description
	textPart, ok := cc.Parts[0].(core.TextContent)
	require.True(t, ok)
	assert.NotEmpty(t, textPart.Text)
	assert.Contains(t, textPart.Text, "image/png")

	// Second part: multimodal content
	mmPart, ok := cc.Parts[1].(core.MultiModalContent)
	require.True(t, ok)
	assert.Equal(t, "img-1", mmPart.Detail.ID)
}
