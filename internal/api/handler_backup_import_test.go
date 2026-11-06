package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/auth"
	"github.com/shaomian/PrismCat/internal/backup"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestBackupImportAPIRequiresSessionOriginAndConfirmation(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Server: config.ServerConfig{UIPassword: "import-test-password"}, Storage: config.StorageConfig{Database: filepath.Join(dir, "prismcat.db"), BlobDir: filepath.Join(dir, "blobs"), BlobStore: "fs"}}
	data, err := cfg.BackupSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "config.yaml")
	if err = os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = config.Load(name)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(cfg.Storage.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	addCleanupTestLog(t, repo, "live-only")
	blobs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
	if err != nil {
		t.Fatal(err)
	}
	h := New(cfg, repo, blobs, nil)
	defer h.CloseBackups()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	sessions := auth.NewManager(cfg)
	protected := auth.RequireSession(sessions, mux)
	archive, err := os.ReadFile(filepath.Join("..", "backup", "testdata", "legacy-v2.4.1.zip"))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body []byte, cookie *http.Cookie, origin, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if cookie != nil {
			r.AddCookie(cookie)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if contentType != "" {
			r.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		return w
	}
	if w := request("POST", "/api/system/backup-import", archive, nil, "", "application/zip"); w.Code != 401 {
		t.Fatal("unauthenticated upload accepted", w.Code)
	}
	login := httptest.NewRecorder()
	if _, err = sessions.Login(login, httptest.NewRequest("POST", "/api/auth/login", nil), "import-test-password"); err != nil {
		t.Fatal(err)
	}
	cookie := login.Result().Cookies()[0]
	if w := request("POST", "/api/system/backup-import", archive, cookie, "https://evil.invalid", "application/zip"); w.Code != 403 {
		t.Fatal("cross-origin upload accepted", w.Code)
	}
	if w := request("POST", "/api/system/backup-import", archive, cookie, "http://example.com", "multipart/form-data"); w.Code != 415 {
		t.Fatal("unexpected body type accepted", w.Code)
	}
	w := request("POST", "/api/system/backup-import", archive, cookie, "http://example.com", "application/zip")
	if w.Code != 202 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("upload: %d %s", w.Code, w.Body)
	}
	var uploaded backup.ImportJob
	if err = json.Unmarshal(w.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	var status struct {
		Job       *backup.ImportJob `json:"import_job"`
		Supported bool              `json:"import_supported"`
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w = request("GET", "/api/system/backups", nil, cookie, "", "")
		if err = json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		if status.Job != nil && status.Job.State != "checking" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if status.Job == nil || status.Job.State != "ready" || !status.Supported {
		t.Fatalf("invalid preview: %+v", status)
	}
	path := "/api/system/backup-import/" + uploaded.ID
	if w = request("POST", path+"/confirm", []byte(`{}`), cookie, "http://example.com", "application/json"); w.Code != 400 {
		t.Fatal("missing explicit confirmation accepted")
	}
	if w = request("POST", path+"/confirm", []byte(`{"confirmed":true}`), cookie, "https://evil.invalid", "application/json"); w.Code != 403 {
		t.Fatal("cross-origin confirmation accepted")
	}
	w = request("POST", path+"/confirm", []byte(`{"confirmed":true}`), cookie, "http://example.com", "application/json")
	if w.Code != 200 {
		t.Fatalf("confirm: %d %s", w.Code, w.Body)
	}
	if _, err = repo.GetLog("live-only"); err != nil {
		t.Fatal("confirmation changed live data", err)
	}
	if _, err = repo.GetLog("sample"); err == nil {
		t.Fatal("confirmation imported data before restart")
	}
	if w = request("DELETE", path, nil, cookie, "https://evil.invalid", ""); w.Code != 403 {
		t.Fatal("cross-origin cancellation accepted")
	}
	if w = request("DELETE", path, nil, cookie, "http://example.com", ""); w.Code != 204 {
		t.Fatal("cancellation failed", w.Code)
	}
	if _, err = repo.GetLog("live-only"); err != nil {
		t.Fatal("cancellation changed data", err)
	}
	// Unknown-length bodies are still streamable; there is no multipart buffer.
	r := httptest.NewRequest("POST", "/api/system/backup-import", io.NopCloser(bytes.NewReader(archive)))
	r.ContentLength = -1
	r.AddCookie(cookie)
	r.Header.Set("Content-Type", "application/zip")
	w = httptest.NewRecorder()
	protected.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatalf("streaming upload failed: %d %s", w.Code, w.Body)
	}
}
