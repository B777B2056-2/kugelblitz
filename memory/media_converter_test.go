package memory

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/B777B2056-2/kugelblitz/constants"
	"github.com/B777B2056-2/kugelblitz/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMediaConverter(t *testing.T) {
	dir := t.TempDir()
	mc := NewMediaConverter(dir)
	require.NotNil(t, mc)
	assert.Equal(t, dir, mc.baseDir)
}

func TestMediaConverter_Base64ToFile_WritesToSessionDir(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())

	detail := &core.MultiModalDetail{
		ID:       "img-1",
		Type:     constants.MultiModalTypeImage,
		Base64:   base64.StdEncoding.EncodeToString([]byte("fake-image-data")),
		MimeType: "image/png",
	}

	require.NoError(t, mc.Base64ToFile("sess-abc", detail))

	// Path should be updated
	assert.Contains(t, detail.Path, "sess-abc")
	assert.Contains(t, detail.Path, "img-1")
	assert.Contains(t, detail.Path, ".png")

	// File should exist on disk
	_, err := os.Stat(filepath.Join(mc.baseDir, detail.Path))
	require.NoError(t, err)
}

func TestMediaConverter_Base64ToFile_PreservesBase64(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())
	originalBase64 := base64.StdEncoding.EncodeToString([]byte("test-image-data"))

	detail := &core.MultiModalDetail{
		ID:       "img-2",
		Type:     constants.MultiModalTypeImage,
		Base64:   originalBase64,
		MimeType: "image/jpeg",
	}

	require.NoError(t, mc.Base64ToFile("sess-xyz", detail))

	// Base64 must remain intact (for immediate provider transport)
	assert.Equal(t, originalBase64, detail.Base64)
	// Path must end with .jpeg (derived from MIME)
	assert.Contains(t, detail.Path, ".jpeg")
}

func TestMediaConverter_FileToBase64_RestoresBase64(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())

	originalData := []byte("audio-data-bytes")
	detail := &core.MultiModalDetail{
		ID:       "aud-1",
		Type:     constants.MultiModalTypeAudio,
		Base64:   base64.StdEncoding.EncodeToString(originalData),
		MimeType: "audio/mp3",
	}

	// Store
	require.NoError(t, mc.Base64ToFile("sess-aud", detail))

	// Simulate session reload: detail has Path but no Base64
	restored, err := mc.FileToBase64("sess-aud", "aud-1")
	require.NoError(t, err)

	// Base64 should be restored
	decoded, err := base64.StdEncoding.DecodeString(restored.Base64)
	require.NoError(t, err)
	assert.Equal(t, originalData, decoded)
}

func TestMediaConverter_FileToBase64_NotFound(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())
	_, err := mc.FileToBase64("nonexistent-session", "nonexistent-id")
	require.Error(t, err)
}

func TestMediaConverter_Remove(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())

	detail := &core.MultiModalDetail{
		ID:       "img-3",
		Type:     constants.MultiModalTypeImage,
		Base64:   base64.StdEncoding.EncodeToString([]byte("data")),
		MimeType: "image/png",
	}

	require.NoError(t, mc.Base64ToFile("sess-rm", detail))

	// File exists
	_, err := os.Stat(filepath.Join(mc.baseDir, detail.Path))
	require.NoError(t, err)

	// Remove
	require.NoError(t, mc.Remove("sess-rm", "img-3"))

	// File gone
	_, err = os.Stat(filepath.Join(mc.baseDir, detail.Path))
	assert.True(t, os.IsNotExist(err))
}

func TestMediaConverter_Remove_NonExistent(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())
	// Removing a non-existent file should not error
	assert.NoError(t, mc.Remove("sess-fake", "fake-id"))
}

func TestMediaConverter_PruneSession(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())

	// Store multiple files in same session
	for i, id := range []string{"img-a", "img-b"} {
		detail := &core.MultiModalDetail{
			ID:       id,
			Type:     constants.MultiModalTypeImage,
			Base64:   base64.StdEncoding.EncodeToString([]byte("data")),
			MimeType: "image/png",
		}
		require.NoError(t, mc.Base64ToFile("sess-prune", detail))
		_ = i
	}

	// Session dir exists
	sessionDir := filepath.Join(mc.baseDir, "media", "sess-prune")
	_, err := os.Stat(sessionDir)
	require.NoError(t, err)

	// Prune
	require.NoError(t, mc.PruneSession("sess-prune"))

	// Session dir gone
	_, err = os.Stat(sessionDir)
	assert.True(t, os.IsNotExist(err))
}

func TestMediaConverter_PruneSession_NonExistent(t *testing.T) {
	mc := NewMediaConverter(t.TempDir())
	assert.NoError(t, mc.PruneSession("never-existed"))
}

func TestMediaConverter_MIMEExtToFileExt(t *testing.T) {
	// Test MIME → file extension mapping
	assert.Equal(t, "png", mimeToExt("image/png"))
	assert.Equal(t, "jpeg", mimeToExt("image/jpeg"))
	assert.Equal(t, "gif", mimeToExt("image/gif"))
	assert.Equal(t, "webp", mimeToExt("image/webp"))
	assert.Equal(t, "mp3", mimeToExt("audio/mpeg"))
	assert.Equal(t, "wav", mimeToExt("audio/wav"))
	assert.Equal(t, "mp4", mimeToExt("audio/mp4"))
	assert.Equal(t, "webm", mimeToExt("audio/webm"))
	assert.Equal(t, "mp4", mimeToExt("video/mp4"))
	assert.Equal(t, "webm", mimeToExt("video/webm"))
	assert.Equal(t, "mov", mimeToExt("video/quicktime"))
	// Unknown → bin
	assert.Equal(t, "bin", mimeToExt("application/octet-stream"))
}
