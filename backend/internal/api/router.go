package api

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/time/rate"

	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/gc"
	"warpstash/internal/logger"
	"warpstash/internal/storage"
	"warpstash/internal/util"
)

// RouterConfig bundles all dependencies needed to configure the HTTP routing engine.
type RouterConfig struct {
	Config   *config.Config
	DB       *database.DB
	Storage  storage.StorageEngine
	Cleaner  *gc.Cleaner
	Logger   *slog.Logger
	StaticFS fs.FS // Embedded SPA build (web/dist); may be empty when the frontend isn't built
}

// isRegularFile returns true if the named file exists in fsys and is not a directory.
func isRegularFile(fsys fs.FS, name string) bool {
	f, err := fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// serveErrorPage serves an error as JSON for API callers, else the SPA (which renders the error view).
func serveErrorPage(w http.ResponseWriter, r *http.Request, staticFS fs.FS, statusCode int) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": http.StatusText(statusCode),
		})
		return
	}
	if !serveIndex(w, staticFS, statusCode) {
		http.Error(w, http.StatusText(statusCode), statusCode)
	}
}

// serveIndex writes the SPA's index.html with the given status; the client router picks the view
// from the URL. Returns false when no frontend build is embedded.
func serveIndex(w http.ResponseWriter, staticFS fs.FS, status int) bool {
	if staticFS == nil {
		return false
	}
	page, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(page)
	return true
}

// NewRouter builds and binds the complete Chi HTTP handler tree.
func NewRouter(rc *RouterConfig) http.Handler {
	r := chi.NewRouter()

	// 1. Core System Middleware
	r.Use(chimw.RequestID)
	r.Use(chimw.GetHead)
	r.Use(logger.HTTPMiddleware(rc.Logger))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rvr := recover(); rvr != nil {
					if rvr == http.ErrAbortHandler {
						panic(rvr)
					}
					rc.Logger.Error("panic recovered", "error", rvr, "path", r.URL.Path)
					serveErrorPage(w, r, rc.StaticFS, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	})

	// 2. CORS Policy for Web UI and Programmatic Clients
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Auth-Token", "X-Filename", "X-Expiry", "X-Burn", "X-Delete-Token"},
		ExposedHeaders:   []string{"Link", "Content-Disposition"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// 3. In-memory Rate Limiting per IP (safe against header spoofing)
	rateLimiter := NewIPRateLimiter(rate.Limit(rc.Config.RateLimitReqPerSec), rc.Config.RateLimitBurst)
	r.Use(RateLimitMiddleware(rateLimiter, rc.Config))

	// 4. Initialize Handlers
	uploadHandler := NewUploadHandler(rc.Config, rc.DB, rc.Storage, rc.Logger)
	serveHandler := NewServeHandler(rc.Config, rc.DB, rc.Storage, rc.Cleaner, rc.Logger, rc.StaticFS)
	catboxHandler := NewCatboxHandler(rc.Config, rc.DB, rc.Storage, rc.Logger)
	deleteHandler := NewDeleteHandler(rc.DB, rc.Cleaner, rc.Logger)

	// Auth middleware for upload endpoints
	authMw := RequireAuthTokenMiddleware(rc.Config)

	// 5. Upload Endpoints (POST and PUT for curl -T / stdin streaming)
	r.With(authMw).Post("/api/upload", uploadHandler.HandleUpload)
	r.With(authMw).Put("/api/upload", uploadHandler.HandleUpload)
	r.With(authMw).Post("/upload", uploadHandler.HandleUpload)
	r.With(authMw).Put("/upload", uploadHandler.HandleUpload)
	r.With(authMw).Post("/", uploadHandler.HandleUpload)
	r.With(authMw).Put("/", uploadHandler.HandleUpload)

	// 6. Catbox / Litterbox Drop-in Compatibility
	r.With(authMw).Post("/resources/internals/api.php", catboxHandler.HandleAPI)

	// 7. File Serving (Public)
	r.Get("/f/{file}", serveHandler.HandleServe)
	r.Post("/f/{file}", serveHandler.HandleServe) // password-protected files are unlocked by a form POST
	r.Get("/api/files/{id}", serveHandler.HandleMeta)
	r.Post("/api/files/{id}/verify", serveHandler.HandleVerify)

	// 8. Early File Deletion
	r.Delete("/api/files/{id}", deleteHandler.HandleAPIDelete)

	// Liveness + database check for container healthchecks (`warpstash -healthcheck` calls it).
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := rc.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})

	// 9. Server Telemetry & Health Info
	r.Get("/api/info", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		dbUsage, _ := rc.DB.GetTotalStorageUsage(ctx)
		diskSpace, _ := rc.Storage.DiskSpace()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service":             "Warpstash",
			"version":             rc.Config.Version,
			"max_file_size_mb":    rc.Config.MaxFileSizeMB,
			"max_file_size_human": util.FormatBytes(rc.Config.MaxFileSizeBytes()),
			"max_expiry_seconds":  int64(rc.Config.MaxExpiryDuration().Seconds()), // 0 = unlimited
			"max_files":           rc.Config.MaxFiles,
			"allowed_expiries":    rc.Config.AllowedExpiries,
			"default_expiry":      rc.Config.DefaultExpiry,
			"auth_required":       rc.Config.AuthToken != "",
			"storage_used_bytes":  dbUsage,
			"storage_used_human":  util.FormatBytes(dbUsage),
			"disk_space": map[string]any{
				"total_bytes":     diskSpace.TotalBytes,
				"total_human":     util.FormatBytes(int64(diskSpace.TotalBytes)),
				"free_bytes":      diskSpace.FreeBytes,
				"free_human":      util.FormatBytes(int64(diskSpace.FreeBytes)),
				"available_bytes": diskSpace.AvailableBytes,
				"available_human": util.FormatBytes(int64(diskSpace.AvailableBytes)),
			},
			"purged_files": rc.Cleaner.PurgedCount(),
		})
	})

	// 10. Fallback and 404/405 error handlers
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		serveErrorPage(w, r, rc.StaticFS, http.StatusNotFound)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
	})

	// 11. Embedded SPA: real files are served as-is, "/" gets index.html, anything else the SPA's 404 view.
	if rc.StaticFS != nil {
		fileServer := http.FileServer(http.FS(rc.StaticFS))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" || path == "about" {
				if !serveIndex(w, rc.StaticFS, http.StatusOK) {
					http.Error(w, "frontend not built", http.StatusNotFound)
				}
				return
			}
			if path != "index.html" && isRegularFile(rc.StaticFS, path) {
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
			serveErrorPage(w, r, rc.StaticFS, http.StatusNotFound)
		})
	}

	return r
}
