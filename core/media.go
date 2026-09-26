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
	types "github.com/B777B2056-2/kugelblitz/core/types"
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

// Normalize validates and normalizes a types.MultiModalDetail.
//   - Path + no scheme → read local file
//   - Path + "://" scheme → HTTP download (future: not yet implemented)
//   - Base64 != "" → decode and validate
//   - Neither → error
func (p *MediaPreprocessor) Normalize(ctx context.Context, detail types.MultiModalDetail) (*types.MultiModalDetail, error) {
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
	mimeType := detectMediaType(data)

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

// detectMediaType returns the MIME type for the given bytes. It extends
// http.DetectContentType with the container formats the standard sniffer cannot
// identify (QuickTime, MP4 audio, audio-only WebM) and normalizes WAV (C1).
func detectMediaType(data []byte) string {
	switch {
	case isISOBMFF(data, "qt  "):
		return "video/quicktime"
	case isISOBMFF(data, "M4A "), isISOBMFF(data, "M4B "), isISOBMFF(data, "f4a "), isISOBMFF(data, "f4b "):
		return "audio/mp4"
	case bytes.HasPrefix(data, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		if webmTrackType(data) == 2 {
			return "audio/webm"
		}
		return "video/webm"
	case isRIFF(data, "WAVE"):
		return "audio/wav"
	default:
		return http.DetectContentType(data)
	}
}

// isISOBMFF reports whether data is an ISO Base Media File (MP4/MOV) whose
// ftyp box declares the given major brand (bytes 8..12).
func isISOBMFF(data []byte, brand string) bool {
	return len(data) >= 12 &&
		bytes.Equal(data[4:8], []byte("ftyp")) &&
		bytes.Equal(data[8:12], []byte(brand))
}

// isRIFF reports whether data is a RIFF container of the given form (e.g. "WAVE").
func isRIFF(data []byte, form string) bool {
	return len(data) >= 12 &&
		bytes.Equal(data[:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte(form))
}

// webmTrackType scans a WebM/Matroska file for the EBML TrackType element
// (ID 0x83) with a 1-byte size and returns its value (1=video, 2=audio),
// or 0 if not found.
func webmTrackType(data []byte) byte {
	for i := 0; i+2 < len(data); i++ {
		if data[i] == 0x83 && data[i+1] == 0x81 {
			if v := data[i+2]; v == 1 || v == 2 {
				return v
			}
		}
	}
	return 0
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
