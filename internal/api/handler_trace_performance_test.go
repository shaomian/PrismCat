package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

type countedTraceRepository struct {
	storage.Repository
	metadataReads atomic.Int32
	bodyReads     atomic.Int32
	started       chan struct{}
	proceed       chan struct{}
}

func (r *countedTraceRepository) GetTraceRequestMetadata(id string) ([]*storage.RequestLog, error) {
	r.metadataReads.Add(1)
	return r.Repository.(storage.TraceMetadataReader).GetTraceRequestMetadata(id)
}

func (r *countedTraceRepository) GetTraceRequests(id string) ([]*storage.RequestLog, error) {
	if r.bodyReads.Add(1) == 1 && r.started != nil {
		close(r.started)
	}
	if r.proceed != nil {
		<-r.proceed
	}
	return r.Repository.GetTraceRequests(id)
}

func tracePerformanceFixture(t *testing.T) (*Handler, *countedTraceRepository) {
	t.Helper()
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "trace.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	if err := repo.SaveLog(&storage.RequestLog{
		ID: "one", TraceID: "trace", TraceSeq: 1, CreatedAt: time.Now().UTC(),
		Upstream: "test", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200,
		RequestBody:  `{"messages":[{"role":"user","content":"hello"}]}`,
		ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"world"}}]}`,
	}); err != nil {
		t.Fatal(err)
	}
	counted := &countedTraceRepository{Repository: repo}
	return New(&config.Config{}, counted, nil, nil), counted
}

func TestTraceDetailUsesMetadataAndPreservesLogBodies(t *testing.T) {
	h, repo := tracePerformanceFixture(t)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/traces/trace", nil))
	if rr.Code != 200 || strings.Contains(rr.Body.String(), `"request_body"`) || strings.Contains(rr.Body.String(), `"response_body"`) {
		t.Fatalf("unexpected trace detail: %d %s", rr.Code, rr.Body)
	}
	if repo.metadataReads.Load() != 1 || repo.bodyReads.Load() != 0 {
		t.Fatal("trace detail read full bodies")
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/logs/one", nil))
	var log storage.RequestLog
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &log) != nil || !strings.Contains(log.RequestBody, "hello") || !strings.Contains(log.ResponseBody, "world") {
		t.Fatalf("single-log bodies unavailable: %d %s", rr.Code, rr.Body)
	}
}

func TestConcurrentTimelineAndDiagramBuildBodiesOnce(t *testing.T) {
	h, repo := tracePerformanceFixture(t)
	repo.started, repo.proceed = make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			rr := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
			if i%2 == 0 {
				h.handleTraceTimeline(rr, req, "trace")
			} else {
				h.handleTraceDiagram(rr, req, "trace")
			}
			if rr.Code != 200 || !strings.Contains(rr.Body.String(), "hello") {
				t.Errorf("invalid response: %d %s", rr.Code, rr.Body)
			}
		})
	}
	<-repo.started
	close(repo.proceed)
	wg.Wait()
	if repo.bodyReads.Load() != 1 {
		t.Fatalf("concurrent views read full trace %d times", repo.bodyReads.Load())
	}
}

func TestTraceDiagramPaginationPreservesAllRequestsAndFullAPI(t *testing.T) {
	h, repo := tracePerformanceFixture(t)
	for i := 2; i <= 4; i++ {
		if err := repo.SaveLog(&storage.RequestLog{
			ID: fmt.Sprint(i), TraceID: "trace", TraceSeq: i, CreatedAt: time.Now().UTC(),
			Upstream: "test", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200,
			RequestBody:  `{"messages":[{"role":"user","content":"hello"}]}`,
			ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"world"}}]}`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query              string
		offset, count      int
		contains, excludes string
	}{
		{"?offset=0&limit=2", 0, 2, "＃2", "＃3"},
		{"?offset=2&limit=2", 2, 2, "＃3", "＃2"},
		{"", 0, 4, "＃4", "＃5"},
		{"?offset=2&limit=2&view=dependencies", 2, 2, "Request 3", "Request 2"},
		{"?offset=999&limit=2", 4, 0, "sequenceDiagram", "＃4"},
	} {
		rr := httptest.NewRecorder()
		h.handleTraceDiagram(rr, httptest.NewRequest("GET", "/"+tc.query, nil), "trace")
		var response struct {
			Mermaid string `json:"mermaid"`
			Offset  int    `json:"request_offset"`
			Total   int    `json:"request_total"`
			Count   int    `json:"request_count"`
		}
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &response) != nil || response.Offset != tc.offset || response.Total != 4 || response.Count != tc.count || !strings.Contains(response.Mermaid, tc.contains) || strings.Contains(response.Mermaid, tc.excludes) {
			t.Errorf("%s: %d %s", tc.query, rr.Code, rr.Body)
		}
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?offset=-1", "?offset=bad"} {
		rr := httptest.NewRecorder()
		h.handleTraceDiagram(rr, httptest.NewRequest("GET", "/"+query, nil), "trace")
		if rr.Code != http.StatusBadRequest {
			t.Errorf("invalid pagination %s: %d", query, rr.Code)
		}
	}
}
