package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"warpstash/internal/config"
	"warpstash/internal/database"
	"warpstash/internal/gc"
	"warpstash/internal/logger"
	"warpstash/internal/storage"
)

func setupTestServer(t *testing.T, authToken string) (http.Handler, *database.DB, storage.StorageEngine, *gc.Cleaner) {
	t.Helper()
	tmpDir := t.TempDir()

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	storageDir := filepath.Join(tmpDir, "storage")
	store, err := storage.NewDiskStorage(storageDir)
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}

	cleaner := gc.NewCleaner(db, store, 10*time.Second, 2)

	cfg := &config.Config{
		Port:               "8080",
		BaseURL:            "http://localhost:8080",
		StoragePath:        storageDir,
		DBPath:             dbPath,
		MaxFileSizeMB:      10,
		MaxTotalStorageGB:  0,
		AuthToken:          authToken,
		DefaultExpiry:      "24h",
		AllowedExpiries:    []string{"1h", "12h", "24h", "72h", "burn"},
		RateLimitReqPerSec: 1000,
		RateLimitBurst:     1000,
	}

	l := logger.Init("error", "text")

	router := NewRouter(&RouterConfig{
		Config:  cfg,
		DB:      db,
		Storage: store,
		Cleaner: cleaner,
		Logger:  l,
	})

	t.Cleanup(func() {
		cleaner.Stop()
		db.Close()
	})

	return router, db, store, cleaner
}

func TestUploadAndDownloadStandardFile(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	// 1. Multipart form upload
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("time", "24h")
	part, _ := writer.CreateFormFile("file", "notes.txt")
	_, _ = part.Write([]byte("Hello Warpstash temporary storage!"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resp UploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Name != "notes.txt" {
		t.Errorf("expected name notes.txt, got %s", resp.Name)
	}
	if resp.IsBurnOnRead {
		t.Errorf("expected is_burn_on_read = false")
	}

	// 2. Download file
	downloadReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt", nil)
	downloadRec := httptest.NewRecorder()

	router.ServeHTTP(downloadRec, downloadReq)

	if downloadRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", downloadRec.Code)
	}
	if downloadRec.Body.String() != "Hello Warpstash temporary storage!" {
		t.Errorf("payload mismatch: %s", downloadRec.Body.String())
	}
	if !strings.Contains(downloadRec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected attachment disposition for download, got: %s", downloadRec.Header().Get("Content-Disposition"))
	}

	// 3. Test inline preview with ?inline=1
	inlineReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?inline=1", nil)
	inlineRec := httptest.NewRecorder()
	router.ServeHTTP(inlineRec, inlineReq)

	if inlineRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for inline request, got %d", inlineRec.Code)
	}
	if !strings.Contains(inlineRec.Header().Get("Content-Disposition"), "inline") {
		t.Errorf("expected inline disposition with ?inline=1, got: %s", inlineRec.Header().Get("Content-Disposition"))
	}
}

func TestCurlUploadPlaintextAutoDetection(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "curl_demo.log")
	_, _ = part.Write([]byte("log output"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "curl/8.4.0") // curl caller!
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", rec.Code)
	}

	respBody := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(respBody, "http://localhost:8080/f/") {
		t.Errorf("expected direct URL output for curl caller, got: %s", respBody)
	}
	if strings.Contains(respBody, "{") {
		t.Errorf("curl output should be plain text without JSON wrapper")
	}
}

