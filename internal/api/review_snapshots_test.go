package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func snapshotExecutorSetup(t *testing.T, n int) (*reviewExecutor, *storage.SQLiteRepository, benchmark.Run) {
	t.Helper()
	exec, repo, run := reviewRegressionSearchSetup(t, n)
	e, err := repo.GetExperiment(run.ExperimentID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repo.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{{RunID: run.ID, Revision: run.Revision}})
	if err != nil {
		t.Fatal(err)
	}
	report := benchmark.ReviewReport{ID: "snapshot-report", ExperimentID: e.ID, Status: "completed", CreatedAt: time.Now(), SnapshotID: snapshot.ID, EvidenceAt: &snapshot.CreatedAt, Tasks: snapshot.Tasks}
	if err := repo.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	exec.tasks, exec.snapshotID, exec.allowed = snapshot.Tasks, snapshot.ID, nil
	return exec, repo, run
}

func TestSnapshotToolPaginationVisitsAllMatchingMembers(t *testing.T) {
	exec, repo, run := snapshotExecutorSetup(t, 400)
	cursor := 0
	seen := map[string]bool{}
	ids := regexp.MustCompile(`log_id=([^ ]+)`)
	next := regexp.MustCompile(`next_offset=(\d+)`)
	for range 24 {
		args, _ := json.Marshal(map[string]any{"run_id": run.ID, "query": "needle", "offset": cursor})
		out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) > 12000 {
			t.Fatal("tool response exceeded byte budget")
		}
		for _, m := range ids.FindAllStringSubmatch(out, -1) {
			if seen[m[1]] {
				t.Fatalf("duplicate page member %s", m[1])
			}
			seen[m[1]] = true
		}
		match := next.FindStringSubmatch(out)
		if match == nil {
			break
		}
		cursor, _ = strconv.Atoi(match[1])
	}
	if len(seen) != 400 {
		t.Fatalf("pagination saw %d/400 frozen requests", len(seen))
	}
	// The same frozen membership is used by normal pages and by details.
	entry, err := repo.GetLog("audit-000")
	if err != nil {
		t.Fatal(err)
	}
	entry.Path = "/v1/" + strings.Repeat("路径", 180) + "/chat/completions"
	if err := repo.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	exec.remaining, exec.bytes = 24, 128<<10
	out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", json.RawMessage(fmt.Sprintf(`{"run_id":%q}`, run.ID)))
	if err != nil || len(out) > 12000 || !strings.Contains(out, "audit-000") {
		t.Fatalf("normal frozen page: %s %v", out, err)
	}
}

func TestSnapshotToolsAndCitationsStayInsideFrozenTask(t *testing.T) {
	exec, repo, run := snapshotExecutorSetup(t, 25)
	if err := repo.SaveLog(&storage.RequestLog{ID: "foreign", TraceID: "other", CreatedAt: time.Now(), Method: "POST", Path: "/v1/responses", StatusCode: 500, ResponseBody: "foreign-needle"}); err != nil {
		t.Fatal(err)
	}
	run.TraceIDs = []string{"other"}
	if _, err := repo.SaveExperimentRun(run); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{"foreign-needle", "needle"} {
		args, _ := json.Marshal(map[string]any{"run_id": run.ID, "query": query})
		out, err := exec.ExecuteToolCall(context.Background(), "get_task_requests", args)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "log_id=foreign ") {
			t.Fatal("search escaped snapshot")
		}
		if query == "needle" && !strings.Contains(out, "audit-000") {
			t.Fatal("old members became inaccessible")
		}
	}
	if _, err := exec.ExecuteToolCall(context.Background(), "get_request_detail", json.RawMessage(`{"log_id":"foreign"}`)); err == nil {
		t.Fatal("foreign detail was accepted")
	}
	if _, err := exec.ExecuteToolCall(context.Background(), "get_request_detail", json.RawMessage(`{"log_id":"audit-000"}`)); err != nil {
		t.Fatal(err)
	}
	report := benchmark.ReviewReport{SnapshotID: exec.snapshotID, Tasks: exec.tasks, Config: benchmark.ReviewConfig{Points: []benchmark.ReviewPoint{{ID: "finding", Name: "Finding"}}, Selected: []string{"finding"}}}
	output := benchmark.ReviewOutput{Summary: "scope", Observations: []benchmark.ReviewObservation{{PointID: "finding", Findings: []benchmark.ReviewFinding{{RunID: run.ID, Value: "evidence", Source: "logs", Evidence: []string{"foreign", "audit-000"}}}}}}
	normalized, err := exec.h.normalizeSnapshotReviewOutput(context.Background(), output, report, false)
	if err != nil || len(normalized.Observations[0].Findings[0].Evidence) != 1 || normalized.Observations[0].Findings[0].Evidence[0] != "audit-000" {
		t.Fatalf("citation escaped task: %+v %v", normalized, err)
	}
}

func TestFullComparisonPreservesOverlapWarningWithoutIDLists(t *testing.T) {
	repo, e, runs := reviewAPISeed(t)
	other, err := repo.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "same evidence", Conditions: benchmark.Conditions{Agent: "other"}, TraceIDs: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	h := New(&config.Config{}, repo, nil, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	rr := reviewRequest(mux, "GET", "/api/experiments/"+e.ID+"/compare?runs="+runs[0].ID+","+other.ID, nil)
	var comparison benchmark.Comparison
	if rr.Code != 200 || json.Unmarshal(rr.Body.Bytes(), &comparison) != nil {
		t.Fatalf("compare: %d %s", rr.Code, rr.Body)
	}
	found := false
	for _, warning := range comparison.Warnings {
		found = found || warning == "overlapping_logs"
	}
	if !found {
		t.Fatal("full comparison lost overlap warning")
	}
	for _, result := range comparison.Results {
		if len(result.Metrics.LogIDs) != 0 || result.Metrics.Requests != 1 {
			t.Fatalf("comparison retained full ID list: %+v", result.Metrics)
		}
	}
}
