package memory

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	"github.com/B777B2056-2/kugelblitz/core"
)

// MediaConverter converts multimedia content between base64 (transport format)
// and file system (persistence format). It is not a cache — it is a format adapter
// that keeps large binary payloads out of JSONL transcripts.
type MediaConverter struct {
	baseDir string // workspace/media/
}

// NewMediaConverter creates a MediaConverter that stores files under baseDir.
func NewMediaConverter(baseDir string) *MediaConverter {
	return &MediaConverter{baseDir: baseDir}
}

// Base64ToFile decodes the base64 payload from detail, writes it to
// media/{sessionID}/{contentID}.{ext}, and updates detail.Path.
// detail.Base64 is preserved for immediate provider transport.
func (c *MediaConverter) Base64ToFile(sessionID string, detail *core.MultiModalDetail) error {
	raw, err := base64.StdEncoding.DecodeString(detail.Base64)
	if err != nil {
		return fmt.Errorf("mediaconv: decode base64: %w", err)
	}

	ext := mimeToExt(detail.MimeType)
	relPath := filepath.Join("media", sessionID, detail.ID+"."+ext)
	absPath := filepath.Join(c.baseDir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("mediaconv: mkdir: %w", err)
	}
	if err := os.WriteFile(absPath, raw, 0644); err != nil {
		return fmt.Errorf("mediaconv: write file: %w", err)
	}

	detail.Path = relPath
	return nil
}

// FileToBase64 reads the persisted file for the given session+contentID
// and returns a populated MultiModalDetail with Base64 filled in.
// Used when restoring a session from JSONL.
func (c *MediaConverter) FileToBase64(sessionID, contentID string) (*core.MultiModalDetail, error) {
	// Search for the file with any extension
	pattern := filepath.Join(c.baseDir, "media", sessionID, contentID+".*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil, fmt.Errorf("mediaconv: media file not found: %s", pattern)
	}

	absPath := matches[0]
	raw, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("mediaconv: read file: %w", err)
	}

	// Derive relative path and MIME from filename
	relPath, _ := filepath.Rel(c.baseDir, absPath)
	ext := filepath.Ext(absPath)
	mimeType := extToMIME(ext)

	return &core.MultiModalDetail{
		ID:       contentID,
		Path:     relPath,
		Base64:   base64.StdEncoding.EncodeToString(raw),
		MimeType: mimeType,
	}, nil
}

// Remove deletes a single media file.
func (c *MediaConverter) Remove(sessionID, contentID string) error {
	pattern := filepath.Join(c.baseDir, "media", sessionID, contentID+".*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("mediaconv: glob: %w", err)
	}
	if len(matches) == 0 {
		return nil // nothing to remove
	}
	return os.Remove(matches[0])
}

// PruneSession removes the entire media directory for a session.
func (c *MediaConverter) PruneSession(sessionID string) error {
	sessionDir := filepath.Join(c.baseDir, "media", sessionID)
	return os.RemoveAll(sessionDir)
}

// ---- MIME ↔ extension mapping ----

// mimeToExt maps a MIME type to a file extension (without dot).
func mimeToExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpeg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	case "audio/mpeg":
		return "mp3"
	case "audio/wav":
		return "wav"
	case "audio/mp4":
		return "mp4"
	case "audio/webm":
		return "webm"
	case "video/mp4":
		return "mp4"
	case "video/webm":
		return "webm"
	case "video/quicktime":
		return "mov"
	default:
		return "bin"
	}
}

// extToMIME maps a file extension (with dot) back to a MIME type.
func extToMIME(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpeg", ".jpg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	default:
		return "application/octet-stream"
	}
}
