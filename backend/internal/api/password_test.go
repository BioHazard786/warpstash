package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func spaRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewDiskStorage(filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	cleaner := gc.NewCleaner(db, store, 10*time.Second, 2)
	t.Cleanup(func() { cleaner.Stop(); db.Close() })
	return NewRouter(&RouterConfig{
		Config: &config.Config{
			BaseURL: "http://x", MaxFileSizeMB: 10, DefaultExpiry: "24h", MaxExpiry: "30d",
			RateLimitReqPerSec: 1000, RateLimitBurst: 1000,
		},
		DB: db, Storage: store, Cleaner: cleaner, Logger: logger.Init("error", "text"),
		StaticFS: fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<html>SPA</html>")}},
	})
}

// uploadWith posts a multipart upload with the given fields (sent before the file) and returns the response.
func uploadWith(t *testing.T, h http.Handler, fields map[string]string, header http.Header) UploadResponse {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	part, _ := w.CreateFormFile("file", "secret.txt")
	_, _ = part.Write([]byte("top secret"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var resp UploadResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp
}

func do(h http.Handler, method, target string, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPasswordProtectedFile(t *testing.T) {
	h := spaRouter(t)
	resp := uploadWith(t, h, map[string]string{"password": "hunter2", "time": "2h"}, nil)
	if !resp.Protected {
		t.Fatal("response should flag the file as password protected")
	}
	path := "/f/" + resp.ID + ".txt"
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}

	// a browser gets the SPA (200), never the content
	rec := do(h, http.MethodGet, path, "", map[string]string{"Accept": "text/html", "User-Agent": "Mozilla/5.0"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SPA") || strings.Contains(rec.Body.String(), "top secret") {
		t.Errorf("browser GET: %d %q", rec.Code, rec.Body.String())
	}
	// curl gets a 401
	if rec := do(h, http.MethodGet, path, "", map[string]string{"User-Agent": "curl/8"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("curl without password: %d", rec.Code)
	}
	// wrong and right password via header
	if rec := do(h, http.MethodGet, path, "", map[string]string{"X-Password": "nope"}); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong header password: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, path, "", map[string]string{"X-Password": "hunter2"}); rec.Code != http.StatusOK || rec.Body.String() != "top secret" {
		t.Errorf("right header password: %d %q", rec.Code, rec.Body.String())
	}
	// form POST, as the SPA does it
	if rec := do(h, http.MethodPost, path, url.Values{"password": {"nope"}}.Encode(), form); rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong form password: %d", rec.Code)
	}
	rec = do(h, http.MethodPost, path, url.Values{"password": {"hunter2"}}.Encode(), form)
	if rec.Code != http.StatusOK || rec.Body.String() != "top secret" || !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("right form password: %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Disposition"))
	}
	// protected files are never rendered inline, whatever the query says
	rec = do(h, http.MethodGet, path+"?inline=1", "", map[string]string{"X-Password": "hunter2"})
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") || strings.Contains(rec.Header().Get("Cache-Control"), "public") {
		t.Errorf("inline protected file: %q %q", rec.Header().Get("Content-Disposition"), rec.Header().Get("Cache-Control"))
	}

	// meta hides the name and size while locked
	rec = do(h, http.MethodGet, "/api/files/"+resp.ID, "", nil)
	if strings.Contains(rec.Body.String(), "secret.txt") || !strings.Contains(rec.Body.String(), `"password":true`) {
		t.Errorf("locked meta leaks or lacks the flag: %s", rec.Body.String())
	}
	// verify: wrong -> 401, right -> name and size
	if rec := do(h, http.MethodPost, "/api/files/"+resp.ID+"/verify", `{"password":"nope"}`, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("verify wrong: %d", rec.Code)
	}
	rec = do(h, http.MethodPost, "/api/files/"+resp.ID+"/verify", `{"password":"hunter2"}`, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "secret.txt") {
		t.Errorf("verify right: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPasswordProtectedBurnFile(t *testing.T) {
	h := spaRouter(t)
	resp := uploadWith(t, h, map[string]string{"password": "pw", "time": "burn"}, nil)
	path := "/f/" + resp.ID + ".txt"
	form := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}

	// verifying and wrong guesses must not burn it
	do(h, http.MethodPost, "/api/files/"+resp.ID+"/verify", `{"password":"pw"}`, nil)
	do(h, http.MethodPost, path+"?raw=1", url.Values{"password": {"bad"}}.Encode(), form)
	if rec := do(h, http.MethodGet, "/api/files/"+resp.ID, "", nil); rec.Code != http.StatusOK {
		t.Fatalf("file burned before a correct download: %d", rec.Code)
	}
	rec := do(h, http.MethodPost, path+"?raw=1", url.Values{"password": {"pw"}}.Encode(), form)
	if rec.Code != http.StatusOK || rec.Body.String() != "top secret" {
		t.Fatalf("burn download: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(h, http.MethodPost, path+"?raw=1", url.Values{"password": {"pw"}}.Encode(), form); rec.Code == http.StatusOK {
		t.Error("burn file served twice")
	}
}

func TestPasswordTooLong(t *testing.T) {
	h := spaRouter(t)
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("password", strings.Repeat("a", maxPasswordLen+1))
	part, _ := w.CreateFormFile("file", "a.txt")
	_, _ = part.Write([]byte("x"))
	_ = w.Close()
	rec := do(h, http.MethodPost, "/api/upload", body.String(), map[string]string{"Content-Type": w.FormDataContentType(), "Accept": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("72+ byte password: %d, want 400", rec.Code)
	}
}

func TestUnprotectedFileIgnoresPasswordMachinery(t *testing.T) {
	h := spaRouter(t)
	resp := uploadWith(t, h, nil, nil)
	if resp.Protected {
		t.Error("no password was sent")
	}
	if rec := do(h, http.MethodGet, "/f/"+resp.ID+".txt", "", nil); rec.Code != http.StatusOK {
		t.Errorf("plain download: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/files/"+resp.ID, "", nil); !strings.Contains(rec.Body.String(), "secret.txt") {
		t.Errorf("unlocked meta should include the name: %s", rec.Body.String())
	}
}

func TestCustomExpiryAndAboutRoute(t *testing.T) {
	h := spaRouter(t)
	resp := uploadWith(t, h, map[string]string{"time": "5d"}, nil)
	if got := resp.ExpiresAt.Sub(resp.CreatedAt); got != 5*24*time.Hour {
		t.Errorf("5d expiry = %v", got)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("time", "90d") // over the 30d cap
	part, _ := w.CreateFormFile("file", "a.txt")
	_, _ = part.Write([]byte("x"))
	_ = w.Close()
	rec := do(h, http.MethodPost, "/api/upload", body.String(), map[string]string{"Content-Type": w.FormDataContentType(), "Accept": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("90d over a 30d cap: %d, want 400", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/about", "", map[string]string{"Accept": "text/html"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "SPA") {
		t.Errorf("/about: %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/api/info", "", nil); !strings.Contains(rec.Body.String(), `"max_expiry_seconds":2592000`) {
		t.Errorf("info should expose the cap: %s", rec.Body.String())
	}
}
