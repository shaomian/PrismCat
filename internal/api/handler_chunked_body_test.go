package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestChunkedBodyWorksWithExistingDownloadAndSSEBodyEndpoints(t *testing.T) {
	for _, events := range []int{500, 5000} {
		t.Run(strconv.Itoa(events), func(t *testing.T) {
			testOptimizedBodyEndpoints(t, events)
		})
	}
}

func testOptimizedBodyEndpoints(t *testing.T, events int) {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.NewFileBlobStore(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("event: response.output_text.delta\ndata: {\"delta\":\"tool result\"}\n\n"), events)
	ref, err := store.Put(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	// Reopening the store services existing download/detail/export callers
	// through the unchanged BlobStore interface.
	reader, err := storage.NewFileBlobStore(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	repo, err := storage.NewSQLiteRepository(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	entry := &storage.RequestLog{ID: "chunked", CreatedAt: time.Now(), Upstream: "test", Method: "POST", Path: "/v1/responses", StatusCode: 200, Streaming: true, ResponseHeaders: map[string][]string{"Content-Type": {"text/event-stream"}}, ResponseBody: "preview", ResponseBodyRef: ref}
	if err = repo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	h := &Handler{repo: repo, blobs: reader, cfg: &config.Config{Logging: config.LoggingConfig{MaxResponseBody: 1 << 20, StoreBase64: true}}}
	download := httptest.NewRecorder()
	h.handleBlob(download, httptest.NewRequest("GET", "/api/blobs/"+ref+"?download=1", nil))
	if download.Code != 200 || !bytes.Equal(download.Body.Bytes(), body) {
		t.Fatalf("raw download differs: %d", download.Code)
	}
	if download.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatal("download protection changed")
	}
	detail := httptest.NewRecorder()
	h.handleLogBody(detail, httptest.NewRequest("GET", "/api/logs/chunked/body?part=response", nil), "chunked")
	var decoded struct {
		Body      string `json:"body"`
		Truncated bool   `json:"truncated"`
	}
	if detail.Code != 200 || json.Unmarshal(detail.Body.Bytes(), &decoded) != nil || decoded.Body != string(body) || decoded.Truncated {
		t.Fatalf("SSE detail differs: %d", detail.Code)
	}
	h.fillExportBodies(context.Background(), entry)
	if entry.ResponseBody != string(body) {
		t.Fatal("export body differs")
	}
}
