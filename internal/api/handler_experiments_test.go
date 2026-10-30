package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func TestExperimentWorkflowComparisonAndOwnership(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	mux := http.NewServeMux()
	New(&config.Config{}, repo, nil, nil).RegisterRoutes(mux)
	request := func(method, path string, value any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(value)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(body))))
		return rr
	}
	e := benchmark.Experiment{Name: "Fix login", Keep: true}
	rr := request("POST", "/api/experiments", e)
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	if err = json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i, traceID := range []string{"task-a", "task-b"} {
		in, out := int64(100), int64(20)
		if err = repo.SaveLog(&storage.RequestLog{ID: traceID, TraceID: traceID, CreatedAt: time.Now().Add(time.Duration(i) * time.Second), Upstream: "agent", Method: "POST", Path: "/v1/chat/completions", RequestBody: `{"model":"m","messages":[{"role":"user","content":"fix"}]}`, ResponseBody: `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"one","function":{"name":"read_file","arguments":"{}"}}]}}]}`, StatusCode: 200, Latency: 100, UsageInputTokens: &in, UsageOutputTokens: &out}); err != nil {
			t.Fatal(err)
		}
		run := benchmark.Run{Name: traceID, State: "completed", Conditions: benchmark.Conditions{Agent: traceID, Model: "m"}, TraceIDs: []string{traceID}}
		rr = request("POST", "/api/experiments/"+e.ID+"/runs", run)
		if rr.Code != 200 {
			t.Fatalf("create run: %d %s", rr.Code, rr.Body)
		}
		if err = json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, run.ID)
		run.Revision = 0
		rr = request("PUT", "/api/experiments/"+e.ID+"/runs/"+run.ID, run)
		if rr.Code != 409 {
			t.Fatalf("stale update accepted: %d %s", rr.Code, rr.Body)
		}
	}
	rr = request("GET", "/api/experiments/"+e.ID+"/compare?runs="+strings.Join(ids, ","), nil)
	if rr.Code != 200 {
		t.Fatalf("compare: %d %s", rr.Code, rr.Body)
	}
	var c benchmark.Comparison
	if err = json.Unmarshal(rr.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Results) != 2 || c.Results[0].Metrics.Requests != 1 || c.Results[0].Metrics.UsageKnown != 1 || c.Results[0].Metrics.TotalTokens == nil || *c.Results[0].Metrics.TotalTokens != 120 || c.Results[0].Metrics.SpanMS == nil || *c.Results[0].Metrics.SpanMS != 100 {
		t.Fatalf("wrong metrics: %+v", c)
	}
	rr = request("GET", "/api/experiments/"+e.ID+"/export?runs="+strings.Join(ids, ","), nil)
	if rr.Code != 200 || rr.Header().Get("Content-Disposition") == "" {
		t.Fatal("evidence export failed")
	}
	if rr = request("GET", "/api/experiments/"+e.ID+"/compare?runs="+ids[0]+","+ids[0], nil); rr.Code != 400 {
		t.Fatal("duplicate run accepted")
	}
	other := e
	other.ID = ""
	rr = request("POST", "/api/experiments", other)
	if err = json.Unmarshal(rr.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	if rr = request("GET", "/api/experiments/"+other.ID+"/runs/"+ids[0], nil); rr.Code != 404 {
		t.Fatal("foreign run exposed under another experiment")
	}
	if rr = request("GET", "/api/experiments/"+other.ID+"/compare?runs="+strings.Join(ids, ","), nil); rr.Code != 404 {
		t.Fatal("cross-experiment comparison allowed")
	}
	rr = request("PUT", "/api/traces/task-a/metadata", storage.TraceMetadata{Name: "Login task", Pinned: true})
	if rr.Code != 200 {
		t.Fatalf("metadata: %d %s", rr.Code, rr.Body)
	}
	rr = request("GET", "/api/traces?q=Login&pinned=true", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Login task") {
		t.Fatalf("name search failed: %s", rr.Body)
	}
	rr = request("GET", "/api/traces/task-a/metadata", nil)
	var metadata storage.TraceMetadata
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &metadata) != nil {
		t.Fatalf("load metadata: %d %s", rr.Code, rr.Body)
	}
	metadata.TopAt = 1
	rr = request("PUT", "/api/traces/task-a/metadata", metadata)
	if rr.Code != 200 {
		t.Fatalf("pin to top: %d %s", rr.Code, rr.Body)
	}
	var pinned storage.TraceMetadata
	if err = json.Unmarshal(rr.Body.Bytes(), &pinned); err != nil || pinned.TopAt <= 1 || !pinned.Pinned {
		t.Fatalf("top position changed favorite or lacks server time: %+v %v", pinned, err)
	}
	if rr = request("PUT", "/api/traces/task-a/metadata", metadata); rr.Code != 409 {
		t.Fatalf("stale top update accepted: %d %s", rr.Code, rr.Body)
	}
	rr = request("GET", "/api/traces?limit=1&offset=0", nil)
	var page struct {
		Traces []storage.TraceSummary `json:"traces"`
	}
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &page) != nil || len(page.Traces) != 1 || page.Traces[0].TraceID != "task-a" {
		t.Fatalf("top was not ordered before pagination: %d %s", rr.Code, rr.Body)
	}
	pinned.TopAt = -1
	if rr = request("PUT", "/api/traces/task-a/metadata", pinned); rr.Code != 400 {
		t.Fatalf("negative top position accepted: %d %s", rr.Code, rr.Body)
	}
}

