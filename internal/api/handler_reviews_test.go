package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func reviewAPISeed(t *testing.T) (*storage.SQLiteRepository, benchmark.Experiment, []benchmark.Run) {
	t.Helper()
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "review.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	c := benchmark.ReviewConfig{TemplateName: "Any scenario", Points: []benchmark.ReviewPoint{{ID: "tokens", Name: "Tokens", Metric: "total_tokens"}, {ID: "finding", Name: "Observed results"}}, Selected: []string{"tokens", "finding"}}
	e, err := repo.SaveExperiment(benchmark.Experiment{Name: "Review", Keep: true, Review: &c})
	if err != nil {
		t.Fatal(err)
	}
	var runs []benchmark.Run
	for _, id := range []string{"a", "b", "secret"} {
		in, out := int64(100), int64(20)
		log := &storage.RequestLog{ID: id, TraceID: id, CreatedAt: time.Now(), Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 50, UsageInputTokens: &in, UsageOutputTokens: &out, RequestBody: `{"messages":[{"role":"user","content":"Find the sample result"}]}`, ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"This is recorded evidence, not independent validation."}}]}`}
		if err = repo.SaveLog(log); err != nil {
			t.Fatal(err)
		}
		if id != "secret" {
			v, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: id, Conditions: benchmark.Conditions{Agent: id}, TraceIDs: []string{id}, State: "completed"})
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, v)
		}
	}
	own := &storage.RequestLog{ID: "own-analysis", TraceID: "a", CreatedAt: time.Now(), Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Tag: "prismcat-analysis", Latency: 9999}
	cost := int64(9000)
	own.UsageInputTokens = &cost
	own.UsageOutputTokens = &cost
	if err = repo.SaveLog(own); err != nil {
		t.Fatal(err)
	}
	return repo, e, runs
}
func reviewRequest(mux *http.ServeMux, method, path string, v any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(v)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(raw))))
	return rr
}
func reviewSSE(w http.ResponseWriter, v any) {
	raw, _ := json.Marshal(v)
	fmt.Fprintf(w, "data: %s\n\n", raw)
}
func reviewModelText(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	reviewSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}})
	reviewSSE(w, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}
func TestAIReviewEvidenceScopingSnapshotsAndSuggestions(t *testing.T) {
	repo, e, runs := reviewAPISeed(t)
	var calls atomic.Int64
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		messages := string(payload["messages"])
		if r.Header.Get("X-PrismCat-Tag") != "prismcat-analysis" {
			t.Error("review requests not tagged separately")
		}
		if strings.Contains(messages, "Suggest up to five") {
			reviewModelText(w, `{"suggestions":[{"name":"Tokens","instructions":"duplicate"},{"name":"Did it verify?","instructions":"Check actual output"}]}`)
			return
		}
		if !strings.Contains(messages, `"role":"tool"`) {
			w.Header().Set("Content-Type", "text/event-stream")
			reviewSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
				map[string]any{"index": 0, "id": "bad", "function": map[string]any{"name": "get_request_detail", "arguments": `{"log_id":"secret"}`}},
				map[string]any{"index": 1, "id": "good", "function": map[string]any{"name": "get_request_detail", "arguments": `{"log_id":"a"}`}},
			}}}}})
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		if !strings.Contains(messages, "log not in selected tasks") || !strings.Contains(messages, "recorded evidence") {
			t.Error("scope guard or valid evidence read failed")
		}
		output := benchmark.ReviewOutput{Summary: "Mock review; inspect the evidence", Observations: []benchmark.ReviewObservation{
			{PointID: "tokens", Findings: []benchmark.ReviewFinding{{RunID: runs[0].ID, Value: "999999", Source: "logs", Evidence: []string{"a"}}}},
			{PointID: "finding", Findings: []benchmark.ReviewFinding{{RunID: runs[0].ID, Value: "one recorded claim", Source: "logs", Evidence: []string{"a", "b", "secret", "invented"}}}},
		}, Recommendations: []string{"Verify the result independently"}}
		if strings.Contains(messages, "Follow-up question:") {
			output.Summary = "Follow-up based on saved snapshot"
			output.Observations = nil
		}
		raw, _ := json.Marshal(output)
		reviewModelText(w, string(raw))
	}))
	defer model.Close()
	cfg := &config.Config{Analysis: config.AnalysisConfig{Enabled: true, BaseURL: model.URL, Model: "mock-reviewer"}}
	cfg.Analysis.APIKey = "private-test-key"
	h := New(cfg, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	status := reviewRequest(mux, "GET", "/api/analysis/config", nil)
	if status.Code != 200 || strings.Contains(status.Body.String(), "private-test-key") || !strings.Contains(status.Body.String(), `"has_api_key":true`) {
		t.Fatal("analysis status endpoint exposed credentials or failed")
	}
	path := "/api/experiments/" + e.ID + "/review"
	request := map[string]any{"revision": e.Revision, "run_ids": []string{runs[0].ID, runs[1].ID}, "language": "zh"}
	rr := reviewRequest(mux, "POST", path+"/analyze", request)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"type":"done"`) {
		t.Fatalf("review failed: %d %s", rr.Code, rr.Body)
	}
	summaries, _, err := repo.ListReviewReports(e.ID, 0, 10)
	if err != nil || len(summaries) != 1 {
		t.Fatal("report not saved")
	}
	report, err := repo.GetReviewReport(summaries[0].ID)
	if err != nil || report.Status != "completed" {
		t.Fatalf("invalid stored report: %+v %v", report, err)
	}
	if report.Output.Observations[0].Findings[0].Value != "120" || report.Output.Observations[0].Findings[0].Source != "statistics" {
		t.Fatal("model overrode authoritative token counts")
	}
	f := report.Output.Observations[1].Findings[0]
	if len(f.Evidence) != 1 || f.Evidence[0] != "a" {
		t.Fatalf("foreign evidence not removed: %+v", f)
	}
	rr = reviewRequest(mux, "GET", path+"/reports/"+report.ID, nil)
	var public benchmark.ReviewReport
	json.Unmarshal(rr.Body.Bytes(), &public)
	if len(public.Tasks[0].Metrics.LogIDs) != 0 || len(report.Tasks[0].Metrics.LogIDs) != 1 {
		t.Fatal("public report leaked IDs or modified stored evidence")
	}
	if rr = reviewRequest(mux, "GET", path+"/reports/"+report.ID+"/logs/secret", nil); rr.Code != 404 {
		t.Fatal("foreign evidence exposed")
	}
	if rr = reviewRequest(mux, "GET", path+"/reports/"+report.ID+"/logs/a", nil); rr.Code != 200 {
		t.Fatalf("valid evidence could not open: %s", rr.Body)
	}
	draft := *e.Review
	draft.Selected = nil
	rr = reviewRequest(mux, "POST", path+"/suggest", map[string]any{"revision": e.Revision, "run_ids": []string{runs[0].ID}, "config": draft})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Did it verify?") {
		t.Fatalf("suggestion failed: %s", rr.Body)
	}
	var suggested struct {
		Points []benchmark.ReviewPoint `json:"points"`
	}
	json.Unmarshal(rr.Body.Bytes(), &suggested)
	if len(suggested.Points) != 1 || suggested.Points[0].Metric != "" {
		t.Fatal("suggestions duplicated or silently became automatic metrics")
	}
	// Follow-ups use the report snapshot even after current task membership changes.
	runs[0].TraceIDs = []string{"secret"}
	if _, err = repo.SaveExperimentRun(runs[0]); err != nil {
		t.Fatal(err)
	}
	rr = reviewRequest(mux, "POST", path+"/analyze", map[string]any{"parent_id": report.ID, "question": "Explain the evidence"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Follow-up based on saved snapshot") {
		t.Fatalf("followup failed: %s", rr.Body)
	}
	history, _, _ := repo.ListReviewReports(e.ID, 0, 10)
	followup, _ := repo.GetReviewReport(history[0].ID)
	if followup.Tasks[0].Metrics.LogIDs[0] != "a" || followup.ParentID != report.ID {
		t.Fatal("follow-up silently used new task logs")
	}
	oldCalls := calls.Load()
	request["revision"] = 0
	if rr = reviewRequest(mux, "POST", path+"/analyze", request); rr.Code != 409 || calls.Load() != oldCalls {
		t.Fatal("stale card triggered AI")
	}
	other, err := repo.SaveExperiment(benchmark.Experiment{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if rr = reviewRequest(mux, "GET", "/api/experiments/"+other.ID+"/review/reports/"+report.ID, nil); rr.Code != 404 {
		t.Fatal("foreign report exposed")
	}
}

type cancelOnReady struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *cancelOnReady) Write(b []byte) (int, error) {
	if strings.Contains(string(b), `"type":"ready"`) {
		w.cancel()
	}
	return w.ResponseRecorder.Write(b)
}
func TestAIReviewCancellationPersistsAndReleasesCapacity(t *testing.T) {
	repo, e, runs := reviewAPISeed(t)
	h := New(&config.Config{Analysis: config.AnalysisConfig{Enabled: true, BaseURL: "http://127.0.0.1:1", Model: "mock"}}, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	raw, _ := json.Marshal(map[string]any{"revision": e.Revision, "run_ids": []string{runs[0].ID, runs[1].ID}})
	req := httptest.NewRequest("POST", "/api/experiments/"+e.ID+"/review/analyze", strings.NewReader(string(raw))).WithContext(ctx)
	rr := &cancelOnReady{httptest.NewRecorder(), cancel}
	mux.ServeHTTP(rr, req)
	values, _, _ := repo.ListReviewReports(e.ID, 0, 10)
	if len(values) != 1 || values[0].Status != "canceled" {
		t.Fatalf("cancellation not saved: %+v %s", values, rr.Body)
	}
	if !h.acquireReview(e.ID) {
		t.Fatal("canceled analysis leaked capacity")
	}
	h.releaseReview(e.ID)
	if !h.acquireReview("one") || !h.acquireReview("two") || h.acquireReview("three") || h.acquireReview("one") {
		t.Fatal("review concurrency bound failed")
	}
	h.releaseReview("one")
	h.releaseReview("two")
}

func TestAIReviewFinalizesToolTextAndPersistsFailures(t *testing.T) {
	for _, recoverable := range []bool{true, false} {
		t.Run(fmt.Sprint("recoverable=", recoverable), func(t *testing.T) {
			repo, e, runs := reviewAPISeed(t)
			var calls atomic.Int64
			model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				var payload map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if n == 1 {
					reviewSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{
						map[string]any{"index": 0, "id": "read-response", "function": map[string]any{"name": "get_request_detail", "arguments": `{"log_id":"a","part":"response"}`}},
					}}}}})
					reviewModelText(w, "")
					return
				}
				messages := string(payload["messages"])
				if !strings.Contains(messages, "This is recorded evidence") {
					t.Error("requested response evidence missing")
				}
				if n == 3 && len(payload["tools"]) > 0 {
					t.Error("format recovery still allows tool calls")
				}
				if n == 3 && recoverable {
					output := benchmark.ReviewOutput{Summary: "Report from collected evidence", Observations: []benchmark.ReviewObservation{{PointID: "finding", Findings: []benchmark.ReviewFinding{{RunID: runs[0].ID, Value: "recorded claim", Source: "logs", Evidence: []string{"a"}}}}}}
					raw, _ := json.Marshal(output)
					reviewModelText(w, string(raw))
					return
				}
				// 复现真实响应：HTTP 200，但 content 是 DSML 而不是 JSON 报告。
				reviewModelText(w, `<｜｜DSML｜｜ calls><｜｜DSML｜｜ invoke name="get_request_detail"></｜｜DSML｜｜ invoke></｜｜DSML｜｜ calls>`)
			}))
			defer model.Close()
			h := New(&config.Config{Analysis: config.AnalysisConfig{Enabled: true, BaseURL: model.URL, Model: "mock-deepseek"}}, repo, nil, nil)
			mux := http.NewServeMux()
			h.RegisterRoutes(mux)
			rr := reviewRequest(mux, "POST", "/api/experiments/"+e.ID+"/review/analyze", map[string]any{"revision": e.Revision, "run_ids": []string{runs[0].ID, runs[1].ID}})
			if rr.Code != 200 || calls.Load() != 3 {
				t.Fatalf("recovery was not bounded: requests=%d response=%s", calls.Load(), rr.Body)
			}
			summaries, _, err := repo.ListReviewReports(e.ID, 0, 10)
			if err != nil || len(summaries) != 1 {
				t.Fatalf("report missing: %v %v", summaries, err)
			}
			report, err := repo.GetReviewReport(summaries[0].ID)
			if err != nil || report.Usage["total_tokens"] != float64(360) {
				t.Fatalf("recovery usage missing: %v %v", report.Usage, err)
			}
			if recoverable {
				if report.Status != "completed" || report.Output == nil || report.Output.Summary != "Report from collected evidence" || !strings.Contains(rr.Body.String(), `"type":"done"`) {
					t.Fatalf("recovered report not saved: %+v %s", report, rr.Body)
				}
			} else if report.Status != "failed" || report.Error != "review_tool_calls_unfinished" || report.Output != nil || !strings.Contains(rr.Body.String(), `"error":"review_tool_calls_unfinished"`) {
				t.Fatalf("tool markup accepted or incorrect error saved: %+v %s", report, rr.Body)
			}
			if !h.acquireReview(e.ID) {
				t.Fatal("finalization leaked review capacity")
			}
			h.releaseReview(e.ID)
		})
	}
}

