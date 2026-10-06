package logger

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerInit(t *testing.T) {
	l := Init("debug", "json")
	if l == nil {
		t.Fatalf("expected non-nil logger")
	}
	if slog.Default() != l {
		t.Errorf("expected default logger to be set")
	}
}

func TestHTTPMiddleware(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(handler)

	mw := HTTPMiddleware(l)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	server := mw(nextHandler)
	req := httptest.NewRequest("GET", "/test-path", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "/test-path") {
		t.Errorf("expected path in log output, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, `"status":200`) {
		t.Errorf("expected status 200 in log output, got: %s", logOutput)
	}
}

func TestFrontendAssetFiltering(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	l := slog.New(handler)

	mw := HTTPMiddleware(l)
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := mw(nextHandler)

	// Frontend static assets should NOT be logged
	assetPaths := []string{
		"/favicon.svg",
		"/favicon.ico",
		"/_astro/index.Bf89.css",
		"/_astro/hoisted.DFx.js",
		"/font.woff2",
	}

	for _, path := range assetPaths {
		buf.Reset()
		req := httptest.NewRequest("GET", path, nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)

		if buf.Len() > 0 {
			t.Errorf("expected no log for frontend asset %s, got: %s", path, buf.String())
		}
	}

	// Backend route should still be logged
	buf.Reset()
	req := httptest.NewRequest("GET", "/api/upload", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if buf.Len() == 0 {
		t.Errorf("expected log for backend route /api/upload, got nothing")
	}
}
