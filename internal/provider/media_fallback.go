package provider

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// media_fallback.go — text-only model support for attached images.
//
// Providers that cannot send images (vision off) used to drop them silently,
// leaving a text-only model blind to an attached screenshot. Instead we save
// the bytes to disk (best effort) and rewrite the user turn with a note that
// points the model at the saved path so it can run the `read_image` CLI on it.

const (
	maxFallbackImageBytes = 10 * 1024 * 1024
	fallbackAttachments   = ".reasonix" + string(filepath.Separator) + "attachments"
)

var fallbackSeq atomic.Uint64

// SaveImagesForTextModel writes unsupported image data URLs to disk and
// returns content with a note pointing at each saved path for the read_image
// CLI. Failures drop the images silently, matching the previous no-vision
// behaviour; content itself is preserved.
func SaveImagesForTextModel(content string, images []string) string {
	var notes []string
	for _, dataURL := range images {
		if path := saveFallbackImage(dataURL); path != "" {
			notes = append(notes, fmt.Sprintf("[imagen guardada en %s. Usa el CLI read_image %s para verla.]", path, path))
		}
	}
	if len(notes) == 0 {
		return content
	}
	if strings.TrimSpace(content) == "" {
		return strings.Join(notes, "\n")
	}
	return content + "\n\n" + strings.Join(notes, "\n")
}

func saveFallbackImage(dataURL string) string {
	mediaType, payload, ok := ParseImageDataURL(dataURL)
	if !ok {
		return ""
	}
	ext := fallbackImageExt(mediaType)
	if ext == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(raw) == 0 || len(raw) > maxFallbackImageBytes {
		return ""
	}
	path, err := writeFallbackImage(raw, ext)
	if err != nil {
		return ""
	}
	return filepath.ToSlash(path)
}

func writeFallbackImage(raw []byte, ext string) (string, error) {
	dir := fallbackAttachments
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	seq := fallbackSeq.Add(1)
	name := fmt.Sprintf("read-image-%d-%06d%s", time.Now().UnixMilli(), seq, ext)
	return filepath.Join(dir, name), os.WriteFile(filepath.Join(dir, name), raw, 0o644)
}

func fallbackImageExt(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	}
	return ""
}
