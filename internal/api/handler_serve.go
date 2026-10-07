package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/gc"
	"warpstash/internal/storage"
	"warpstash/internal/util"
)

var (
	reBurnData     = regexp.MustCompile(`(?si)<script[^>]*\bid=["']burn-data["'][^>]*>.*?</script>`)
	reBurnFilename = regexp.MustCompile(`(?si)(<[^>]*\bid=["']burn-filename["'][^>]*>)(.*?)(</[^>]+>)`)
	reBurnFilesize = regexp.MustCompile(`(?si)(<[^>]*\bid=["']burn-filesize["'][^>]*>)(.*?)(</[^>]+>)`)
	reBurnBtnHref  = regexp.MustCompile(`(?i)(\bid=["']burn-download-btn["'][^>]*\bhref=["'])[^"']*(")`)
	reBurnHrefBtn  = regexp.MustCompile(`(?i)(\bhref=["'])[^"']*("[^>]*\bid=["']burn-download-btn["'])`)
)

// ServeHandler handles public file streaming, range requests, and bot-shielded burn-on-read delivery.
type ServeHandler struct {
	cfg          *config.Config
	db           *database.DB
	storage      storage.StorageEngine
	cleaner      *gc.Cleaner
	logger       *slog.Logger
	staticFS     fs.FS
	burnHTMLOnce sync.Once
	burnHTMLData string
}

// NewServeHandler creates a ServeHandler instance.
func NewServeHandler(cfg *config.Config, db *database.DB, store storage.StorageEngine, cleaner *gc.Cleaner, l *slog.Logger, staticFS fs.FS) *ServeHandler {
	return &ServeHandler{
		cfg:      cfg,
		db:       db,
		storage:  store,
		cleaner:  cleaner,
		logger:   l,
		staticFS: staticFS,
	}
}

func (h *ServeHandler) getBurnHTML() string {
	h.burnHTMLOnce.Do(func() {
		if h.staticFS != nil {
			if content, err := fs.ReadFile(h.staticFS, "burn.html"); err == nil {
				h.burnHTMLData = string(content)
			}
		}
	})
	return h.burnHTMLData
}

// HandleServe processes GET /f/{file} requests.
func (h *ServeHandler) HandleServe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rawParam := chi.URLParam(r, "file")
	if rawParam == "" {
		http.NotFound(w, r)
		return
	}

	// Extract ID by removing extension if present (e.g. "k8X2mP9z.png" -> "k8X2mP9z")
	ext := filepath.Ext(rawParam)
	id := strings.TrimSuffix(rawParam, ext)

	// Fetch file metadata from DB
	file, err := h.db.GetFile(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Check if this was a burn-after-reading file that has already been consumed
			isBurned, checkErr := h.db.IsBurnedFile(ctx, id)
			if checkErr == nil && isBurned {
				h.renderBurnDestroyed(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		h.logger.Error("error querying file metadata", "id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Handle Burn-After-Reading files
	if file.IsBurnOnRead {
		h.handleBurnServe(w, r, file)
		return
	}

	// Handle Standard Temporary files
	h.handleStandardServe(w, r, file)
}

// handleBurnServe manages the bot-shield interstitial and atomic single-use streaming.
func (h *ServeHandler) handleBurnServe(w http.ResponseWriter, r *http.Request, file *database.FileRecord) {
	ctx := r.Context()

	// HEAD requests should never claim or burn a one-time file
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		return
	}

	isCLI := isCLICaller(r)
	hasBurnHeader := strings.EqualFold(r.Header.Get("X-Burn"), "true")
	hasRawQuery := r.URL.Query().Get("raw") == "1" || r.URL.Query().Get("burn") == "1"

	// 1. Bot Shield Interstitial: If requested by browser without explicit confirmation,
	// render confirmation UI so chat crawlers (Discord, Slack, Twitter) do not destroy the link!
	if !isCLI && !hasBurnHeader && !hasRawQuery {
		h.renderBurnConfirmation(w, file)
		return
	}

	// 2. Confirmed Claim: Atomic race-condition-proof claim query
	claimed, err := h.db.AtomicClaimBurnFile(ctx, file.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.renderBurnDestroyed(w, r)
			return
		}
		h.logger.Error("failed to claim burn file", "id", file.ID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// 3. Open file from storage
	rc, info, err := h.storage.Open(claimed.StoragePath)
	if err != nil {
		h.logger.Error("failed to open burn file on disk", "id", file.ID, "path", claimed.StoragePath, "error", err)
		http.NotFound(w, r)
		return
	}

	var closed bool
	defer func() {
		if !closed {
			_ = rc.Close()
			h.cleaner.EnqueueImmediate(claimed.ID, claimed.StoragePath, claimed.SizeBytes)
			h.logger.Info("burn file dispatched to shredder", "id", claimed.ID)
		}
	}()

	// Apply defensive headers: burn files must download as attachments
	w.Header().Set("Content-Type", claimed.MimeType)
	w.Header().Set("Content-Disposition", util.ContentDispositionHeader(claimed.MimeType, claimed.OriginalName, false))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox allow-downloads")

	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)

	// Close open file handle immediately BEFORE scheduling physical unlink to prevent Windows ERROR_SHARING_VIOLATION
	_ = rc.Close()
	closed = true
	h.cleaner.EnqueueImmediate(claimed.ID, claimed.StoragePath, claimed.SizeBytes)
	h.logger.Info("burn file streamed and dispatched to shredder", "id", claimed.ID)
}

// handleStandardServe handles regular file streaming with HTTP Range support.
func (h *ServeHandler) handleStandardServe(w http.ResponseWriter, r *http.Request, file *database.FileRecord) {
	rc, _, err := h.storage.Open(file.StoragePath)
	if err != nil {
		h.logger.Error("failed to open file on disk", "id", file.ID, "path", file.StoragePath, "error", err)
		http.NotFound(w, r)
		return
	}
	defer rc.Close()

	isInline := r.URL.Query().Get("inline") == "1" || r.URL.Query().Get("view") == "1"

	// Security and Cache headers
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", util.ContentDispositionHeader(file.MimeType, file.OriginalName, isInline))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	if isInline {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; media-src 'self'; img-src 'self'; style-src 'unsafe-inline'; sandbox allow-downloads")
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox allow-downloads")
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
	}

	// Native HTTP Range, 206 Partial Content, and seek support
	http.ServeContent(w, r, file.OriginalName, file.CreatedAt, rc)

	// Increment download count only for full downloads (never on Range chunks)
	if r.Header.Get("Range") == "" {
		_ = h.db.IncrementDownloadCount(r.Context(), file.ID)
	}
}

