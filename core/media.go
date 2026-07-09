package core

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"  // register GIF decoder for image.DecodeConfig
	_ "image/jpeg" // register JPEG decoder for image.DecodeConfig
	_ "image/png"  // register PNG decoder for image.DecodeConfig
	"net/http"
	"os"
	"strings"

	"github.com/B777B2056-2/kugelblitz/constants"
)

// MediaTypeValidator defines per-type validation rules and metadata extraction.
// One implementation per MultiModalType. Unregistered types are rejected.
type MediaTypeValidator interface {
	Type() constants.MultiModalType
	MIMEWhitelist() []string
	MaxSize() int64
	ExtractMeta(data []byte) map[string]any
}

// MediaValidatorRegistry maps MultiModalType → MediaTypeValidator.
// Validators are queried at Normalize time; types without a registered
// validator are rejected.
type MediaValidatorRegistry struct {
	validators map[constants.MultiModalType]MediaTypeValidator
}

// Validator returns the registered validator for the given type, or nil.
func (r *MediaValidatorRegistry) Validator(t constants.MultiModalType) MediaTypeValidator {
	if r == nil {
		return nil
	}
	return r.validators[t]
}

// NewDefaultRegistry returns a MediaValidatorRegistry with built-in validators
// for image, audio, and video.
func NewDefaultRegistry() *MediaValidatorRegistry {
	return &MediaValidatorRegistry{
		validators: map[constants.MultiModalType]MediaTypeValidator{
			constants.MultiModalTypeImage: &imageValidator{},
			constants.MultiModalTypeAudio: &audioValidator{},
			constants.MultiModalTypeVideo: &videoValidator{},
		},
	}
}

// ---- imageValidator ----

type imageValidator struct{}

func (v *imageValidator) Type() constants.MultiModalType { return constants.MultiModalTypeImage }
func (v *imageValidator) MIMEWhitelist() []string {
	return []string{"image/png", "image/jpeg", "image/gif", "image/webp"}
}
func (v *imageValidator) MaxSize() int64 { return 20 * 1024 * 1024 } // 20MB

func (v *imageValidator) ExtractMeta(data []byte) map[string]any {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return map[string]any{"width": cfg.Width, "height": cfg.Height}
}

// ---- audioValidator ----

type audioValidator struct{}

func (v *audioValidator) Type() constants.MultiModalType { return constants.MultiModalTypeAudio }
func (v *audioValidator) MIMEWhitelist() []string {
	return []string{"audio/mpeg", "audio/wav", "audio/mp4", "audio/webm"}
}
func (v *audioValidator) MaxSize() int64                      { return 50 * 1024 * 1024 } // 50MB
func (v *audioValidator) ExtractMeta(_ []byte) map[string]any { return nil }

// ---- videoValidator ----

type videoValidator struct{}

func (v *videoValidator) Type() constants.MultiModalType { return constants.MultiModalTypeVideo }
func (v *videoValidator) MIMEWhitelist() []string {
	return []string{"video/mp4", "video/webm", "video/quicktime"}
}
func (v *videoValidator) MaxSize() int64                      { return 100 * 1024 * 1024 } // 100MB
func (v *videoValidator) ExtractMeta(_ []byte) map[string]any { return nil }

// ---- MediaPreprocessor ----

// MediaPreprocessor normalizes arbitrary media inputs into a standard form:
// Base64 populated, MimeType detected, Meta extracted.
type MediaPreprocessor struct {
	validators *MediaValidatorRegistry
}

// NewMediaPreprocessor creates a MediaPreprocessor backed by the given registry.
func NewMediaPreprocessor(registry *MediaValidatorRegistry) *MediaPreprocessor {
	return &MediaPreprocessor{validators: registry}
}

// Normalize validates and normalizes a MultiModalDetail.
//   - Path + no scheme → read local file
//   - Path + "://" scheme → HTTP download (future: not yet implemented)
//   - Base64 != "" → decode and validate
//   - Neither → error
func (p *MediaPreprocessor) Normalize(ctx context.Context, detail MultiModalDetail) (*MultiModalDetail, error) {
	var data []byte
	var err error

	switch {
	case detail.Base64 != "":
		data, err = base64.StdEncoding.DecodeString(detail.Base64)
		if err != nil {
			return nil, fmt.Errorf("media: decode base64: %w", err)
		}

	case strings.Contains(detail.Path, "://"):
		return nil, fmt.Errorf("media: URL download not yet supported")

	case detail.Path != "":
		data, err = os.ReadFile(detail.Path)
		if err != nil {
			return nil, fmt.Errorf("media: read file %q: %w", detail.Path, err)
		}

	default:
		return nil, fmt.Errorf("media: no source — set Path or Base64")
	}

	// MIME detection
	mimeType := http.DetectContentType(data)

	// Lookup validator
	v := p.validators.Validator(detail.Type)
	if v == nil {
		return nil, fmt.Errorf("media: no validator registered for type %q", detail.Type)
	}

	// MIME whitelist check
	if !matchMIME(mimeType, v.MIMEWhitelist()) {
		return nil, fmt.Errorf("media: MIME type %q not allowed for type %q", mimeType, detail.Type)
	}

	// Size check
	if int64(len(data)) > v.MaxSize() {
		return nil, fmt.Errorf("media: size %d exceeds max %d bytes for type %q", len(data), v.MaxSize(), detail.Type)
	}

	// Base64 encode if not already present (e.g. from file/URL)
	if detail.Base64 == "" {
		detail.Base64 = base64.StdEncoding.EncodeToString(data)
	}

	detail.MimeType = mimeType

	// Extract metadata
	if meta := v.ExtractMeta(data); meta != nil {
		detail.Meta = meta
	}

	return &detail, nil
}

// matchMIME checks whether detected MIME matches any entry in the whitelist.
// Uses prefix matching: "image/png" matches "image/png", and
// "image/png; charset=utf-8" also matches "image/png".
func matchMIME(detected string, whitelist []string) bool {
	for _, allowed := range whitelist {
		if strings.HasPrefix(detected, allowed) {
			return true
		}
	}
	return false
}
