package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/backup"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestBackupAPICreatePollDownloadDeleteAndCleanupProtection(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{Storage: config.StorageConfig{Database: filepath.Join(dir, "db"), BlobDir: filepath.Join(dir, "blobs"), BlobStore: "fs", RetentionDays: 1, CleanupPaused: true}}
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
	addCleanupTestLog(t, repo, "old")
	blobs, err := storage.NewFileBlobStore(cfg.Storage.BlobDir)
	if err != nil {
		t.Fatal(err)
	}
	h := New(cfg, repo, blobs, nil)
	defer h.CloseBackups()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := request("POST", "/api/system/backups")
	if w.Code != 202 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	var job backup.Job
	if err = json.Unmarshal(w.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		w = request("GET", "/api/system/backups/"+job.ID)
		if err = json.Unmarshal(w.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.State != "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if job.State != "completed" || job.Size == 0 {
		t.Fatalf("backup did not finish: %+v", job)
	}
	w = request("GET", "/api/system/backups/"+job.ID+"/download")
	if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("download: %d %s", w.Code, w.Body)
	}
	r := httptest.NewRequest("GET", "/api/system/backups/"+job.ID+"/download", nil)
	r.Header.Set("Range", "bytes=0-3")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.Len() != 4 {
		t.Fatal("range download not supported")
	}
	if preview := cleanupTestPreview(t, h); preview.Enabled {
		t.Fatal("restored history exposed to manual cleanup")
	}
	w = request("POST", "/api/system/cleanup/resume")
	if w.Code != 200 || cfg.StorageSnapshot().CleanupPaused {
		t.Fatal("resume failed")
	}
	if preview := cleanupTestPreview(t, h); !preview.Enabled || preview.Counts.EligibleLogs != 1 {
		t.Fatal("cleanup not resumed")
	}
	w = request("DELETE", "/api/system/backups/"+job.ID)
	if w.Code != 204 {
		t.Fatal("delete failed", w.Code)
	}
	if w = request("GET", "/api/system/backups/"+job.ID+"/download"); w.Code != 404 {
		t.Fatal("deleted archive still served")
	}
	if _, err = repo.GetLog("old"); err != nil {
		t.Fatal("deleting backup changed live logs")
	}
}
