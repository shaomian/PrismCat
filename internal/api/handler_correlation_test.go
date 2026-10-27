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

func TestCaptureAndCorrectionRoutes(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	cfg := &config.Config{Correlation: config.CorrelationConfig{Enabled: true}, Upstreams: map[string]config.UpstreamConfig{"agent-a": {Target: "http://localhost:9999"}}}
	h := New(cfg, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	rr := request("POST", "/api/capture-runs", `{"name":"experiment","upstream":"agent-a"}`)
	if rr.Code != 200 {
		t.Fatalf("start: %d %s", rr.Code, rr.Body)
	}
	var run storage.CaptureRun
	if err := json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
		t.Fatal(err)
	}
	if rr := request("POST", "/api/capture-runs", `{"name":"overlap","upstream":"agent-a"}`); rr.Code != 409 {
		t.Fatal("overlap allowed")
	}
	entry := &storage.RequestLog{ID: "log", CreatedAt: time.Now(), Upstream: "agent-a", Method: "POST", Path: "/v1/responses", CaptureRunID: run.ID}
	if err := repo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	rr = request("GET", "/api/logs?capture_run_id="+run.ID, "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"id":"log"`) {
		t.Fatalf("capture filter: %s", rr.Body)
	}
	rr = request("PATCH", "/api/logs/log/trace", `{"trace_id":"reviewed","revision":0}`)
	if rr.Code != 200 {
		t.Fatalf("correction: %d %s", rr.Code, rr.Body)
	}
	rr = request("PATCH", "/api/logs/log/trace", `{"trace_id":"stale","revision":0}`)
	if rr.Code != 409 {
		t.Fatalf("stale correction status %d", rr.Code)
	}
	rr = request("GET", "/api/logs/log/trace", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"source":"manual"`) {
		t.Fatalf("metadata: %s", rr.Body)
	}
	rr = request("GET", "/api/traces/reviewed", "")
	if rr.Code != 200 {
		t.Fatalf("trace detail: %s", rr.Body)
	}
	rr = request("GET", "/api/logs/export?trace_id=reviewed", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"trace_id":"reviewed"`) {
		t.Fatalf("trace export mismatch: %s", rr.Body)
	}
	if rr := request("POST", "/api/capture-runs/"+run.ID+"/stop", ""); rr.Code != 200 {
		t.Fatalf("stop: %s", rr.Body)
	}
	if repo.CaptureRunForUpstream("agent-a") != "" {
		t.Fatal("stop did not release entry")
	}
}

func TestCaptureStatusAndPagedHistoryWithAsyncRepository(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Upstreams: map[string]config.UpstreamConfig{"agent-a": {Target: "http://localhost:9999"}}}
	async := storage.NewAsyncRepository(repo, cfg, 16)
	defer async.Close()
	h := New(cfg, async, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	readStatus := func() struct {
		Enabled bool `json:"enabled"`
		Active  []struct {
			ID       string `json:"id"`
			Upstream string `json:"upstream"`
		} `json:"active_captures"`
		Queued  int `json:"queued_bytes"`
		Dropped int `json:"dropped_observations"`
	} {
		t.Helper()
		rr := request("GET", "/api/trace-correlation/status", "")
		var status struct {
			Enabled bool `json:"enabled"`
			Active  []struct {
				ID       string `json:"id"`
				Upstream string `json:"upstream"`
			} `json:"active_captures"`
			Queued  int `json:"queued_bytes"`
			Dropped int `json:"dropped_observations"`
		}
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &status) != nil {
			t.Fatalf("status: %d %s", rr.Code, rr.Body)
		}
		return status
	}
	if status := readStatus(); status.Enabled || status.Active == nil || len(status.Active) != 0 {
		t.Fatalf("initial status: %+v", status)
	}
	if rr := request("POST", "/api/capture-runs", `{"name":"single","upstream":"agent-a","single_task":true}`); rr.Code != 400 {
		t.Fatalf("single task allowed without correlation: %d", rr.Code)
	}
	for i := 0; i < 3; i++ {
		rr := request("POST", "/api/capture-runs", `{"name":"普通录制","upstream":"agent-a"}`)
		var run storage.CaptureRun
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &run) != nil {
			t.Fatalf("start without correlation: %d %s", rr.Code, rr.Body)
		}
		status := readStatus()
		if status.Enabled || len(status.Active) != 1 || status.Active[0].ID != run.ID || status.Active[0].Upstream != "agent-a" {
			t.Fatalf("active status: %+v", status)
		}
		if i < 2 {
			if rr := request("POST", "/api/capture-runs/"+run.ID+"/stop", ""); rr.Code != 200 {
				t.Fatalf("stop: %d %s", rr.Code, rr.Body)
			}
			if len(readStatus().Active) != 0 {
				t.Fatal("stopped capture still active")
			}
		}
	}
	rr := request("GET", "/api/capture-runs?limit=2&offset=0", "")
	var page struct {
		Runs  []storage.CaptureRun `json:"runs"`
		Total int                  `json:"total"`
		Limit int                  `json:"limit"`
	}
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &page) != nil || len(page.Runs) != 2 || page.Total != 3 || page.Limit != 2 || page.Runs[0].StoppedAt != nil {
		t.Fatalf("first page: %d %s", rr.Code, rr.Body)
	}
	rr = request("GET", "/api/capture-runs?limit=2&offset=2", "")
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &page) != nil || len(page.Runs) != 1 || page.Total != 3 {
		t.Fatalf("last page: %d %s", rr.Code, rr.Body)
	}
	for _, query := range []string{"limit=0", "limit=101", "offset=-1", "offset=invalid"} {
		if rr := request("GET", "/api/capture-runs?"+query, ""); rr.Code != 400 {
			t.Fatalf("invalid pagination accepted: %s", query)
		}
	}
}
