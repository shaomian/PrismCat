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

func TestTraceOriginMetadataAndListRoutes(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	for _, id := range []string{"a", "b"} {
		if err = repo.SaveLog(&storage.RequestLog{ID: id, TraceID: "client-task", CreatedAt: time.Now(), StatusCode: 200, Method: "POST", Path: "/v1/responses"}); err != nil {
			t.Fatal(err)
		}
	}
	h := New(&config.Config{}, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(raw))))
		return rr
	}
	v := storage.TraceGroupingRequest{Action: "split", TraceIDs: []string{"client-task"}, LogIDs: []string{"a"}}
	rr := request("POST", "/api/trace-grouping/preview", v)
	var p storage.TraceGroupingPreview
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &p) != nil {
		t.Fatalf("preview: %d %s", rr.Code, rr.Body)
	}
	v.Version = p.Version
	rr = request("POST", "/api/trace-grouping/apply", v)
	var result storage.TraceGroupingResult
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &result) != nil {
		t.Fatalf("split: %d %s", rr.Code, rr.Body)
	}
	rr = request("GET", "/api/traces/"+result.Target+"/metadata", nil)
	var m storage.TraceMetadata
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &m) != nil || m.Origin == nil || m.Origin.RequestCount != 1 {
		t.Fatalf("metadata: %d %s", rr.Code, rr.Body)
	}
	created := m.Origin.CreatedAt
	m.Name = "Renamed"
	m.Origin.SourceTraceIDs = []string{"forged"}
	m.Origin.RequestCount = 999
	rr = request("PUT", "/api/traces/"+result.Target+"/metadata", m)
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &m) != nil || m.Origin == nil || m.Origin.RequestCount != 1 || m.Origin.CreatedAt != created || len(m.Origin.SourceTraceIDs) != 1 || m.Origin.SourceTraceIDs[0] != "client-task" {
		t.Fatalf("readonly metadata: %d %s", rr.Code, rr.Body)
	}
	rr = request("GET", "/api/traces?q=Renamed&limit=1", nil)
	var page struct {
		Traces []storage.TraceSummary `json:"traces"`
	}
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &page) != nil || len(page.Traces) != 1 || page.Traces[0].Origin == nil || page.Traces[0].Origin.RequestCount != 1 {
		t.Fatalf("list: %d %s", rr.Code, rr.Body)
	}
}