type reviewCountingRepo struct {
	storage.Repository
	reads int
}

func (r *reviewCountingRepo) GetLog(id string) (*storage.RequestLog, error) {
	r.reads++
	return &storage.RequestLog{ID: id, Path: "/v1/chat/completions", StatusCode: 200, RequestBody: `{"messages":[{"role":"user","content":"sample"}]}`, ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"sample"}}]}`}, nil
}
func TestReviewContextDoesNotLoadAllLogs(t *testing.T) {
	repo := &reviewCountingRepo{}
	h := New(&config.Config{}, repo, nil, nil)
	tasks := []benchmark.ReviewTask{}
	for n := 0; n < 8; n++ {
		task := benchmark.ReviewTask{RunID: fmt.Sprint(n), Notes: strings.Repeat("large note ", 3000)}
		for i := 0; i < 2000; i++ {
			task.Metrics.LogIDs = append(task.Metrics.LogIDs, fmt.Sprintf("%d-%d", n, i))
		}
		tasks = append(tasks, task)
	}
	text := h.reviewContext(context.Background(), tasks, true)
	if repo.reads > 8*15 || len([]rune(text)) > 50000 {
		t.Fatalf("unbounded context: reads=%d chars=%d", repo.reads, len([]rune(text)))
	}
	for _, task := range tasks {
		if !strings.Contains(text, "run_id="+task.RunID) {
			t.Fatal("context budget dropped an entire task")
		}
	}
}
