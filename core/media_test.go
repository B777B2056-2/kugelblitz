package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time check: all validators satisfy interface.
var (
	_ MediaTypeValidator = &imageValidator{}
	_ MediaTypeValidator = &audioValidator{}
	_ MediaTypeValidator = &videoValidator{}
)

func TestNewDefaultRegistry(t *testing.T) {
	reg := NewDefaultRegistry()
	assert.NotNil(t, reg)

	v := reg.Validator(constants.MultiModalTypeImage)
	require.NotNil(t, v)
	assert.Equal(t, constants.MultiModalTypeImage, v.Type())

	v = reg.Validator(constants.MultiModalTypeAudio)
	require.NotNil(t, v)

	v = reg.Validator(constants.MultiModalTypeVideo)
	require.NotNil(t, v)

	// 未注册的类型返回 nil
	v = reg.Validator(constants.MultiModalTypePDF)
	assert.Nil(t, v)
}

func TestImageValidator_MIMEWhitelist(t *testing.T) {
	v := &imageValidator{}
	allowed := v.MIMEWhitelist()
	assert.Contains(t, allowed, "image/png")
	assert.Contains(t, allowed, "image/jpeg")
	assert.Contains(t, allowed, "image/gif")
	assert.Contains(t, allowed, "image/webp")
}

func TestImageValidator_MaxSize(t *testing.T) {
	v := &imageValidator{}
	assert.Equal(t, int64(20*1024*1024), v.MaxSize())
}

func TestImageValidator_ExtractMeta(t *testing.T) {
	v := &imageValidator{}

	// 创建 1×1 PNG
	buf := new(bytes.Buffer)
	require.NoError(t, png.Encode(buf, image.NewRGBA(image.Rect(0, 0, 1, 1))))

	meta := v.ExtractMeta(buf.Bytes())
	require.NotNil(t, meta)
	assert.Equal(t, 1, meta["width"])
	assert.Equal(t, 1, meta["height"])
}

func TestImageValidator_ExtractMeta_InvalidData(t *testing.T) {
	v := &imageValidator{}
	meta := v.ExtractMeta([]byte("not an image"))
	assert.Nil(t, meta)
}

func TestAudioValidator_MIMEWhitelist(t *testing.T) {
	v := &audioValidator{}
	allowed := v.MIMEWhitelist()
	assert.Contains(t, allowed, "audio/mpeg")
	assert.Contains(t, allowed, "audio/wav")
	assert.Contains(t, allowed, "audio/mp4")
	assert.Contains(t, allowed, "audio/webm")
}

func TestAudioValidator_ExtractMeta_ReturnsNil(t *testing.T) {
	v := &audioValidator{}
	// Audio metadata extraction not yet implemented — should return nil gracefully
	assert.Nil(t, v.ExtractMeta([]byte("fake audio data")))
}

func TestVideoValidator_ExtractMeta_ReturnsNil(t *testing.T) {
	v := &videoValidator{}
	assert.Nil(t, v.ExtractMeta([]byte("fake video data")))
}

func TestMediaPreprocessor_Normalize_FromFile(t *testing.T) {
	// 创建临时 PNG 文件
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.png")
	buf := new(bytes.Buffer)
	require.NoError(t, png.Encode(buf, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	require.NoError(t, os.WriteFile(imgPath, buf.Bytes(), 0644))

	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	detail, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:   "img-1",
		Type: constants.MultiModalTypeImage,
		Path: imgPath,
	})
	require.NoError(t, err)

	assert.Equal(t, "img-1", detail.ID)
	assert.Equal(t, "image/png", detail.MimeType)
	assert.NotEmpty(t, detail.Base64)

	// 验证 Base64 可解码且内容一致
	decoded, err := base64.StdEncoding.DecodeString(detail.Base64)
	require.NoError(t, err)
	assert.Equal(t, buf.Bytes(), decoded)

	// Meta 应包含宽高
	require.NotNil(t, detail.Meta)
	assert.Equal(t, 2, detail.Meta["width"])
	assert.Equal(t, 2, detail.Meta["height"])
}

func TestMediaPreprocessor_Normalize_FromBase64(t *testing.T) {
	buf := new(bytes.Buffer)
	require.NoError(t, png.Encode(buf, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())

	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	detail, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:     "img-1",
		Type:   constants.MultiModalTypeImage,
		Base64: b64,
	})
	require.NoError(t, err)

	assert.Equal(t, "image/png", detail.MimeType)
	assert.Equal(t, b64, detail.Base64) // 已有效的 base64 保持不变
}

func TestMediaPreprocessor_Normalize_InvalidMIME(t *testing.T) {
	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	_, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:     "img-1",
		Type:   constants.MultiModalTypeImage,
		Base64: base64.StdEncoding.EncodeToString([]byte("not an image at all")),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MIME")
}

func TestMediaPreprocessor_Normalize_FileTooLarge(t *testing.T) {
	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	// 创建一个超过 image 类型限制的 fake 数据 (1MB > 0)
	// 重写 image validator 的 max size 为 100 bytes
	preprocessor.validators = &MediaValidatorRegistry{
		validators: map[constants.MultiModalType]MediaTypeValidator{
			constants.MultiModalTypeImage: &customImageValidator{maxSize: 100},
		},
	}

	buf := new(bytes.Buffer)
	require.NoError(t, png.Encode(buf, image.NewRGBA(image.Rect(0, 0, 100, 100))))
	// 100×100 PNG > 100 bytes

	_, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:     "img-1",
		Type:   constants.MultiModalTypeImage,
		Base64: base64.StdEncoding.EncodeToString(buf.Bytes()),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "size")
}

func TestMediaPreprocessor_Normalize_UnregisteredType(t *testing.T) {
	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	_, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:     "pdf-1",
		Type:   constants.MultiModalTypePDF,
		Base64: base64.StdEncoding.EncodeToString([]byte("fake pdf data")),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validator")
}

func TestMediaPreprocessor_Normalize_MissingPathAndBase64(t *testing.T) {
	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	_, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:   "img-1",
		Type: constants.MultiModalTypeImage,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestMediaPreprocessor_Normalize_NonExistentFile(t *testing.T) {
	preprocessor := NewMediaPreprocessor(NewDefaultRegistry())
	_, err := preprocessor.Normalize(context.Background(), MultiModalDetail{
		ID:   "img-1",
		Type: constants.MultiModalTypeImage,
		Path: "/nonexistent/path/image.png",
	})
	require.Error(t, err)
}

// customImageValidator for testing size limits
type customImageValidator struct {
	maxSize int64
}

func (v *customImageValidator) Type() constants.MultiModalType         { return constants.MultiModalTypeImage }
func (v *customImageValidator) MIMEWhitelist() []string                { return []string{"image/png"} }
func (v *customImageValidator) MaxSize() int64                         { return v.maxSize }
func (v *customImageValidator) ExtractMeta(data []byte) map[string]any { return nil }

// Compile-time check
var _ MediaTypeValidator = &customImageValidator{}
