package util

import (
	"strings"
	"testing"
)

func TestDetectMimeType(t *testing.T) {
	tests := []struct {
		filename string
		head     []byte
		expected string
	}{
		{"image.png", []byte("\x89PNG\r\n\x1a\n"), "image/png"},
		{"document.pdf", []byte("%PDF-1.4"), "application/pdf"},
		{"song.mp3", []byte("ID3"), "audio/mpeg"},
		{"video.mp4", []byte("\x00\x00\x00 ftypisom"), "video/mp4"},
		{"script.sh", []byte("#!/bin/bash"), "application/x-sh"},
		{"page.html", []byte("<!DOCTYPE html>"), "text/html"},
		{"unknown.bin", []byte{0x00, 0x01, 0x02, 0x03}, "application/octet-stream"},
	}

	for _, tt := range tests {
		got := DetectMimeType(tt.filename, tt.head)
		if tt.filename == "script.sh" {
			if got != "application/x-sh" && got != "application/x-shellscript" {
				t.Errorf("DetectMimeType(%s) = %s, expected shell script mime", tt.filename, got)
			}
			continue
		}
		if got != tt.expected {
			t.Errorf("DetectMimeType(%s) = %s, expected %s", tt.filename, got, tt.expected)
		}
	}
}

func TestIsSafeInline(t *testing.T) {
	safe := []string{"image/png", "image/jpeg", "application/pdf", "video/mp4", "text/plain"}
	for _, mime := range safe {
		if !IsSafeInline(mime) {
			t.Errorf("expected %s to be safe inline", mime)
		}
	}

	dangerous := []string{"text/html", "image/svg+xml", "application/javascript", "application/x-sh", "text/xml"}
	for _, mime := range dangerous {
		if IsSafeInline(mime) {
			t.Errorf("expected %s to NOT be safe inline (XSS risk)", mime)
		}
	}
}

func TestContentDispositionHeader(t *testing.T) {
	// Default: attachment to trigger automatic browser download
	cdDefault := ContentDispositionHeader("image/png", "photo.png", false)
	if !strings.HasPrefix(cdDefault, "attachment") {
		t.Errorf("expected attachment by default for image/png, got %s", cdDefault)
	}

	// Explicit inline requested for safe mime
	cdSafeInline := ContentDispositionHeader("image/png", "photo.png", true)
	if !strings.HasPrefix(cdSafeInline, "inline") {
		t.Errorf("expected inline when requested for image/png, got %s", cdSafeInline)
	}

	// Explicit inline requested for dangerous mime must still force attachment (XSS protection)
	cdDangerous := ContentDispositionHeader("text/html", "exploit.html", true)
	if !strings.HasPrefix(cdDangerous, "attachment") {
		t.Errorf("expected attachment for text/html even when inline requested, got %s", cdDangerous)
	}

	cdSvg := ContentDispositionHeader("image/svg+xml", "vector.svg", true)
	if !strings.HasPrefix(cdSvg, "attachment") {
		t.Errorf("expected attachment for image/svg+xml even when inline requested, got %s", cdSvg)
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.png", "normal.png"},
		{"../../etc/passwd", "passwd"},
		{"foo\x00bar.txt", "foo_bar.txt"},
		{"foo\r\nbar.txt", "foo__bar.txt"},
		{"", "file"},
		{".", "file"},
		{"/root/documents/presentation.pdf", "presentation.pdf"},
	}

	for _, tt := range tests {
		got := SanitizeFilename(tt.input)
		if got != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