// renderBurnConfirmation outputs the sleek Astro-based burn confirmation page protecting burn links from web crawlers.
func (h *ServeHandler) renderBurnConfirmation(w http.ResponseWriter, file *database.FileRecord) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	downloadURL := fmt.Sprintf("/f/%s%s?raw=1", file.ID, file.Extension)

	// If staticFS is available and contains burn.html, render the native Astro UI with Empty component
	if h.staticFS != nil {
		if htmlStr := h.getBurnHTML(); htmlStr != "" {
			burnDataJSON, _ := json.Marshal(map[string]any{
				"id":          file.ID,
				"name":        file.OriginalName,
				"sizeBytes":   file.SizeBytes,
				"downloadUrl": downloadURL,
			})

			// 1. Inject JSON metadata into <script is:inline id="burn-data">
			if reBurnData.MatchString(htmlStr) {
				htmlStr = reBurnData.ReplaceAllString(
					htmlStr,
					fmt.Sprintf(`<script is:inline id="burn-data" type="application/json">%s</script>`, burnDataJSON),
				)
			} else {
				// Fallback append before closing body if script tag not found
				htmlStr = strings.Replace(
					htmlStr,
					"</body>",
					fmt.Sprintf(`<script is:inline id="burn-data" type="application/json">%s</script></body>`, burnDataJSON),
					1,
				)
			}

			// 2. Pre-fill filename in #burn-filename
			if reBurnFilename.MatchString(htmlStr) {
				htmlStr = reBurnFilename.ReplaceAllString(htmlStr, "${1}"+html.EscapeString(file.OriginalName)+"${3}")
			} else {
				htmlStr = strings.Replace(htmlStr, "Loading file...", html.EscapeString(file.OriginalName), 1)
			}

			// 3. Pre-fill filesize in #burn-filesize
			humanSize := util.FormatBytes(file.SizeBytes)
			if reBurnFilesize.MatchString(htmlStr) {
				htmlStr = reBurnFilesize.ReplaceAllString(htmlStr, "${1}"+html.EscapeString(humanSize)+" &bull; Single-Use Delivery${3}")
			}

			// 4. Pre-fill download button href
			if reBurnBtnHref.MatchString(htmlStr) {
				htmlStr = reBurnBtnHref.ReplaceAllString(htmlStr, "${1}"+html.EscapeString(downloadURL)+"${2}")
			} else if reBurnHrefBtn.MatchString(htmlStr) {
				htmlStr = reBurnHrefBtn.ReplaceAllString(htmlStr, "${1}"+html.EscapeString(downloadURL)+"${2}")
			} else {
				htmlStr = strings.Replace(
					htmlStr,
					`id="burn-download-btn" href="#"`,
					fmt.Sprintf(`id="burn-download-btn" href="%s"`, html.EscapeString(downloadURL)),
					1,
				)
			}

			_, _ = w.Write([]byte(htmlStr))
			return
		}
	}

	// Fallback UI when staticFS is not configured (e.g. unit test mode)
	fallbackHTML := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en" class="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>🔥 Burn-After-Reading: %s — WarpStash</title>
  <style>
    :root {
      --bg: #090a0f;
      --card: #12141c;
      --border: #232738;
      --text: #f0f2f8;
      --text-muted: #8b92a5;
      --accent: #ef4444;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; font-family: system-ui, sans-serif; }
    body {
      background-color: var(--bg);
      color: var(--text);
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      padding: 1.5rem;
    }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 1rem;
      padding: 2rem;
      max-width: 440px;
      width: 100%%;
      text-align: center;
    }
    h1 { font-size: 1.25rem; font-weight: 700; margin-bottom: 0.75rem; }
    .filename {
      background: #181b26;
      border: 1px solid var(--border);
      padding: 0.5rem 0.75rem;
      border-radius: 0.5rem;
      font-family: monospace;
      font-size: 0.9rem;
      margin: 1rem 0;
      word-break: break-all;
    }
    .meta { color: var(--text-muted); font-size: 0.85rem; margin-bottom: 1rem; }
    .warning {
      color: #fca5a5;
      background: rgba(239, 68, 68, 0.1);
      border: 1px solid rgba(239, 68, 68, 0.2);
      border-radius: 0.5rem;
      padding: 0.75rem;
      font-size: 0.8rem;
      line-height: 1.4;
      margin-bottom: 1.5rem;
    }
    .btn {
      display: block;
      width: 100%%;
      padding: 0.75rem 1rem;
      background: var(--accent);
      color: white;
      text-decoration: none;
      font-weight: 600;
      border-radius: 0.5rem;
      font-size: 0.95rem;
    }
  </style>
