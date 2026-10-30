package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestCorrelationStatusAndPartialTuningUpdate(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("trace_correlation:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	repo, err := storage.NewSQLiteRepository(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := New(cfg, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	readStatus := func() (enabled bool, maxEntries, idleMinutes int) {
		t.Helper()
		rr := request("GET", "/api/trace-correlation/status", "")
		var status struct {
			Enabled     bool `json:"enabled"`
			MaxEntries  int  `json:"max_entries"`
			IdleMinutes int  `json:"idle_minutes"`
		}
		if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &status) != nil {
			t.Fatalf("status: %d %s", rr.Code, rr.Body)
		}
		return status.Enabled, status.MaxEntries, status.IdleMinutes
	}

	if enabled, max, idle := readStatus(); !enabled || max != config.DefaultCorrelationMaxEntries || idle != config.DefaultCorrelationIdleMinutes {
		t.Fatalf("initial tuning: enabled=%v max=%d idle=%d", enabled, max, idle)
	}

	// 只更新空闲窗口:其余字段(含 enabled)必须保持不变
	if rr := request("PUT", "/api/config", `{"trace_correlation":{"idle_minutes":90}}`); rr.Code != 200 {
		t.Fatalf("update idle: %d %s", rr.Code, rr.Body)
	}
	if enabled, max, idle := readStatus(); !enabled || max != config.DefaultCorrelationMaxEntries || idle != 90 {
		t.Fatalf("after idle update: enabled=%v max=%d idle=%d", enabled, max, idle)
	}

	// 超出范围收敛到上限,已调字段不被覆盖
	if rr := request("PUT", "/api/config", `{"trace_correlation":{"max_entries":500000}}`); rr.Code != 200 {
		t.Fatalf("update max: %d %s", rr.Code, rr.Body)
	}
	if _, max, idle := readStatus(); max != config.MaxCorrelationMaxEntries || idle != 90 {
		t.Fatalf("after max update: max=%d idle=%d", max, idle)
	}

	// 现有前端只传 enabled 的旧请求形式不能清掉调优值
	if rr := request("PUT", "/api/config", `{"trace_correlation":{"enabled":false}}`); rr.Code != 200 {
		t.Fatalf("toggle enabled: %d %s", rr.Code, rr.Body)
	}
	if enabled, max, idle := readStatus(); enabled || max != config.MaxCorrelationMaxEntries || idle != 90 {
		t.Fatalf("after toggle: enabled=%v max=%d idle=%d", enabled, max, idle)
	}

	// 调优值随 Save 落盘,重载后仍然生效
	reloaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if snap := reloaded.CorrelationSnapshot(); snap.Enabled || snap.MaxEntries != config.MaxCorrelationMaxEntries || snap.IdleMinutes != 90 {
		t.Fatalf("persisted tuning: %+v", snap)
	}
}
