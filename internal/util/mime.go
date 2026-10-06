package util

import (
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// Safe inline MIME types that cannot execute malicious script in the browser context.
	safeInlineMimeTypes = map[string]bool{
		"image/jpeg":       true,
		"image/png":        true,
		"image/gif":        true,
		"image/webp":       true,
		"image/avif":       true,
		"video/mp4":        true,
		"video/webm":       true,
		"video/ogg":        true,
		"video/quicktime":  true,
		"audio/mpeg":       true,
		"audio/ogg":        true,
		"audio/wav":        true,
		"audio/flac":       true,
		"audio/mp4":        true,
		"application/pdf":  true,
		"text/plain":       true,
	}

	invalidFilenameChars = regexp.MustCompile(`[\r\n\t\x00-\x1f"\\/]`)
)

// DetectMimeType determines the MIME type of a file using its extension and initial bytes.
func DetectMimeType(filename string, headBytes []byte) string {
	ext := filepath.Ext(filename)
	if ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			// Strip parameters like ; charset=utf-8 for classification
			return strings.Split(t, ";")[0]
		}
	}

	if len(headBytes) > 0 {
		detected := http.DetectContentType(headBytes)
		return strings.Split(detected, ";")[0]
	}

	return "application/octet-stream"
}

// IsSafeInline checks if the MIME type can safely be displayed inline in browsers
// without risking Stored XSS attacks (e.g. blocking SVG, HTML, JS).
func IsSafeInline(mimeType string) bool {
	clean := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	return safeInlineMimeTypes[clean]
}

// ContentDispositionHeader formats a Content-Disposition header.
// By default (inline = false), files are served as "attachment" so downloads start automatically in browsers.
// If inline is true and the MIME type is safe against Stored XSS, it returns "inline".
func ContentDispositionHeader(mimeType, filename string, inline bool) string {
	cleanName := SanitizeFilename(filename)
	if inline && IsSafeInline(mimeType) {
		return fmt.Sprintf("inline; filename=%q", cleanName)
	}
	return fmt.Sprintf("attachment; filename=%q", cleanName)
}

// SanitizeFilename removes path traversal components, newlines, and control characters.
func SanitizeFilename(name string) string {
	base := filepath.Base(name)
	cleaned := invalidFilenameChars.ReplaceAllString(base, "_")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" || cleaned == "." {
		return "file"
	}
	// Limit filename length to 255 chars
	if len(cleaned) > 255 {
		ext := filepath.Ext(cleaned)
		stemLen := 255 - len(ext)
		if stemLen > 0 {
			cleaned = cleaned[:stemLen] + ext
		} else {
			cleaned = cleaned[:255]
		}
	}
	return cleaned
}
