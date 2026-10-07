package api

import (
	"encoding/json"
	"io"
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
	StaticFS fs.FS // Optional embedded filesystem for Astro frontend
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

// serveErrorPage serves an error page (HTML if browser request and file exists, JSON if API request).
func serveErrorPage(w http.ResponseWriter, r *http.Request, staticFS fs.FS, statusCode int, pageName string) {
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": http.StatusText(statusCode),
		})
		return
	}

	if staticFS != nil {
		if f, err := staticFS.Open(pageName); err == nil {
			defer f.Close()
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(statusCode)
			_, _ = io.Copy(w, f)
			return
		}
	}

	http.Error(w, http.StatusText(statusCode), statusCode)
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
					serveErrorPage(w, r, rc.StaticFS, http.StatusInternalServerError, "500.html")
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

	// 8. Early File Deletion
	r.Delete("/api/files/{id}", deleteHandler.HandleAPIDelete)
	r.Get("/d/{token}", deleteHandler.HandleBrowserDelete)

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
		serveErrorPage(w, r, rc.StaticFS, http.StatusNotFound, "404.html")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error": "method not allowed",
		})
	})

	// 11. Embedded Static Frontend (Astro)
	if rc.StaticFS != nil {
		fileServer := http.FileServer(http.FS(rc.StaticFS))
		r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
			path := strings.TrimPrefix(r.URL.Path, "/")
			if path == "" {
				fileServer.ServeHTTP(w, r)
				return
			}

			// 1. Direct file match (e.g. "favicon.svg", "_astro/foo.css", "404.html", "500.html")
			if isRegularFile(rc.StaticFS, path) {
				if strings.HasPrefix(path, "_astro/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else if strings.HasSuffix(path, ".html") {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}

			// 2. Route extension match (e.g. "/404" -> "404.html", "/500" -> "500.html")
			if isRegularFile(rc.StaticFS, path+".html") {
				if path == "404" {
					serveErrorPage(w, r, rc.StaticFS, http.StatusNotFound, "404.html")
					return
				}
				if path == "410" {
					serveErrorPage(w, r, rc.StaticFS, http.StatusGone, "410.html")
					return
				}
				if path == "500" {
					serveErrorPage(w, r, rc.StaticFS, http.StatusInternalServerError, "500.html")
					return
				}
				w.Header().Set("Cache-Control", "no-cache")
				r.URL.Path = "/" + path + ".html"
				fileServer.ServeHTTP(w, r)
				return
			}

			// 3. Subdirectory index match (e.g. "/subdir" -> "subdir/index.html")
			if isRegularFile(rc.StaticFS, path+"/index.html") {
				r.URL.Path = "/" + path + "/index.html"
				fileServer.ServeHTTP(w, r)
				return
			}

			// 4. Non-existent path -> serve 404 error page with status 404
			serveErrorPage(w, r, rc.StaticFS, http.StatusNotFound, "404.html")
		})
	}

	return r
}
