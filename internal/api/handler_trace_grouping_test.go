package api

import (
	"encoding/json"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTraceGroupingRoutesExplicitSplitAndOldLink(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := New(&config.Config{}, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(path string, v storage.TraceGroupingRequest) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(v)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(string(raw))))
		return rr
	}
	for _, id := range []string{"e1", "e2"} {
		if err = repo.SaveLog(&storage.RequestLog{ID: id, TraceID: "explicit", CreatedAt: time.Now(), StatusCode: 200, Method: "POST", Path: "/v1/messages"}); err != nil {
			t.Fatal(err)
		}
	}
	v := storage.TraceGroupingRequest{Action: "split", TraceIDs: []string{"explicit"}, LogIDs: []string{"e1", "e2"}}
	rr := request("/api/trace-grouping/preview", v)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var p storage.TraceGroupingPreview
	json.Unmarshal(rr.Body.Bytes(), &p)
	if !p.CanApply {
		t.Fatalf("split blocked: %+v", p)
	}
	if rr = request("/api/trace-grouping/apply", v); rr.Code != 409 {
		t.Fatal("apply without preview accepted")
	}
	v.Version = p.Version
	rr = request("/api/trace-grouping/apply", v)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var result storage.TraceGroupingResult
	json.Unmarshal(rr.Body.Bytes(), &result)
	if result.LogCount != 2 {
		t.Fatal("batch not applied")
	}
	if rr = request("/api/trace-grouping/unknown", v); rr.Code != 404 {
		t.Fatal("unknown endpoint accepted")
	}
	if rr = request("/api/trace-grouping/preview", storage.TraceGroupingRequest{Action: "other"}); rr.Code != 400 {
		t.Fatal("invalid action accepted")
	}
	// Legacy automatic/manual grouping provides mergeable fixtures without client headers.
	for _, id := range []string{"a", "b"} {
		if err = repo.SaveLog(&storage.RequestLog{ID: id, CreatedAt: time.Now(), StatusCode: 200, Method: "POST", Path: "/v1/responses"}); err != nil {
			t.Fatal(err)
		}
		if _, err = repo.AssignTrace(id, id, 0); err != nil {
			t.Fatal(err)
		}
	}
	v = storage.TraceGroupingRequest{Action: "merge", TraceIDs: []string{"a"}, Target: "b"}
	rr = request("/api/trace-grouping/preview", v)
	json.Unmarshal(rr.Body.Bytes(), &p)
	v.Version = p.Version
	if rr = request("/api/trace-grouping/apply", v); rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("GET", "/api/traces/a", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"trace_id":"b"`) {
		t.Fatalf("old trace URL did not resolve: %d %s", rr.Code, rr.Body)
	}
}