func TestRawStreamUploadStdin(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	payload := "streamed stdin content"
	req := httptest.NewRequest(http.MethodPost, "/upload?filename=stdin.log&time=1h", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "curl/8.4.0")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", rec.Code, rec.Body.String())
	}

	url := strings.TrimSpace(rec.Body.String())
	fileParam := strings.TrimPrefix(url, "http://localhost:8080/f/")

	// Fetch file
	fetchReq := httptest.NewRequest(http.MethodGet, "/f/"+fileParam, nil)
	fetchRec := httptest.NewRecorder()
	router.ServeHTTP(fetchRec, fetchReq)

	if fetchRec.Body.String() != payload {
		t.Errorf("expected %q, got %q", payload, fetchRec.Body.String())
	}

	// 2. Test curl -T (which sends HTTP PUT)
	putPayload := "streamed via curl -T (PUT)"
	putReq := httptest.NewRequest(http.MethodPut, "/upload?filename=put_test.txt&time=1h", strings.NewReader(putPayload))
	putReq.Header.Set("User-Agent", "curl/8.4.0")
	putRec := httptest.NewRecorder()
	router.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created for PUT upload, got %d: %s", putRec.Code, putRec.Body.String())
	}

	putURL := strings.TrimSpace(putRec.Body.String())
	putFileParam := strings.TrimPrefix(putURL, "http://localhost:8080/f/")
	fetchPutReq := httptest.NewRequest(http.MethodGet, "/f/"+putFileParam, nil)
	fetchPutRec := httptest.NewRecorder()
	router.ServeHTTP(fetchPutRec, fetchPutReq)
	if fetchPutRec.Body.String() != putPayload {
		t.Errorf("expected %q, got %q", putPayload, fetchPutRec.Body.String())
	}
}

func TestBurnAfterReadingBotShieldAndAtomicShred(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	// 1. Upload one-time file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("burn", "true")
	part, _ := writer.CreateFormFile("file", "secret_passwords.txt")
	_, _ = part.Write([]byte("super-secret-password-12345"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %s", rec.Body.String())
	}

	var resp UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.IsBurnOnRead {
		t.Fatalf("expected is_burn_on_read = true")
	}

	// 2. Browser Request (or Discord crawler): without ?raw=1 or X-Burn header
	// MUST return HTML confirmation page, and NOT burn the file!
	browserReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt", nil)
	browserReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	browserReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	browserRec := httptest.NewRecorder()

	router.ServeHTTP(browserRec, browserReq)

	if browserRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for interstitial, got %d", browserRec.Code)
	}
	if !strings.Contains(browserRec.Body.String(), "Burn-After-Reading") {
		t.Errorf("expected burn warning interstitial HTML")
	}
	if strings.Contains(browserRec.Body.String(), "super-secret-password-12345") {
		t.Errorf("interstitial must NOT leak the secret payload before confirmation")
	}

	// 3. User clicks button (requests with ?raw=1)
	confirmReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?raw=1", nil)
	confirmRec := httptest.NewRecorder()

	router.ServeHTTP(confirmRec, confirmReq)

	if confirmRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on confirmed download, got %d", confirmRec.Code)
	}
	if confirmRec.Body.String() != "super-secret-password-12345" {
		t.Errorf("expected secret content, got %s", confirmRec.Body.String())
	}

	// Wait briefly for async unlinking worker
	time.Sleep(100 * time.Millisecond)

	// 4. Second attempt: MUST return 404 Not Found or 410 Gone (consumed and shredded!)
	secondReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?raw=1", nil)
	secondRec := httptest.NewRecorder()

	router.ServeHTTP(secondRec, secondReq)

	if secondRec.Code != http.StatusNotFound && secondRec.Code != http.StatusGone {
		t.Errorf("expected 404 or 410 for already consumed burn file, got %d", secondRec.Code)
	}
}

