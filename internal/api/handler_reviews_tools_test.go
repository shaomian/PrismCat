package api

import (
	"context"
	"encoding/json"
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

// get_task_requests 工具的三种形态:不带过滤的旧行为、关键词过滤(带命中
// 片段)、仅失败过滤(SupportedPath 之外的请求不出现),以及预算与成员校验。
func TestReviewToolGetTaskRequestsFilters(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := New(&config.Config{}, repo, nil, nil)

	e, err := repo.SaveExperiment(benchmark.Experiment{Name: "Compare agents", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "A", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Add(-time.Hour)
	logs := []*storage.RequestLog{
		{ID: "flag-log", TraceID: "task", CreatedAt: now, Upstream: "u", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 50, ResponseBody: `{"flag captured from target alpha"}`},
		{ID: "fail-log", TraceID: "task", CreatedAt: now.Add(time.Minute), Upstream: "u", Method: "POST", Path: "/v1/messages", StatusCode: 500, Latency: 100, Error: "boom timeout"},
		{ID: "plain-log", TraceID: "task", CreatedAt: now.Add(2 * time.Minute), Upstream: "u", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 60, ResponseBody: "normal answer"},
		{ID: "non-model", TraceID: "task", CreatedAt: now.Add(3 * time.Minute), Upstream: "u", Method: "POST", Path: "/api/upload", StatusCode: 500, Latency: 10, ResponseBody: "uploaded"},
	}
	for _, log := range logs {
		if err = repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}

	// trace 必须已有日志,运行才能关联
	run.TraceIDs = []string{"task"}
	if run, err = repo.SaveExperimentRun(run); err != nil {
		t.Fatal(err)
	}

	allowed := map[string]bool{"flag-log": true, "fail-log": true, "plain-log": true}
	exec := &reviewExecutor{
		h:         h,
		tasks:     []benchmark.ReviewTask{{RunID: run.ID, Name: "A", Metrics: benchmark.Metrics{LogIDs: []string{"flag-log", "fail-log", "plain-log"}}}},
		allowed:   allowed,
		remaining: 24,
		bytes:     128 << 10,
	}
	call := func(args string) (string, error) {
		return exec.ExecuteToolCall(context.Background(), "get_task_requests", json.RawMessage(args))
	}
	containsAll := func(haystack string, needles ...string) bool {
		for _, needle := range needles {
			if !strings.Contains(haystack, needle) {
				return false
			}
		}
		return true
	}

	// 不带过滤:保持旧的全量分页行为
	out, err := call(`{"run_id":"` + run.ID + `"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(out, "flag-log", "fail-log", "plain-log", "next_offset=3") || strings.Contains(out, "non-model") {
		t.Fatalf("unfiltered listing changed: %s", out)
	}

	// 关键词过滤:命中日志 + match 片段
	out, err = call(`{"run_id":"` + run.ID + `","query":"flag"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(out, "flag-log", "match:", "flag captured") || strings.Contains(out, "fail-log") {
		t.Fatalf("keyword filter: %s", out)
	}

	// 仅失败:失败的模型请求;非模型路径的失败请求被 SupportedPath 过滤
	out, err = call(`{"run_id":"` + run.ID + `","errors_only":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fail-log") || strings.Contains(out, "flag-log") || strings.Contains(out, "non-model") {
		t.Fatalf("errors_only filter: %s", out)
	}

	// 无命中:既没有日志行也没有 next_offset(窗口未拉满即数据见底)
	out, err = call(`{"run_id":"` + run.ID + `","query":"nothing-matches-this"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "log_id=") || strings.Contains(out, "next_offset") {
		t.Fatalf("empty match should end paging: %s", out)
	}

	// 不在本报告中的任务拒绝访问
	if _, err = call(`{"run_id":"some-other-run"}`); err == nil || !strings.Contains(err.Error(), "task not in this report") {
		t.Fatalf("foreign run accepted: %v", err)
	}

	// 预算扣减:每次调用消耗一次
	if exec.remaining != 24-5 {
		t.Fatalf("budget accounting: remaining=%d, want %d", exec.remaining, 24-5)
	}
}

// SSE 工具事件装饰器:调用前发 tool,返回后发 tool_result(含预览与剩余预算)。
func TestReviewToolTraceEvents(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	h := New(&config.Config{}, repo, nil, nil)
	e, err := repo.SaveExperiment(benchmark.Experiment{Name: "Trace events", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "A", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Hour)
	if err = repo.SaveLog(&storage.RequestLog{ID: "flag-log", TraceID: "task", CreatedAt: now, Upstream: "u", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 50, ResponseBody: `{"flag captured from target alpha"}`}); err != nil {
		t.Fatal(err)
	}
	run.TraceIDs = []string{"task"}
	if run, err = repo.SaveExperimentRun(run); err != nil {
		t.Fatal(err)
	}

	exec := &reviewExecutor{
		h:         h,
		tasks:     []benchmark.ReviewTask{{RunID: run.ID, Name: "A", Metrics: benchmark.Metrics{LogIDs: []string{"flag-log"}}}},
		allowed:   map[string]bool{"flag-log": true},
		remaining: 24,
		bytes:     128 << 10,
	}
	var events []map[string]any
	wrapped := &reviewToolTrace{inner: exec, send: func(v any) error {
		events = append(events, v.(map[string]any))
		return nil
	}}

	out, err := wrapped.ExecuteToolCall(context.Background(), "get_task_requests", json.RawMessage(`{"run_id":"`+run.ID+`","query":"flag"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "flag-log") {
		t.Fatalf("result missing log: %s", out)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0]["type"] != "tool" || events[0]["name"] != "get_task_requests" || !strings.Contains(events[0]["args"].(string), "flag") {
		t.Fatalf("tool event: %v", events[0])
	}
	if events[1]["type"] != "tool_result" || events[1]["name"] != "get_task_requests" {
		t.Fatalf("tool_result event: %v", events[1])
	}
	if chars, _ := events[1]["chars"].(int); chars == 0 {
		t.Fatalf("tool_result missing chars: %v", events[1])
	}
	if preview, _ := events[1]["preview"].(string); !strings.Contains(preview, "flag-log") {
		t.Fatalf("tool_result preview: %v", events[1])
	}
	if remaining, _ := events[1]["remaining"].(int); remaining != 23 {
		t.Fatalf("tool_result remaining: %v", events[1])
	}

	// 内部执行失败时,tool_result 携带错误信息
	exhausted := &reviewExecutor{h: h, tasks: exec.tasks, allowed: exec.allowed, remaining: 0, bytes: 128 << 10}
	wrapped = &reviewToolTrace{inner: exhausted, send: func(v any) error {
		events = append(events, v.(map[string]any))
		return nil
	}}
	if _, err = wrapped.ExecuteToolCall(context.Background(), "get_task_requests", json.RawMessage(`{"run_id":"`+run.ID+`"}`)); err == nil {
		t.Fatal("budget-exhausted call succeeded")
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	if events[3]["type"] != "tool_result" || events[3]["error"] == nil {
		t.Fatalf("failed tool_result event: %v", events[3])
	}
}

// DELETE /api/experiments/{id}/review/reports/{rid}:运行中 409、
// 不存在/跨卡片 404、已完成 200 且历史移除。
func TestDeleteReviewReportRoute(t *testing.T) {
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	mux := http.NewServeMux()
	New(&config.Config{}, repo, nil, nil).RegisterRoutes(mux)
	request := func(method, path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, path, nil))
		return rr
	}
	e, err := repo.SaveExperiment(benchmark.Experiment{Name: "Delete reports", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	other, err := repo.SaveExperiment(benchmark.Experiment{Name: "Other", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.CreateReviewReport(benchmark.ReviewReport{ID: "report-1", ExperimentID: e.ID, Status: "running", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	base := "/api/experiments/" + e.ID + "/review/reports"
	if rr := request("DELETE", base+"/report-1"); rr.Code != 409 {
		t.Fatalf("running report delete: %d %s", rr.Code, rr.Body)
	}
	if rr := request("DELETE", base+"/missing"); rr.Code != 404 {
		t.Fatalf("missing report delete: %d %s", rr.Code, rr.Body)
	}
	if rr := request("DELETE", "/api/experiments/"+other.ID+"/review/reports/report-1"); rr.Code != 404 {
		t.Fatalf("foreign report delete: %d %s", rr.Code, rr.Body)
	}
	if err = repo.FinishReviewReport(benchmark.ReviewReport{ID: "report-1", ExperimentID: e.ID, Status: "completed", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if rr := request("DELETE", base+"/report-1"); rr.Code != 200 {
		t.Fatalf("completed report delete: %d %s", rr.Code, rr.Body)
	}
	rr := request("GET", "/api/experiments/"+e.ID+"/review/reports")
	if !strings.Contains(rr.Body.String(), `"total":0`) {
		t.Fatalf("deleted report still listed: %s", rr.Body)
	}
	if rr = request("DELETE", base+"/report-1"); rr.Code != 404 {
		t.Fatalf("double delete: %d %s", rr.Code, rr.Body)
	}
}
