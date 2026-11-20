package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

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