func TestBurnUploadWithFieldsAfterFile(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	// Form has file part FIRST, then burn part AFTER (like standard browser FormData)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "post_field_secret.txt")
	_, _ = part.Write([]byte("trailing-field-secret"))
	_ = writer.WriteField("time", "burn")
	_ = writer.WriteField("burn", "true")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %s", rec.Body.String())
	}

	var resp UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.IsBurnOnRead {
		t.Fatalf("expected is_burn_on_read = true when burn fields are placed after file")
	}

	// GET without confirmation should render interstitial
	browserReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt", nil)
	browserRec := httptest.NewRecorder()
	router.ServeHTTP(browserRec, browserReq)
	if browserRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for interstitial, got %d", browserRec.Code)
	}
	if !strings.Contains(browserRec.Body.String(), "Burn-After-Reading") {
		t.Errorf("expected burn warning interstitial HTML")
	}

	// HEAD request must NEVER claim or destroy the file
	headReq := httptest.NewRequest(http.MethodHead, "/f/"+resp.ID+".txt", nil)
	headRec := httptest.NewRecorder()
	router.ServeHTTP(headRec, headReq)
	if headRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for HEAD request, got %d", headRec.Code)
	}

	// User confirmed download via ?raw=1
	confirmReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?raw=1", nil)
	confirmRec := httptest.NewRecorder()
	router.ServeHTTP(confirmRec, confirmReq)
	if confirmRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on confirmed download, got %d", confirmRec.Code)
	}
	if confirmRec.Body.String() != "trailing-field-secret" {
		t.Errorf("expected secret content, got %s", confirmRec.Body.String())
	}

	// Subsequent request MUST return 404 or 410 (cannot download again)
	time.Sleep(100 * time.Millisecond)
	secondReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt", nil)
	secondRec := httptest.NewRecorder()
	router.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusNotFound && secondRec.Code != http.StatusGone {
		t.Errorf("expected 404 or 410 for consumed burn file, got %d", secondRec.Code)
	}
}

func TestCatboxCompatibility(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("reqtype", "fileupload")
	_ = writer.WriteField("time", "12h")
	part, _ := writer.CreateFormFile("fileToUpload", "catbox_image.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nfake-png-content"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/resources/internals/api.php", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Catbox compatibility failed with %d: %s", rec.Code, rec.Body.String())
	}

	rawURL := rec.Body.String()
	if !strings.HasPrefix(rawURL, "http://localhost:8080/f/") {
		t.Errorf("expected Catbox to return raw URL string, got: %s", rawURL)
	}
}

func TestEarlyDeletion(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	// Upload file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "to_delete.txt")
	_, _ = part.Write([]byte("delete me"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	var resp UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	// 1. Delete via REST API
	delReq := httptest.NewRequest(http.MethodDelete, "/api/files/"+resp.ID+"?token="+resp.DeleteToken, nil)
	delRec := httptest.NewRecorder()

	router.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on deletion, got %d", delRec.Code)
	}

	// 2. Subsequent access should return 404
	getReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt", nil)
	getRec := httptest.NewRecorder()
	router.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found after deletion, got %d", getRec.Code)
	}
}

func TestHybridAuthToken(t *testing.T) {
	// Setup server with WARPSTASH_AUTH_TOKEN required
	router, _, _, _ := setupTestServer(t, "secret-admin-pass")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "secure.txt")
	_, _ = part.Write([]byte("test"))
	_ = writer.Close()

	// 1. Unauthorized attempt -> must fail 401
	unauthReq := httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader(body.Bytes()))
	unauthReq.Header.Set("Content-Type", writer.FormDataContentType())
	unauthRec := httptest.NewRecorder()

	router.ServeHTTP(unauthRec, unauthReq)
	if unauthRec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", unauthRec.Code)
	}

	// 2. Authorized attempt with Bearer token -> must succeed 201
	authReq := httptest.NewRequest(http.MethodPost, "/api/upload", bytes.NewReader(body.Bytes()))
	authReq.Header.Set("Content-Type", writer.FormDataContentType())
	authReq.Header.Set("Authorization", "Bearer secret-admin-pass")
	authRec := httptest.NewRecorder()

	router.ServeHTTP(authRec, authReq)
	if authRec.Code != http.StatusCreated {
		t.Errorf("expected 201 Created with valid token, got %d", authRec.Code)
	}
}

