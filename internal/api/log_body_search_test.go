package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func bodySearchFixture(t *testing.T) (*http.ServeMux, *storage.SQLiteRepository, *storage.FileBlobStore) {
	t.Helper()
	dir := t.TempDir()
	repo, err := storage.NewSQLiteRepository(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	blobs, err := storage.NewFileBlobStore(filepath.Join(dir, "blobs"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Logging: config.LoggingConfig{StoreBase64: true}}
	async := storage.NewAsyncRepository(repo, cfg, 32, blobs)
	t.Cleanup(func() { _ = async.Close() })
	mux := http.NewServeMux()
	New(cfg, async, blobs, nil).RegisterRoutes(mux)
	return mux, repo, blobs
}

func searchBodies(t *testing.T, mux *http.ServeMux, query string, before *storage.LogBodyPosition) bodySearchResult {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"query": query, "before": before})
	request := httptest.NewRequest(http.MethodPost, "/api/logs/body-search?trace_id=trace", bytes.NewReader(payload))
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, request)
	var result bodySearchResult
	if out.Code != http.StatusOK || json.Unmarshal(out.Body.Bytes(), &result) != nil {
		t.Fatalf("search returned %d: %s", out.Code, out.Body)
	}
	return result
}

func TestBodySearchFindsDetachedBodiesWithinTrace(t *testing.T) {
	mux, repo, blobs := bodySearchFixture(t)
	const needle = "中文 100%_匹配"
	var encoded bytes.Buffer
	gzipWriter := gzip.NewWriter(&encoded)
	_, _ = gzipWriter.Write([]byte(strings.Repeat("日志片段\n", 10000) + needle))
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	responseRef, err := blobs.Put(context.Background(), encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	requestRef, err := blobs.Put(context.Background(), []byte(strings.Repeat("request text\n", 20000)+needle))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	logs := []storage.RequestLog{
		{ID: "gzip", CreatedAt: now.Add(-time.Minute), TraceID: "trace", Upstream: "a", ResponseBody: "preview", ResponseBodyRef: responseRef,
			ResponseHeaders: map[string][]string{"Content-Type": {"text/plain"}, "Content-Encoding": {"gzip"}}},
		{ID: "chunked", CreatedAt: now.Add(-2 * time.Minute), TraceID: "trace", RequestBody: "preview", RequestBodyRef: requestRef},
		{ID: "old", CreatedAt: now.Add(-48 * time.Hour), TraceID: "trace", RequestBody: needle},
		{ID: "other-trace", CreatedAt: now, TraceID: "other", RequestBody: needle},
		{ID: "wrong-literal", CreatedAt: now, TraceID: "trace", RequestBody: "中文 100XX匹配"},
		{ID: "missing", CreatedAt: now, TraceID: "trace", RequestBodyRef: "sha256:" + strings.Repeat("1", 64)},
	}
	for i := range logs {
		if err := repo.SaveLog(&logs[i]); err != nil {
			t.Fatal(err)
		}
	}
	result := searchBodies(t, mux, needle, nil)
	if result.Scanned != 5 || result.Matched != 3 || result.Incomplete != 1 || result.Limited {
		t.Fatalf("incorrect scoped results: %+v", result)
	}
	if result.Hits[0].LogID != "gzip" || result.Hits[0].Part != "response" || result.Hits[1].LogID != "chunked" || result.Hits[1].Part != "request" || result.Hits[2].LogID != "old" {
		t.Fatalf("incorrect body matches: %+v", result.Hits)
	}
	for _, hit := range result.Hits {
		if hit.Match != needle {
			t.Fatalf("match changed: %q", hit.Match)
		}
	}
}

func TestTraceBodySearchPrefersOriginalSSEToolCall(t *testing.T) {
	mux, repo, _ := bodySearchFixture(t)
	// The command occurs only after the two deltas are merged and the
	// arguments are decoded. The newest request carries a copy of the call.
	sse := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call","function":{"name":"exec_command","arguments":"{\"command\":\"printf \\\""}}]}}]}

data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"中文\\\"\"}"}}]}}]}

data: [DONE]

`
	command := `printf "中文"`
	carried := `{"messages":[{"role":"assistant","tool_calls":[{"id":"call","function":{"name":"exec_command","arguments":"{\"command\":\"printf \\\"中文\\\"\"}"}}]}]}`
	logs := []storage.RequestLog{
		{ID: "origin", CreatedAt: time.Now().Add(-48 * time.Hour), TraceID: "trace", Path: "/v1/chat/completions", ResponseBody: sse, Streaming: true},
		{ID: "carried", CreatedAt: time.Now(), TraceID: "trace", Path: "/v1/chat/completions", RequestBody: carried},
		{ID: "other-trace", CreatedAt: time.Now(), TraceID: "other", RequestBody: command},
	}
	for i := range logs {
		if err := repo.SaveLog(&logs[i]); err != nil {
			t.Fatal(err)
		}
	}
	result := searchBodies(t, mux, command, nil)
	if result.Scanned != 2 || result.Matched != 2 || result.Hits[0].LogID != "origin" ||
		result.Hits[0].Tool != "exec_command" || result.Hits[0].Part != "response" ||
		result.Hits[1].LogID != "carried" || result.Hits[1].Part != "request" {
		t.Fatalf("unexpected tool-call matches: %+v", result)
	}
}

func TestBodySearchContinuesFromScanLimitAtSameMillisecond(t *testing.T) {
	mux, repo, _ := bodySearchFixture(t)
	now := time.Now()
	for i := range bodySearchLogLimit + 1 {
		log := &storage.RequestLog{
			ID: fmt.Sprintf("%04d", i), CreatedAt: now, TraceID: "trace",
		}
		if i == 0 {
			log.RequestBody = "needle"
		}
		if err := repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}
	first := searchBodies(t, mux, "needle", nil)
	if !first.Limited || first.Scanned != bodySearchLogLimit || first.Matched != 0 || first.Next == nil {
		t.Fatalf("incorrect scan limit: %+v", first)
	}
	next := searchBodies(t, mux, "needle", first.Next)
	if next.Limited || next.Next != nil || next.Scanned != 1 || len(next.Hits) != 1 || next.Hits[0].LogID != "0000" {
		t.Fatalf("continuation skipped or repeated a log: %+v", next)
	}
}

func TestBodySearchContinuesAfterHitLimit(t *testing.T) {
	mux, repo, _ := bodySearchFixture(t)
	now := time.Now()
	for i := range bodySearchHitLimit + 1 {
		log := &storage.RequestLog{ID: fmt.Sprintf("%04d", i), CreatedAt: now, TraceID: "trace", ResponseBody: "needle"}
		if err := repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}
	first := searchBodies(t, mux, "needle", nil)
	if len(first.Hits) != bodySearchHitLimit || first.Next == nil {
		t.Fatalf("missing continuation after hit limit: %+v", first)
	}
	next := searchBodies(t, mux, "needle", first.Next)
	if len(next.Hits) != 1 || next.Hits[0].LogID != "0000" || next.Next != nil {
		t.Fatalf("continuation lost a match: %+v", next)
	}
}

func TestBodySearchRequiresTrace(t *testing.T) {
	mux, _, _ := bodySearchFixture(t)
	request := httptest.NewRequest(http.MethodPost, "/api/logs/body-search", strings.NewReader(`{"query":"needle"}`))
	out := httptest.NewRecorder()
	mux.ServeHTTP(out, request)
	if out.Code != http.StatusBadRequest {
		t.Fatalf("expected missing trace to be rejected, got %d", out.Code)
	}
}
