package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/analysis"
	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func reviewRegressionSearchSetup(t *testing.T, n int) (*reviewExecutor, *storage.SQLiteRepository, benchmark.Run) {
	t.Helper()
	repo, err := storage.NewSQLiteRepository(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	e, err := repo.SaveExperiment(benchmark.Experiment{Name: "audit", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	allowed := map[string]bool{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("audit-%03d", i)
		err = repo.SaveLog(&storage.RequestLog{ID: id, TraceID: "audit-task", CreatedAt: time.Now().Add(time.Duration(i) * time.Millisecond), Method: "POST", Path: "/v1/chat/completions", StatusCode: 503, ResponseBody: "needle evidence"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		allowed[id] = true
	}
	run, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "audit", Conditions: benchmark.Conditions{Agent: "audit"}, TraceIDs: []string{"audit-task"}})
	if err != nil {
		t.Fatal(err)
	}
	exec := &reviewExecutor{h: New(&config.Config{}, repo, nil, nil), tasks: []benchmark.ReviewTask{{RunID: run.ID, Metrics: benchmark.Metrics{LogIDs: ids}}}, allowed: allowed, remaining: 24, bytes: 128 << 10}
	return exec, repo, run
}

func TestReviewRegressionSearchLastPageKeepsCursor(t *testing.T) {
	exec, _, run := reviewRegressionSearchSetup(t, 25)
	args, _ := json.Marshal(map[string]any{"run_id": run.ID, "query": "needle"})
	out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
	if err != nil {
		t.Fatal(err)
	}
	shown := strings.Count(out, " log_id=")
	if shown < 25 && !strings.Contains(out, "next_offset=") {
		t.Errorf("25 matching logs: returned %d with no continuation cursor; %d matches are unreachable via advertised pagination", shown, 25-shown)
	}
}

func TestReviewRegressionSearchCursorVisitsAllMatches(t *testing.T) {
	exec, _, run := reviewRegressionSearchSetup(t, 400)
	cursor := 0
	seen := map[string]bool{}
	ids := regexp.MustCompile(`log_id=([^ ]+)`)
	next := regexp.MustCompile(`next_offset=(\d+)`)
	for page := 0; page < 24; page++ {
		args, _ := json.Marshal(map[string]any{"run_id": run.ID, "errors_only": true, "offset": cursor})
		out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range ids.FindAllStringSubmatch(out, -1) {
			seen[m[1]] = true
		}
		match := next.FindStringSubmatch(out)
		if match == nil {
			break
		}
		cursor, _ = strconv.Atoi(match[1])
	}
	if len(seen) != 400 {
		t.Errorf("following every returned cursor visited only %d/400 matching logs; %d silently skipped", len(seen), 400-len(seen))
	}
}

func TestReviewRegressionSearchRespectsFrozenReportMembership(t *testing.T) {
	exec, repo, run := reviewRegressionSearchSetup(t, 1)
	err := repo.SaveLog(&storage.RequestLog{ID: "outside-report", TraceID: "other-task", CreatedAt: time.Now(), Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, ResponseBody: "new-only-secret"})
	if err != nil {
		t.Fatal(err)
	}
	run.TraceIDs = []string{"other-task"}
	if run, err = repo.SaveExperimentRun(run); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]any{"run_id": run.ID, "query": "new-only-secret"})
	out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
	if err != nil {
		t.Fatal(err)
	}
	_, detailErr := exec.ExecuteToolCall(context.Background(), "get_request_detail", json.RawMessage(`{"log_id":"outside-report"}`))
	t.Logf("detail tool rejects same log: %v", detailErr)
	original, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", json.RawMessage(fmt.Sprintf(`{"run_id":%q,"query":"needle"}`, run.ID)))
	if err != nil || !strings.Contains(original, "log_id=audit-000 ") {
		t.Fatalf("original snapshot evidence no longer searchable: %s %v", original, err)
	}
	if strings.Contains(out, "outside-report") {
		t.Errorf("filtered search returned log and body snippet outside the report's frozen LogIDs: %s", out)
	}
}

func TestReviewRegressionUnicodeSnippetDoesNotPanic(t *testing.T) {
	defer func() {
		if v := recover(); v != nil {
			t.Errorf("keyword snippet panicked on valid Unicode: %v", v)
		}
	}()
	log := &storage.RequestLog{ResponseBody: strings.Repeat("Ⱥ", 100) + "needle"}
	requestMatchSnippet("needle", log)
}

func TestReviewRegressionUnicodeReviewCompletes(t *testing.T) {
	repo, experiment, runs := reviewAPISeed(t)
	entry, err := repo.GetLog("a")
	if err != nil {
		t.Fatal(err)
	}
	entry.ResponseBody = strings.Repeat("Ⱥ", 100) + "needle"
	if err = repo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []analysis.Message `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		for _, m := range payload.Messages {
			if m.Role == "tool" {
				reviewModelText(w, `{"summary":"Unicode evidence inspected"}`)
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		args, _ := json.Marshal(map[string]any{"run_id": runs[0].ID, "query": "needle"})
		reviewSSE(w, map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "audit-search", "function": map[string]any{"name": "get_task_requests", "arguments": string(args)}}}}}}})
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer model.Close()
	cfg := &config.Config{Analysis: config.AnalysisConfig{Enabled: true, BaseURL: model.URL, Model: "mock"}}
	mux := http.NewServeMux()
	New(cfg, repo, nil, nil).RegisterRoutes(mux)
	var caught any
	func() {
		defer func() { caught = recover() }()
		reviewRequest(mux, "POST", "/api/experiments/"+experiment.ID+"/review/analyze", map[string]any{"revision": experiment.Revision, "run_ids": []string{runs[0].ID, runs[1].ID}})
	}()
	reports, _, err := repo.ListReviewReports(experiment.ID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if caught != nil {
		t.Fatalf("review panicked: %v", caught)
	}
	if len(reports) != 1 || reports[0].Status != "completed" {
		t.Fatalf("unexpected report status: %+v", reports)
	}
}

// Unexpected tool/storage panics still finish the persisted report and release deletion protection.
type reviewPanicRepository struct{ storage.Repository }

func (r reviewPanicRepository) GetLog(string) (*storage.RequestLog, error) {
	panic("synthetic storage panic")
}
func TestReviewRegressionPanicFinishesReport(t *testing.T) {
	repo, e, runs := reviewAPISeed(t)
	report := benchmark.ReviewReport{ID: "panic-report", ExperimentID: e.ID, Status: "running", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: runs[0].ID, Metrics: benchmark.Metrics{LogIDs: []string{"a"}}}}}
	if err := repo.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	h := New(&config.Config{}, reviewPanicRepository{repo}, nil, nil)
	rr := httptest.NewRecorder()
	h.runReview(rr, context.Background(), repo, report, nil, "en", config.AnalysisConfig{})
	got, err := repo.GetReviewReport(report.ID)
	if err != nil || got.Status != "failed" || got.FinishedAt == nil || !strings.Contains(rr.Body.String(), `"type":"error"`) {
		t.Fatalf("panic left unfinished report: %+v %v %s", got, err, rr.Body)
	}
	if err = repo.DeleteReviewReport(report.ID, e.ID); err != nil {
		t.Fatal(err)
	}
}

func TestReviewRegressionPaginationBoundaries(t *testing.T) {
	for _, n := range []int{21, 199, 200, 201} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			exec, _, run := reviewRegressionSearchSetup(t, n)
			seen := map[string]bool{}
			offset := 0
			for page := 0; page < 24; page++ {
				args, _ := json.Marshal(map[string]any{"run_id": run.ID, "query": "needle", "offset": offset})
				out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range regexp.MustCompile(`log_id=([^ ]+)`).FindAllStringSubmatch(out, -1) {
					if seen[m[1]] {
						t.Fatal("duplicate result")
					}
					seen[m[1]] = true
				}
				next := regexp.MustCompile(`next_offset=(\d+)`).FindStringSubmatch(out)
				if next == nil {
					break
				}
				updated, _ := strconv.Atoi(next[1])
				if updated <= offset {
					t.Fatal("cursor did not advance")
				}
				offset = updated
			}
			if len(seen) != n {
				t.Fatalf("visited %d/%d", len(seen), n)
			}
		})
	}
}

func TestReviewRegressionPagingWithLongUnicodePathsAndSkippedRows(t *testing.T) {
	exec, repo, run := reviewRegressionSearchSetup(t, 25)
	for i := 0; i < 25; i++ {
		entry, err := repo.GetLog(fmt.Sprintf("audit-%03d", i))
		if err != nil {
			t.Fatal(err)
		}
		entry.Path = "/" + strings.Repeat("阶", 190) + "/chat/completions"
		if i < 3 {
			entry.Path = "/api/upload"
		}
		if err = repo.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	offset := 0
	for page := 0; page < 24; page++ {
		args, _ := json.Marshal(map[string]any{"run_id": run.ID, "errors_only": true, "offset": offset})
		out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > 12000 {
			t.Fatal("tool response exceeded byte budget")
		}
		for _, m := range regexp.MustCompile(`log_id=([^ ]+)`).FindAllStringSubmatch(out, -1) {
			if seen[m[1]] {
				t.Fatal("duplicate result")
			}
			seen[m[1]] = true
		}
		next := regexp.MustCompile(`next_offset=(\d+)`).FindStringSubmatch(out)
		if next == nil {
			break
		}
		updated, _ := strconv.Atoi(next[1])
		if updated <= offset {
			t.Fatal("cursor did not advance")
		}
		offset = updated
	}
	if len(seen) != 22 {
		t.Fatalf("visited %d/22 supported requests", len(seen))
	}
}