func TestServerInfoEndpoint(t *testing.T) {
	router, _, _, _ := setupTestServer(t, "")

	req := httptest.NewRequest(http.MethodGet, "/api/info", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if data["service"] != "Warpstash" {
		t.Errorf("expected service Warpstash, got %v", data["service"])
	}
}

func TestStaticFSErrorPages(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body>Home</body></html>")},
		"404.html":   &fstest.MapFile{Data: []byte("<html><body>Page Not Found 404</body></html>")},
		"500.html":   &fstest.MapFile{Data: []byte("<html><body>Internal Error 500</body></html>")},
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	store, err := storage.NewDiskStorage(filepath.Join(tmpDir, "storage"))
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}
	cleaner := gc.NewCleaner(db, store, 10*time.Second, 2)
	defer cleaner.Stop()

	cfg := &config.Config{
		Port:               "8080",
		BaseURL:            "http://localhost:8080",
		MaxFileSizeMB:      10,
		DefaultExpiry:      "24h",
		AllowedExpiries:    []string{"1h", "24h"},
		RateLimitReqPerSec: 1000,
		RateLimitBurst:     1000,
	}
	l := logger.Init("error", "text")

	router := NewRouter(&RouterConfig{
		Config:   cfg,
		DB:       db,
		Storage:  store,
		Cleaner:  cleaner,
		Logger:   l,
		StaticFS: mockFS,
	})

	// 1. Root path should render index.html (200 OK)
	reqHome := httptest.NewRequest(http.MethodGet, "/", nil)
	recHome := httptest.NewRecorder()
	router.ServeHTTP(recHome, reqHome)
	if recHome.Code != http.StatusOK {
		t.Errorf("expected 200 for /, got %d", recHome.Code)
	}
	if !strings.Contains(recHome.Body.String(), "Home") {
		t.Errorf("expected Home in body, got: %s", recHome.Body.String())
	}

	// 2. Random unknown path /asdasd should return 404 and render 404.html
	req404 := httptest.NewRequest(http.MethodGet, "/asdasd", nil)
	rec404 := httptest.NewRecorder()
	router.ServeHTTP(rec404, req404)
	if rec404.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /asdasd, got %d", rec404.Code)
	}
	if !strings.Contains(rec404.Body.String(), "Page Not Found 404") {
		t.Errorf("expected 404.html content for /asdasd, got: %s", rec404.Body.String())
	}

	// 3. Explicit /404 path should return 404 and render 404.html
	reqExplicit404 := httptest.NewRequest(http.MethodGet, "/404", nil)
	recExplicit404 := httptest.NewRecorder()
	router.ServeHTTP(recExplicit404, reqExplicit404)
	if recExplicit404.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /404, got %d", recExplicit404.Code)
	}
	if !strings.Contains(recExplicit404.Body.String(), "Page Not Found 404") {
		t.Errorf("expected 404.html content for /404, got: %s", recExplicit404.Body.String())
	}

	// 4. Explicit /500 path should return 500 and render 500.html
	req500 := httptest.NewRequest(http.MethodGet, "/500", nil)
	rec500 := httptest.NewRecorder()
	router.ServeHTTP(rec500, req500)
	if rec500.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 for /500, got %d", rec500.Code)
	}
	if !strings.Contains(rec500.Body.String(), "Internal Error 500") {
		t.Errorf("expected 500.html content for /500, got: %s", rec500.Body.String())
	}

	// 5. API route with non-existent path should return JSON 404
	reqAPI404 := httptest.NewRequest(http.MethodGet, "/api/random-missing", nil)
	recAPI404 := httptest.NewRecorder()
	router.ServeHTTP(recAPI404, reqAPI404)
	if recAPI404.Code != http.StatusNotFound {
		t.Errorf("expected 404 for /api/random-missing, got %d", recAPI404.Code)
	}
	if !strings.Contains(recAPI404.Header().Get("Content-Type"), "application/json") {
		t.Errorf("expected application/json for API 404, got: %s", recAPI404.Header().Get("Content-Type"))
	}
}

