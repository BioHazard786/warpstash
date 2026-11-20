package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/storage"
	"warpstash/internal/util"
)

// CatboxHandler provides 100% drop-in compatibility with Catbox and Litterbox clients,
// allowing ShareX, curl scripts, and existing bots to upload directly via:
// POST /resources/internals/api.php
type CatboxHandler struct {
	cfg     *config.Config
	db      *database.DB
	storage storage.StorageEngine
	logger  *slog.Logger
}

// NewCatboxHandler creates a CatboxHandler instance.
func NewCatboxHandler(cfg *config.Config, db *database.DB, store storage.StorageEngine, l *slog.Logger) *CatboxHandler {
	return &CatboxHandler{
		cfg:     cfg,
		db:      db,
		storage: store,
		logger:  l,
	}
}

// HandleAPI processes POST /resources/internals/api.php requests.
func (h *CatboxHandler) HandleAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	mr, err := r.MultipartReader()
	if err != nil {
		h.respondCatboxError(w, http.StatusBadRequest, "Invalid multipart request: "+err.Error())
		return
	}

	var (
		filePart  *multipart.Part
		reqType   string
		timeParam string
		filename  string
	)

	// Stream through multipart parts
	for {
		part, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			h.respondCatboxError(w, http.StatusBadRequest, "Error reading form part: "+err.Error())
			return
		}

		formName := part.FormName()
		if formName == "fileToUpload" || part.FileName() != "" {
			filePart = part
			filename = part.FileName()
			break
		}

		buf := make([]byte, 1024)
		n, _ := part.Read(buf)
		val := strings.TrimSpace(string(buf[:n]))

		switch formName {
		case "reqtype":
			reqType = val
		case "time":
			timeParam = val
		}
		_ = part.Close()
	}

	if filePart == nil {
		h.respondCatboxError(w, http.StatusBadRequest, "No file provided (fileToUpload expected)")
		return
	}
	defer filePart.Close()

	// Disk space check
	hasDiskSpace, err := h.storage.HasAvailableDiskSpace(0)
	if err != nil || !hasDiskSpace {
		h.respondCatboxError(w, http.StatusInsufficientStorage, "Server storage is full")
		return
	}

	sanitizedName := util.SanitizeFilename(filename)
	ext := filepath.Ext(sanitizedName)

	id, err := util.GenerateID(10)
	if err != nil {
		h.respondCatboxError(w, http.StatusInternalServerError, "Internal ID error")
		return
	}

	headBuf := make([]byte, 512)
	headN, _ := io.ReadFull(filePart, headBuf)
	headBytes := headBuf[:headN]

	mimeType := util.DetectMimeType(sanitizedName, headBytes)
	fullStream := io.MultiReader(bytes.NewReader(headBytes), filePart)

	storagePath, sizeBytes, sha256Hex, err := h.storage.SaveStream(ctx, id, fullStream, h.cfg.MaxFileSizeBytes())
	if err != nil {
		if errors.Is(err, storage.ErrFileTooLarge) {
			h.respondCatboxError(w, http.StatusRequestEntityTooLarge, "File exceeds maximum upload size")
			return
		}
		h.respondCatboxError(w, http.StatusInternalServerError, "Storage write error: "+err.Error())
		return
	}

	// Post-stream quota check
	if maxStorage := h.cfg.MaxTotalStorageBytes(); maxStorage > 0 {
		currentUsage, err := h.db.GetTotalStorageUsage(ctx)
		if err == nil && currentUsage+sizeBytes > maxStorage {
			_ = h.storage.Delete(storagePath)
			h.respondCatboxError(w, http.StatusInsufficientStorage, "Server storage quota exceeded")
			return
		}
	}

	// Read trailing multipart parts (e.g. time or reqtype placed after the file payload)
	for {
		part, err := mr.NextPart()
		if err != nil {
			break
		}
		buf := make([]byte, 1024)
		n, _ := part.Read(buf)
		val := strings.TrimSpace(string(buf[:n]))

		switch part.FormName() {
		case "reqtype":
			if reqType == "" {
				reqType = val
			}
		case "time":
			if timeParam == "" {
				timeParam = val
			}
		}
		_ = part.Close()
	}

	if reqType != "" && reqType != "fileupload" {
		_ = h.storage.Delete(storagePath)
		h.respondCatboxError(w, http.StatusBadRequest, "Unsupported reqtype: "+reqType)
		return
	}

	// Parse duration
	dur, isBurn, err := h.cfg.ParseExpiry(timeParam)
	if err != nil {
		_ = h.storage.Delete(storagePath)
		h.respondCatboxError(w, http.StatusBadRequest, err.Error())
		return
	}

	deleteToken, _ := util.GenerateDeleteToken()
	now := time.Now().UTC()

	record := &database.FileRecord{
		ID:           id,
		OriginalName: sanitizedName,
		Extension:    ext,
		SizeBytes:    sizeBytes,
		MimeType:     mimeType,
		SHA256Hash:   sha256Hex,
		StoragePath:  storagePath,
		DeleteToken:  deleteToken,
		UploaderIP:   ClientIP(r, h.cfg.TrustProxy),
		IsBurnOnRead: isBurn,
		CreatedAt:    now,
		ExpiresAt:    now.Add(dur),
	}

	if err := h.db.InsertFile(ctx, record); err != nil {
		_ = h.storage.Delete(storagePath)
		h.respondCatboxError(w, http.StatusInternalServerError, "Database error: "+err.Error())
		return
	}

	fileURL := fmt.Sprintf("%s/f/%s%s", h.cfg.BaseURL, id, ext)
	h.logger.Info("Catbox compatibility upload completed", "id", id, "name", sanitizedName, "size", sizeBytes)

	// Catbox and Litterbox return raw plain text URL on success with 200 OK
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, fileURL)
}

func (h *CatboxHandler) respondCatboxError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "Error: %s", msg)
}