</head>
<body>
  <div class="card">
    <div style="font-size: 2.5rem; margin-bottom: 0.5rem;">🔥</div>
    <h1>Burn-After-Reading File</h1>
    <div class="filename">%s</div>
    <div class="meta">Size: %s &bull; One-Time Delivery</div>
    <div class="warning">
      ⚠️ <strong>Warning:</strong> This file will be downloaded and permanently destroyed from the server immediately upon viewing.
    </div>
    <a href="%s" class="btn">Reveal & Download File</a>
  </div>
</body>
</html>`,
		html.EscapeString(file.OriginalName),
		html.EscapeString(file.OriginalName),
		util.FormatBytes(file.SizeBytes),
		downloadURL,
	)

	_, _ = w.Write([]byte(fallbackHTML))
}

// renderBurnDestroyed outputs a 410 Gone response indicating the burn-after-reading file was consumed.
func (h *ServeHandler) renderBurnDestroyed(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusGone)

	isCLI := isCLICaller(r)
	accept := r.Header.Get("Accept")
	if isCLI || (!strings.Contains(accept, "text/html") && strings.Contains(accept, "application/json")) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("410 Gone: This file was configured to burn after reading and has already been downloaded or destroyed.\n"))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// If staticFS is available and contains 410.html, serve the native Astro Empty-component page
	if h.staticFS != nil {
		if content, err := fs.ReadFile(h.staticFS, "410.html"); err == nil {
			_, _ = w.Write(content)
			return
		}
	}

	// Fallback UI
	fallbackHTML := `<!DOCTYPE html>
<html lang="en" class="dark">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>410 Gone — WarpStash</title>
  <style>
    body {
      background-color: #090a0f;
      color: #f0f2f8;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      font-family: system-ui, sans-serif;
      margin: 0;
    }
    .card {
      background: #12141c;
      border: 1px solid #232738;
      border-radius: 1rem;
      padding: 2rem;
      max-width: 440px;
      text-align: center;
    }
    .btn {
      display: inline-block;
      margin-top: 1rem;
      padding: 0.6rem 1.25rem;
      background: #1e2230;
      color: #f0f2f8;
      text-decoration: none;
      border-radius: 0.5rem;
    }
  </style>
</head>
<body>
  <div class="card">
    <div style="font-size: 2.5rem; margin-bottom: 0.5rem;">💨</div>
    <h1 style="font-size: 1.25rem; margin-bottom: 0.5rem;">File Burned &amp; Shredded</h1>
    <p style="color: #8b92a5; font-size: 0.9rem; line-height: 1.4;">
      This file was configured with <strong>Burn-After-Reading</strong> and has already been downloaded or destroyed from the server.
    </p>
    <a href="/" class="btn">Upload a new file</a>
  </div>
</body>
</html>`
	_, _ = w.Write([]byte(fallbackHTML))
}