func TestBurnStaticFSRendering(t *testing.T) {
	mockFS := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body>Home</body></html>")},
		"burn.html": &fstest.MapFile{Data: []byte(`<!DOCTYPE html><html><body><div id="burn-filename">Loading file...</div><div id="burn-filesize"></div><a id="burn-download-btn" href="#">Reveal</a><script is:inline id="burn-data" type="application/json">
    {}
  </script></body></html>`)},
		"410.html": &fstest.MapFile{Data: []byte("<!DOCTYPE html><html><body><h1>Astro File Burned 410</h1></body></html>")},
	}

	tmpDir := t.TempDir()
	db, err := database.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	store, err := storage.NewDiskStorage(filepath.Join(tmpDir, "storage"))
	if err != nil {
		t.Fatalf("failed to open test storage: %v", err)
	}
	cleaner := gc.NewCleaner(db, store, 10*time.Second, 2)
	defer cleaner.Stop()

	cfg := &config.Config{
		Port:               "8080",
		BaseURL:            "http://localhost:8080",
		MaxFileSizeMB:      10,
		DefaultExpiry:      "24h",
		AllowedExpiries:    []string{"1h", "24h"},
		RateLimitReqPerSec: 1000,
		RateLimitBurst:     1000,
	}
	l := logger.Init("error", "text")

	router := NewRouter(&RouterConfig{
		Config:   cfg,
		DB:       db,
		Storage:  store,
		Cleaner:  cleaner,
		Logger:   l,
		StaticFS: mockFS,
	})

	// 1. Upload a burn-on-read file
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("burn", "true")
	part, _ := writer.CreateFormFile("file", "secret_report.pdf")
	_, _ = part.Write([]byte("classified-data"))
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	var resp UploadResponse
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &resp)

	// 2. Interstitial request should serve burn.html with replaced metadata
	browserReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".pdf", nil)
	browserReq.Header.Set("Accept", "text/html")
	browserRec := httptest.NewRecorder()
	router.ServeHTTP(browserRec, browserReq)

	if browserRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for burn.html interstitial, got %d", browserRec.Code)
	}
	bodyStr := browserRec.Body.String()
	if !strings.Contains(bodyStr, "secret_report.pdf") {
		t.Errorf("expected secret_report.pdf prefilled into burn.html, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "Single-Use Delivery") {
		t.Errorf("expected Single-Use Delivery prefilled in filesize, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, fmt.Sprintf(`"id":"%s"`, resp.ID)) {
		t.Errorf("expected JSON metadata with id in burn-data script, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, fmt.Sprintf("/f/%s.pdf?raw=1", resp.ID)) {
		t.Errorf("expected download URL in button, got: %s", bodyStr)
	}

	// 3. Confirm download
	rawReq := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/f/%s.pdf?raw=1", resp.ID), nil)
	rawRec := httptest.NewRecorder()
	router.ServeHTTP(rawRec, rawReq)
	if rawRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for raw download, got %d", rawRec.Code)
	}

	// 4. Test explicit /410 route served via router
	req410 := httptest.NewRequest(http.MethodGet, "/410", nil)
	rec410 := httptest.NewRecorder()
	router.ServeHTTP(rec410, req410)
	if rec410.Code != http.StatusGone {
		t.Errorf("expected 410 for /410, got %d", rec410.Code)
	}
	if !strings.Contains(rec410.Body.String(), "Astro File Burned 410") {
		t.Errorf("expected 410.html content, got: %s", rec410.Body.String())
	}
}

func TestBrowserDeletePrefetchRejection(t *testing.T) {
	router, db, _, _ := setupTestServer(t, "")

	// 1. Upload a file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "important.txt")
	_, _ = part.Write([]byte("critical data that should not be deleted by prefetch"))
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	var resp UploadResponse
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &resp)

	// 2. Prefetch request MUST be rejected
	prefetchReq := httptest.NewRequest(http.MethodGet, "/d/"+resp.DeleteToken, nil)
	prefetchReq.Header.Set("Sec-Purpose", "prefetch")
	prefetchRec := httptest.NewRecorder()
	router.ServeHTTP(prefetchRec, prefetchReq)

	if prefetchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for Sec-Purpose: prefetch, got %d", prefetchRec.Code)
	}

	// 3. Verify file is STILL ACTIVE in database
	rec, err := db.GetFile(uploadReq.Context(), resp.ID)
	if err != nil || rec == nil {
		t.Fatalf("file should still exist after prefetch rejection: %v", err)
	}

	// 4. Normal browser delete succeeds
	delReq := httptest.NewRequest(http.MethodGet, "/d/"+resp.DeleteToken, nil)
	delRec := httptest.NewRecorder()
	router.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for human browser delete, got %d", delRec.Code)
	}
}

