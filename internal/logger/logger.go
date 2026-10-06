package logger

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/lmittmann/tint"
	"github.com/mattn/go-isatty"
)

// Init initializes and sets the global default slog logger based on level and format.
func Init(levelStr, formatStr string) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	var handler slog.Handler
	switch strings.ToLower(strings.TrimSpace(formatStr)) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	case "logfmt":
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	default:
		handler = tint.NewTextHandler(os.Stdout, &tint.Options{Level: level, TimeFormat: "15:04:05.000", NoColor: !isColorTerminal(os.Stdout)})
	}

	l := slog.New(handler)
	slog.SetDefault(l)
	return l
}

func isColorTerminal(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd()) || os.Getenv("COLORTERM") != "" || os.Getenv("TERM") != ""
	}
	return true
}

// isFrontendAsset returns true for static web UI assets that should be omitted from backend server logs.
func isFrontendAsset(path string) bool {
	if strings.HasPrefix(path, "/_astro/") {
		return true
	}
	if path == "/favicon.ico" || path == "/favicon.svg" {
		return true
	}
	// Omit common frontend static assets when NOT under /f/ (user storage) or /d/ or /api/
	if !strings.HasPrefix(path, "/f/") && !strings.HasPrefix(path, "/d/") && !strings.HasPrefix(path, "/api/") {
		ext := strings.ToLower(filepath.Ext(path))
		switch ext {
		case ".css", ".js", ".svg", ".ico", ".woff", ".woff2", ".ttf", ".webp", ".png", ".jpg", ".jpeg", ".map":
			return true
		}
	}
	return false
}

// HTTPMiddleware creates a structured HTTP request logging middleware using slog.
func HTTPMiddleware(l *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for frontend static assets to prevent bloated logs
			if isFrontendAsset(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			duration := time.Since(start)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			clientIP := r.RemoteAddr
			if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
				clientIP = xri
			} else if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
				parts := strings.Split(xff, ",")
				clientIP = strings.TrimSpace(parts[0])
			}

			reqID := middleware.GetReqID(r.Context())

			// Clean, structured message: "GET /api/upload"
			msg := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

			attrs := []any{
				slog.Int("status", status),
				slog.Duration("duration", duration),
				slog.String("ip", clientIP),
				slog.Int("bytes", ww.BytesWritten()),
			}
			if reqID != "" {
				attrs = append(attrs, slog.String("req_id", reqID))
			}
			if l.Enabled(r.Context(), slog.LevelDebug) {
				attrs = append(attrs, slog.String("user_agent", r.UserAgent()))
			}

			// Differentiate log level based on status code
			switch {
			case status >= 500:
				l.Error(msg, attrs...)
			case status >= 400:
				l.Warn(msg, attrs...)
			default:
				l.Info(msg, attrs...)
			}
		})
	}
}
