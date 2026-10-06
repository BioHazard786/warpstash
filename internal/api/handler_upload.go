package api

import (
	"bytes"
	"encoding/json"
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

// UploadResponse represents the JSON output returned for API uploads.
type UploadResponse struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Size         int64     `json:"size"`
	MimeType     string    `json:"mime_type"`
	URL          string    `json:"url"`
	DeleteURL    string    `json:"delete_url"`
	DeleteToken  string    `json:"delete_token"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	IsBurnOnRead bool      `json:"is_burn_on_read"`
	TimeToLive   string    `json:"time_to_live"`
}

// UploadHandler handles multipart and raw binary file uploads.
type UploadHandler struct {
	cfg     *config.Config
	db      *database.DB
	storage storage.StorageEngine
	logger  *slog.Logger
}

// NewUploadHandler creates an UploadHandler instance.
func NewUploadHandler(cfg *config.Config, db *database.DB, store storage.StorageEngine, l *slog.Logger) *UploadHandler {
	return &UploadHandler{
		cfg:     cfg,
		db:      db,
		storage: store,
		logger:  l,
	}
}

// HandleUpload handles POST uploads from Web, CLI (curl/wget), and raw binary pipes.
func (h *UploadHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Pre-flight Content-Length verification
	if r.ContentLength > h.cfg.MaxFileSizeBytes() {
		h.respondError(w, r, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("file exceeds maximum allowed size of %d MB", h.cfg.MaxFileSizeMB))
		return
	}

	// 2. Pre-flight Physical Disk Space Check
	requiredBytes := r.ContentLength
	if requiredBytes < 0 {
		requiredBytes = 0
	}
	hasDiskSpace, err := h.storage.HasAvailableDiskSpace(requiredBytes)
	if err != nil {
		h.logger.Error("failed to query available disk space", "error", err)
	} else if !hasDiskSpace {
		h.respondError(w, r, http.StatusInsufficientStorage, "server physical disk is full or below safety reserve")
		return
	}

	// 3. Pre-flight Application Quota Check
	if maxStorage := h.cfg.MaxTotalStorageBytes(); maxStorage > 0 {
		currentUsage, err := h.db.GetTotalStorageUsage(ctx)
		if err != nil {
			h.logger.Error("failed to query total storage usage", "error", err)
		} else if currentUsage+requiredBytes > maxStorage {
			h.respondError(w, r, http.StatusInsufficientStorage, "server storage quota exceeded")
			return
		}
	}

	// Determine if upload is multipart or raw binary stream
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		h.handleMultipartUpload(w, r)
	} else {
		h.handleRawStreamUpload(w, r)
	}
}

// handleMultipartUpload processes standard form uploads without memory buffering.
func (h *UploadHandler) handleMultipartUpload(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		h.respondError(w, r, http.StatusBadRequest, "invalid multipart request: "+err.Error())
		return
	}

	var (
		filePart    *multipart.Part
		filename    string
		expiryStr   string
		burnStr     string
		fileFound   bool
	)

	// Iterate through multipart parts streaming directly
	for {
		part, err := mr.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			h.respondError(w, r, http.StatusBadRequest, "error reading multipart form: "+err.Error())
			return
		}

		formName := part.FormName()
		if part.FileName() != "" || formName == "file" || formName == "files[]" || formName == "fileToUpload" {
			filePart = part
			filename = part.FileName()
			fileFound = true
			break
		}

		// Read form values prior to file
		buf := make([]byte, 1024)
		n, _ := part.Read(buf)
		val := strings.TrimSpace(string(buf[:n]))

		switch formName {
		case "time", "expiry", "ttl":
			expiryStr = val
		case "burn":
			burnStr = val
		}
		_ = part.Close()
	}

	if !fileFound || filePart == nil {
		h.respondError(w, r, http.StatusBadRequest, "no file field found in upload request")
		return
	}
	defer filePart.Close()

	if filename == "" {
		filename = "upload.bin"
	}

	// Check headers and query params initially
	if expiryStr == "" {
		expiryStr = r.Header.Get("X-Expiry")
	}
	if expiryStr == "" {
		expiryStr = r.URL.Query().Get("time")
		if expiryStr == "" {
			expiryStr = r.URL.Query().Get("expiry")
		}
	}
	if burnStr == "" {
		burnStr = r.Header.Get("X-Burn")
	}
	if burnStr == "" {
		burnStr = r.URL.Query().Get("burn")
	}

	h.processFileStream(w, r, filePart, filename, expiryStr, burnStr, mr)
}

// handleRawStreamUpload handles stdin piping (e.g., curl -T - or curl --data-binary).
func (h *UploadHandler) handleRawStreamUpload(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("filename")
	if filename == "" {
		filename = r.Header.Get("X-Filename")
	}
	if filename == "" {
		filename = "stdin.txt"
	}

	expiryStr := r.Header.Get("X-Expiry")
	if expiryStr == "" {
		expiryStr = r.URL.Query().Get("time")
		if expiryStr == "" {
			expiryStr = r.URL.Query().Get("expiry")
		}
	}

	burnStr := r.Header.Get("X-Burn")
	if burnStr == "" {
		burnStr = r.URL.Query().Get("burn")
	}

	h.processFileStream(w, r, r.Body, filename, expiryStr, burnStr, nil)
}

// processFileStream streams content directly to storage, updates DB, and responds.
func (h *UploadHandler) processFileStream(w http.ResponseWriter, r *http.Request, reader io.Reader, filename, expiryStr, burnStr string, mr *multipart.Reader) {
	ctx := r.Context()

	// Clean and sanitize filename
	sanitizedName := util.SanitizeFilename(filename)
	ext := filepath.Ext(sanitizedName)

	// Generate 10-char NanoID
	id, err := util.GenerateID(10)
	if err != nil {
		h.logger.Error("failed to generate NanoID", "error", err)
		h.respondError(w, r, http.StatusInternalServerError, "internal server error")
		return
	}

	// Read initial header bytes (up to 512) for accurate MIME sniffing without buffering the whole file
	headBuf := make([]byte, 512)
	headN, _ := io.ReadFull(reader, headBuf)
	headBytes := headBuf[:headN]

	mimeType := util.DetectMimeType(sanitizedName, headBytes)

	// Combine headBytes with remaining stream
	fullStream := io.MultiReader(bytes.NewReader(headBytes), reader)

	// Stream directly to sharded storage with zero RAM buffering
	storagePath, sizeBytes, sha256Hex, err := h.storage.SaveStream(ctx, id, fullStream, h.cfg.MaxFileSizeBytes())
	if err != nil {
		if errors.Is(err, storage.ErrFileTooLarge) {
			h.respondError(w, r, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("file exceeds maximum allowed size of %d MB", h.cfg.MaxFileSizeMB))
			return
		}
		h.logger.Error("failed to save stream", "error", err, "id", id)
		h.respondError(w, r, http.StatusInternalServerError, "failed to persist file to disk")
		return
	}

	// Post-stream quota verification for chunked or indeterminate uploads
	if maxStorage := h.cfg.MaxTotalStorageBytes(); maxStorage > 0 {
		currentUsage, err := h.db.GetTotalStorageUsage(ctx)
		if err == nil && currentUsage+sizeBytes > maxStorage {
			_ = h.storage.Delete(storagePath)
			h.respondError(w, r, http.StatusInsufficientStorage, "server storage quota exceeded")
			return
		}
	}

	// If multipart upload had trailing form fields (e.g. burn or time sent after the file payload),
	// read them now that the file part stream has been fully drained.
	if mr != nil {
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			buf := make([]byte, 1024)
			n, _ := part.Read(buf)
			val := strings.TrimSpace(string(buf[:n]))

			switch part.FormName() {
			case "time", "expiry", "ttl":
				if expiryStr == "" {
					expiryStr = val
				}
			case "burn":
				if burnStr == "" {
					burnStr = val
				}
			}
			_ = part.Close()
		}
	}

	// Fallback to headers and query parameters if still empty
	if expiryStr == "" {
		expiryStr = r.Header.Get("X-Expiry")
	}
	if expiryStr == "" {
		expiryStr = r.URL.Query().Get("time")
		if expiryStr == "" {
			expiryStr = r.URL.Query().Get("expiry")
		}
	}
	if burnStr == "" {
		burnStr = r.Header.Get("X-Burn")
	}
	if burnStr == "" {
		burnStr = r.URL.Query().Get("burn")
	}

	// Parse expiration duration and burn flag
	dur, isBurn, err := h.cfg.ParseExpiry(expiryStr)
	if err != nil {
		_ = h.storage.Delete(storagePath)
		h.respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	if strings.EqualFold(burnStr, "true") || strings.EqualFold(burnStr, "1") {
		isBurn = true
	}

	// Generate 32-byte crypto deletion token
	deleteToken, err := util.GenerateDeleteToken()
	if err != nil {
		_ = h.storage.Delete(storagePath)
		h.respondError(w, r, http.StatusInternalServerError, "failed to generate delete token")
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(dur)

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
		ExpiresAt:    expiresAt,
	}

	if err := h.db.InsertFile(ctx, record); err != nil {
		_ = h.storage.Delete(storagePath)
		h.logger.Error("failed to record file in database", "error", err, "id", id)
		h.respondError(w, r, http.StatusInternalServerError, "failed to save file metadata")
		return
	}

	h.logger.Info("file uploaded successfully",
		"id", id,
		"name", sanitizedName,
		"size", sizeBytes,
		"mime", mimeType,
		"burn", isBurn,
		"expires_in", dur.String(),
	)

	// Formulate direct and management URLs
	fileURL := fmt.Sprintf("%s/f/%s%s", h.cfg.BaseURL, id, ext)
	deleteURL := fmt.Sprintf("%s/api/files/%s?token=%s", h.cfg.BaseURL, id, deleteToken)

	ttlString := expiryStr
	if isBurn {
		ttlString = "burn"
	}

	// Content negotiation: return plain-text direct URL for curl/wget, JSON for API/Web
	if isCLICaller(r) && !strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, "%s\n", fileURL)
		return
	}

	resp := UploadResponse{
		ID:           id,
		Name:         sanitizedName,
		Size:         sizeBytes,
		MimeType:     mimeType,
		URL:          fileURL,
		DeleteURL:    deleteURL,
		DeleteToken:  deleteToken,
		CreatedAt:    now,
		ExpiresAt:    expiresAt,
		IsBurnOnRead: isBurn,
		TimeToLive:   ttlString,
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *UploadHandler) respondError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	if isCLICaller(r) && !strings.Contains(r.Header.Get("Accept"), "application/json") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "Error (%d): %s\n", status, msg)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":  msg,
		"status": status,
	})
}

// isCLICaller checks if the HTTP client is curl, wget, or requesting plain text.
func isCLICaller(r *http.Request) bool {
	ua := strings.ToLower(r.UserAgent())
	if strings.Contains(ua, "bot") || strings.Contains(ua, "crawl") || strings.Contains(ua, "spider") || strings.Contains(ua, "preview") {
		return false
	}
	if strings.Contains(ua, "curl") || strings.Contains(ua, "wget") || strings.Contains(ua, "httpie") {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/plain")
}