func TestCatboxTrailingFieldsOrdering(t *testing.T) {
	router, db, _, _ := setupTestServer(t, "")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Place file FIRST, and time parameter SECOND (common in multipart clients)
	part, _ := writer.CreateFormFile("fileToUpload", "avatar.png")
	_, _ = part.Write([]byte("\x89PNG\r\n\x1a\nfake-image-bytes"))
	_ = writer.WriteField("reqtype", "fileupload")
	_ = writer.WriteField("time", "12h")
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/resources/internals/api.php", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for Catbox upload, got %d: %s", rec.Code, rec.Body.String())
	}

	url := strings.TrimSpace(rec.Body.String())
	fileParam := strings.TrimPrefix(url, "http://localhost:8080/f/")
	id := strings.TrimSuffix(fileParam, ".png")

	record, err := db.GetFile(req.Context(), id)
	if err != nil {
		t.Fatalf("failed to query uploaded file: %v", err)
	}

	// Expiry should be approx 12h from created_at
	diff := record.ExpiresAt.Sub(record.CreatedAt)
	if diff < 11*time.Hour || diff > 13*time.Hour {
		t.Errorf("expected 12h duration when field placed after file, got: %v", diff)
	}
}

func TestRangeRequestDoesNotIncrementDownloadCount(t *testing.T) {
	router, db, _, _ := setupTestServer(t, "")

	// 1. Upload file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, _ := writer.CreateFormFile("file", "video.mp4")
	_, _ = part.Write([]byte("0123456789abcdefghijklmnopqrstuvwxyz"))
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var resp UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)

	// 2. HTTP Range request (e.g. video chunk)
	rangeReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".mp4", nil)
	rangeReq.Header.Set("Range", "bytes=0-5")
	rangeRec := httptest.NewRecorder()
	router.ServeHTTP(rangeRec, rangeReq)

	if rangeRec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content, got %d", rangeRec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	recFromDB, _ := db.GetFile(req.Context(), resp.ID)
	if recFromDB.DownloadCount != 0 {
		t.Errorf("Range request should not increment download_count, got: %d", recFromDB.DownloadCount)
	}

	// 3. Full download
	fullReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".mp4", nil)
	fullRec := httptest.NewRecorder()
	router.ServeHTTP(fullRec, fullReq)

	if fullRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", fullRec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	recFromDB2, _ := db.GetFile(req.Context(), resp.ID)
	if recFromDB2.DownloadCount != 1 {
		t.Errorf("Full download should increment download_count to 1, got: %d", recFromDB2.DownloadCount)
	}
}

func TestBurnTombstonePreservedAfterGCSweep(t *testing.T) {
	router, _, _, cleaner := setupTestServer(t, "")

	// 1. Upload burn file
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("burn", "true")
	part, _ := writer.CreateFormFile("file", "temp_token.txt")
	_, _ = part.Write([]byte("confidential-token-12345"))
	_ = writer.Close()

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	router.ServeHTTP(uploadRec, uploadReq)

	var resp UploadResponse
	_ = json.Unmarshal(uploadRec.Body.Bytes(), &resp)

	// 2. Download and burn
	claimReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?raw=1", nil)
	claimRec := httptest.NewRecorder()
	router.ServeHTTP(claimRec, claimReq)

	if claimRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on claim, got %d", claimRec.Code)
	}

	// Wait for unlinker worker
	time.Sleep(100 * time.Millisecond)

	// 3. Trigger periodic GC sweep (SweepOnce)
	cleaner.SweepOnce()

	// 4. Visit link again: MUST return 410 Gone (tombstone preserved!), not 404
	afterSweepReq := httptest.NewRequest(http.MethodGet, "/f/"+resp.ID+".txt?raw=1", nil)
	afterSweepRec := httptest.NewRecorder()
	router.ServeHTTP(afterSweepRec, afterSweepReq)

	if afterSweepRec.Code != http.StatusGone {
		t.Fatalf("expected 410 Gone after GC sweep, got %d (tombstone was prematurely wiped)", afterSweepRec.Code)
	}
}