// 历史上统计截断在前 2000 条,超长任务的耗时/请求数/Token 会失真,
// 且复盘 AI 的证据分页只能看到被截断的日志清单。放开上限后统计必须全量。
func TestExperimentStatisticsCountBeyondLegacySamplingCap(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	mux := http.NewServeMux()
	h := New(&config.Config{}, repo, nil, nil)
	h.RegisterRoutes(mux)
	request := func(method, path string, value any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(value)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(body))))
		return rr
	}
	e := benchmark.Experiment{Name: "Big task", Keep: true}
	rr := request("POST", "/api/experiments", e)
	if rr.Code != 200 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	if err = json.Unmarshal(rr.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}

	const total = 2005
	base := time.Now().Add(-time.Hour)
	for i := 0; i < total; i++ {
		if err = repo.SaveLog(&storage.RequestLog{
			ID:         fmt.Sprintf("big-log-%d", i),
			TraceID:    "big-task",
			CreatedAt:  base.Add(time.Duration(i) * time.Second),
			Upstream:   "agent",
			Method:     "POST",
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			Latency:    10,
		}); err != nil {
			t.Fatalf("save log %d: %v", i, err)
		}
	}
	if err = repo.SaveLog(&storage.RequestLog{ID: "small-log-1", TraceID: "small-task", CreatedAt: base, Upstream: "agent", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 10}); err != nil {
		t.Fatal(err)
	}

	runIDs := []string{}
	for _, traceID := range []string{"big-task", "small-task"} {
		run := benchmark.Run{Name: traceID, State: "completed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{traceID}}
		rr = request("POST", "/api/experiments/"+e.ID+"/runs", run)
		if rr.Code != 200 {
			t.Fatalf("create run %s: %d %s", traceID, rr.Code, rr.Body)
		}
		if err = json.Unmarshal(rr.Body.Bytes(), &run); err != nil {
			t.Fatal(err)
		}
		runIDs = append(runIDs, run.ID)
	}

	// 统计单元:全部 2005 条都要计入,且不再标记 sampled
	bigRun := benchmark.Run{Name: "big-task", State: "completed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"big-task"}}
	rr = request("GET", "/api/experiments/"+e.ID+"/runs/"+runIDs[0], nil)
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &bigRun) != nil {
		t.Fatalf("load run: %d %s", rr.Code, rr.Body)
	}
	result, err := h.measureExperimentRun(httptest.NewRequest(http.MethodGet, "/", nil), repo, bigRun)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.Requests != total {
		t.Fatalf("requests = %d, want %d (legacy cap truncated statistics)", result.Metrics.Requests, total)
	}
	if result.Metrics.Sampled {
		t.Fatal("sampled = true, want false")
	}
	// 复盘 AI 的证据分页遍历这份 LogIDs,必须是全量
	if len(result.Metrics.LogIDs) != total {
		t.Fatalf("log ids = %d, want %d", len(result.Metrics.LogIDs), total)
	}

	// 对比接口:统计全量,但响应不携带逐条日志 ID(与 publicReview 同策略)
	rr = request("GET", "/api/experiments/"+e.ID+"/compare?runs="+strings.Join(runIDs, ","), nil)
	if rr.Code != 200 {
		t.Fatalf("compare: %d %s", rr.Code, rr.Body)
	}
	var c benchmark.Comparison
	if err = json.Unmarshal(rr.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(c.Results))
	}
	for _, res := range c.Results {
		if len(res.Metrics.LogIDs) != 0 {
			t.Fatalf("compare response leaked %d log IDs", len(res.Metrics.LogIDs))
		}
	}
	if res := c.Results[0]; res.Metrics.Requests != total || res.Metrics.Sampled {
		t.Fatalf("big run metrics: requests=%d sampled=%v", res.Metrics.Requests, res.Metrics.Sampled)
	}
	if got := strings.Contains(rr.Body.String(), "big-log-0"); got {
		t.Fatal("compare response contains individual log IDs")
	}
}
