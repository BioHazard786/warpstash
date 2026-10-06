package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"warpstash/internal/database"
	"warpstash/internal/gc"
)

// DeleteHandler handles early manual file deletion via secret delete tokens.
type DeleteHandler struct {
	db      *database.DB
	cleaner *gc.Cleaner
	logger  *slog.Logger
}

// NewDeleteHandler creates a DeleteHandler instance.
func NewDeleteHandler(db *database.DB, cleaner *gc.Cleaner, l *slog.Logger) *DeleteHandler {
	return &DeleteHandler{
		db:      db,
		cleaner: cleaner,
		logger:  l,
	}
}

// HandleAPIDelete handles DELETE /api/files/{id}?token={token}.
func (h *DeleteHandler) HandleAPIDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-Delete-Token")
	}
	if token == "" {
		http.Error(w, "missing required 'token' parameter", http.StatusBadRequest)
		return
	}

	record, err := h.db.GetFileByDeleteToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "invalid delete token or file already deleted", http.StatusNotFound)
			return
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if id != "" && record.ID != id {
		http.Error(w, "token does not match file ID", http.StatusForbidden)
		return
	}

	// Soft delete and dispatch to immediate shredder
	deleted, err := h.db.SoftDeleteByToken(ctx, token)
	if err != nil {
		http.Error(w, "failed to delete file", http.StatusInternalServerError)
		return
	}

	h.cleaner.EnqueueImmediate(deleted.ID, deleted.StoragePath, deleted.SizeBytes)
	h.logger.Info("file deleted early by owner", "id", deleted.ID)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":  "deleted",
		"id":      deleted.ID,
		"message": "File was successfully deleted.",
	})
}

// HandleBrowserDelete handles GET /d/{token} (one-click deletion from browser link).
func (h *DeleteHandler) HandleBrowserDelete(w http.ResponseWriter, r *http.Request) {
	// Reject prefetch requests from browsers and preview bots to prevent unintended deletion
	if strings.EqualFold(r.Header.Get("Sec-Purpose"), "prefetch") || strings.EqualFold(r.Header.Get("Purpose"), "prefetch") {
		http.Error(w, "Prefetch requests are not permitted for deletion", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	token := chi.URLParam(r, "token")
	if token == "" {
		http.NotFound(w, r)
		return
	}

	deleted, err := h.db.SoftDeleteByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			h.renderDeleteResult(w, false, "Invalid or expired deletion link. The file may have already been purged.")
			return
		}
		h.renderDeleteResult(w, false, "Internal error processing deletion.")
		return
	}

	h.cleaner.EnqueueImmediate(deleted.ID, deleted.StoragePath, deleted.SizeBytes)
	h.logger.Info("file deleted via browser link", "id", deleted.ID)

	h.renderDeleteResult(w, true, fmt.Sprintf("File %q has been permanently deleted from the server.", deleted.OriginalName))
}

func (h *DeleteHandler) renderDeleteResult(w http.ResponseWriter, success bool, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if !success {
		w.WriteHeader(http.StatusNotFound)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	statusIcon := "✅"
	statusTitle := "File Deleted"
	statusColor := "#10b981"
	if !success {
		statusIcon = "❌"
		statusTitle = "Deletion Failed"
		statusColor = "#ef4444"
	}

	htmlContent := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>%s - Warpstash</title>
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <style>
    body {
      background: #090a0f;
      color: #f0f2f8;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
      margin: 0;
    }
    .card {
      background: #12141c;
      border: 1px solid #232738;
      border-radius: 1.5rem;
      padding: 2.5rem;
      max-width: 440px;
      text-align: center;
      box-shadow: 0 20px 50px rgba(0,0,0,0.5);
    }
    .icon { font-size: 3rem; margin-bottom: 1rem; }
    h1 { font-size: 1.4rem; color: %s; margin-bottom: 0.75rem; }
    p { color: #8b92a5; line-height: 1.5; margin-bottom: 1.5rem; font-size: 0.95rem; }
    a {
      display: inline-block;
      color: #38bdf8;
      text-decoration: none;
      font-weight: 500;
      padding: 0.6rem 1.25rem;
      background: #181b26;
      border: 1px solid #232738;
      border-radius: 0.5rem;
    }
    a:hover { background: #232738; }
  </style>
</head>
<body>
  <div class="card">
    <div class="icon">%s</div>
    <h1>%s</h1>
    <p>%s</p>
    <a href="/">Return to Warpstash</a>
  </div>
</body>
</html>`,
		statusTitle,
		statusColor,
		statusIcon,
		statusTitle,
		html.EscapeString(msg),
	)

	_, _ = w.Write([]byte(htmlContent))
}
