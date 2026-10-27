package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func cleanupTestRequest(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if path == "/api/system/cleanup/preview" {
		h.handleCleanupPreview(w, r)
	} else {
		h.handleCleanup(w, r)
	}
	return w
}
func cleanupTestPreview(t *testing.T, h *Handler) cleanupPreviewState {
	t.Helper()
	w := cleanupTestRequest(h, "GET", "/api/system/cleanup/preview", "")
	var preview cleanupPreviewState
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	return preview
}
func cleanupTestFinish(t *testing.T, h *Handler) cleanupJobState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		w := cleanupTestRequest(h, "GET", "/api/system/cleanup", "")
		var job cleanupJobState
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &job) != nil {
			t.Fatalf("status: %d %s", w.Code, w.Body)
		}
		if job.State != "running" {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("cleanup did not finish")
	return cleanupJobState{}
}
func newCleanupTestRepository(t *testing.T) *storage.SQLiteRepository {
	t.Helper()
	r, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "cleanup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
func addCleanupTestLog(t *testing.T, r *storage.SQLiteRepository, id string) {
	t.Helper()
	if err := r.SaveLog(&storage.RequestLog{ID: id, TraceID: id, CreatedAt: time.Now().Add(-72 * time.Hour), Upstream: "a", Method: "POST", Path: "/v1/messages", StatusCode: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestManualCleanupRechecksProtectionAndUsesPreviewCutoff(t *testing.T) {
	r := newCleanupTestRepository(t)
	addCleanupTestLog(t, r, "free")
	addCleanupTestLog(t, r, "newly-pinned")
	cfg := &config.Config{Storage: config.StorageConfig{RetentionDays: 1}}
	h := New(cfg, r, nil, nil)
	preview := cleanupTestPreview(t, h)
	if !preview.Enabled || preview.Token == "" || preview.Counts.EligibleLogs != 2 {
		t.Fatalf("preview: %+v", preview)
	}
	if _, err := r.SaveTraceMetadata(storage.TraceMetadata{TraceID: "newly-pinned", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"token": preview.Token})
	w := cleanupTestRequest(h, "POST", "/api/system/cleanup", string(body))
	if w.Code != http.StatusAccepted || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	job := cleanupTestFinish(t, h)
	if job.State != "completed" || job.DeletedLogs != 1 || !job.Before.Equal(preview.Before) {
		t.Fatalf("job: %+v", job)
	}
	if _, err := r.GetLog("newly-pinned"); err != nil {
		t.Fatal("cleanup ignored protection added after preview")
	}
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", string(body)); w.Code != http.StatusConflict {
		t.Fatalf("consumed token accepted: %d", w.Code)
	}
}

func TestManualCleanupRejectsDisabledStaleAndTamperedPreview(t *testing.T) {
	r := newCleanupTestRepository(t)
	addCleanupTestLog(t, r, "old")
	cfg := &config.Config{}
	h := New(cfg, r, nil, nil)
	if p := cleanupTestPreview(t, h); p.Enabled || p.Token != "" {
		t.Fatalf("disabled cleanup: %+v", p)
	}
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", `{"token":"invented"}`); w.Code != 409 {
		t.Fatalf("forged token accepted: %d", w.Code)
	}
	cfg.Update(func(c *config.Config) { c.Storage.RetentionDays = 1 })
	preview := cleanupTestPreview(t, h)
	body, _ := json.Marshal(map[string]string{"token": preview.Token})
	cfg.Update(func(c *config.Config) { c.Storage.RetentionDays = 30 })
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", string(body)); w.Code != 409 || !strings.Contains(w.Body.String(), "cleanup_config_changed") {
		t.Fatalf("stale config accepted: %d %s", w.Code, w.Body)
	}
	cfg.Update(func(c *config.Config) { c.Storage.RetentionDays = 1 })
	h.cleanupMu.Lock()
	h.cleanupPreview.CreatedAt = time.Now().Add(-11 * time.Minute)
	h.cleanupMu.Unlock()
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", string(body)); w.Code != 409 || !strings.Contains(w.Body.String(), "cleanup_preview_expired") {
		t.Fatalf("expired token accepted: %d %s", w.Code, w.Body)
	}
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", `{"token":"anything","before":"2100-01-01"}`); w.Code != 400 {
		t.Fatalf("client cutoff accepted: %d", w.Code)
	}
	if _, err := r.GetLog("old"); err != nil {
		t.Fatal("invalid request deleted data")
	}
}

type blockingCleanupRepository struct {
	*storage.SQLiteRepository
	started chan struct{}
	release chan struct{}
}

func (r *blockingCleanupRepository) DeleteLogsBefore(before time.Time) (int64, error) {
	close(r.started)
	<-r.release
	return r.SQLiteRepository.DeleteLogsBefore(before)
}
func TestManualCleanupRejectsConcurrentJobs(t *testing.T) {
	r := newCleanupTestRepository(t)
	addCleanupTestLog(t, r, "old")
	blocked := &blockingCleanupRepository{SQLiteRepository: r, started: make(chan struct{}), release: make(chan struct{})}
	h := New(&config.Config{Storage: config.StorageConfig{RetentionDays: 1}}, blocked, nil, nil)
	preview := cleanupTestPreview(t, h)
	body, _ := json.Marshal(map[string]string{"token": preview.Token})
	if w := cleanupTestRequest(h, "POST", "/api/system/cleanup", string(body)); w.Code != 202 {
		t.Fatalf("start: %d %s", w.Code, w.Body)
	}
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("worker not started")
	}
	for _, path := range []string{"/api/system/cleanup", "/api/system/cleanup/preview"} {
		method := "POST"
		if strings.HasSuffix(path, "preview") {
			method = "GET"
		}
		if w := cleanupTestRequest(h, method, path, string(body)); w.Code != 409 || !strings.Contains(w.Body.String(), "cleanup_busy") {
			t.Fatalf("overlap accepted: %s %d %s", path, w.Code, w.Body)
		}
	}
	close(blocked.release)
	if job := cleanupTestFinish(t, h); job.State != "completed" || job.DeletedLogs != 1 {
		t.Fatalf("job: %+v", job)
	}
}
