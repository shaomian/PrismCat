package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

func seedSnapshotTask(t *testing.T, n int) (*SQLiteRepository, benchmark.Experiment, benchmark.Run) {
	t.Helper()
	r := mustNewSQLiteRepoForTest(t)
	t.Cleanup(func() { r.Close() })
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO request_logs(id,created_at,created_at_unix_ms,upstream,target_url,method,path,status_code,latency_ms,error,trace_id,response_body,usage_input_tokens,usage_output_tokens,usage_total_tokens) VALUES(?,?,?,'snapshot','','POST','/v1/responses',?,5,'','snapshot-source',?,100,10,110)`)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	for i := range n {
		status, body := 200, "ordinary"
		if i == n-1 {
			status, body = 503, "tail-evidence"
		}
		created := at.Add(time.Duration(i) * time.Millisecond)
		if _, err := statement.Exec(fmt.Sprintf("snap-%06d", i), created, created.UnixMilli(), status, body); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "snapshot", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"snapshot-source"}})
	if err != nil {
		t.Fatal(err)
	}
	return r, e, run
}

func snapshotTask(run benchmark.Run) benchmark.ReviewTask {
	return benchmark.ReviewTask{RunID: run.ID, Revision: run.Revision, Name: run.Name}
}
func snapshotReport(s ReviewSnapshot, card, id string) benchmark.ReviewReport {
	return benchmark.ReviewReport{ID: id, ExperimentID: card, SnapshotID: s.ID, EvidenceAt: &s.CreatedAt, Status: "completed", CreatedAt: time.Now(), Tasks: s.Tasks, Config: reviewTestConfig(), Output: &benchmark.ReviewOutput{Summary: "snapshot"}}
}

func TestFullSnapshotBeyondPrefixLimitWithConcurrentLogWriter(t *testing.T) {
	// SQLite is Go code and heavily instrumented under -race. Capacity is
	// exercised by the normal suite; -short keeps the same multi-batch writer
	// interaction small enough for the race suite's fixed operation deadlines.
	count := 200005
	if testing.Short() {
		count = 10005
	}
	t.Logf("snapshot rows=%d (short=%v)", count, testing.Short())
	r, e, run := seedSnapshotTask(t, count)
	full, err := r.MeasureFullExperimentRun(context.Background(), run.ID)
	if err != nil || full.Requests != count || full.Errors != 1 || full.Sampled || len(full.LogIDs) != 0 || full.TotalTokens == nil || *full.TotalTokens != int64(count)*110 {
		t.Fatalf("full statistics missed tail: %+v %v", full, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type result struct {
		s   ReviewSnapshot
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := r.PrepareReviewSnapshot(ctx, e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)})
		done <- result{s, err}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := r.db.QueryRow("SELECT COUNT(*) FROM review_snapshot_logs").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n >= 1000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("snapshot made no progress")
		}
		time.Sleep(time.Millisecond)
	}
	a := NewAsyncRepository(r, nil, 1024)
	defer a.Close()
	for i := range 20 {
		entry := newTestLog(fmt.Sprintf("after-freeze-%d", i), time.Now())
		entry.TraceID = "snapshot-source"
		entry.StatusCode = 200
		if err := a.SaveLog(entry); err != nil {
			t.Fatalf("concurrent log queue failed: %v", err)
		}
	}
	// The writer must progress while the large directory is still being built,
	// rather than merely draining its queue after one giant write transaction.
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, err := r.GetLog("after-freeze-19"); err == nil {
			break
		}
		select {
		case result := <-done:
			t.Fatalf("snapshot finished before queued logs could progress: %v", result.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("normal log writer did not progress during directory construction")
		}
		time.Sleep(time.Millisecond)
	}
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.s.Tasks[0].Metrics.Requests != count || len(got.s.Tasks[0].Metrics.LogIDs) != 0 {
		t.Fatalf("snapshot drifted: %+v", got.s.Tasks[0].Metrics)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if _, err := r.GetLog("after-freeze-19"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normal log writer stalled")
		}
		time.Sleep(time.Millisecond)
	}
	if n := a.dropped.Load(); n != 0 {
		t.Fatalf("snapshot pressure dropped %d log writes", n)
	}
	report := snapshotReport(got.s, e.ID, "large-report")
	if err := r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	var document string
	if err := r.db.QueryRow("SELECT document FROM review_reports WHERE id=?", report.ID).Scan(&document); err != nil {
		t.Fatal(err)
	}
	if len(document) > 10000 || strings.Contains(document, "snap-000000") {
		t.Fatalf("report retained the full member list: %d bytes", len(document))
	}
	rows, err := r.ReviewSnapshotRequests(ctx, got.s.ID, run.ID, 0, "tail-evidence", true, 20)
	if err != nil || len(rows) != 1 || rows[0].Position != count || rows[0].Log.ID != fmt.Sprintf("snap-%06d", count-1) {
		t.Fatalf("tail is not searchable: %+v %v", rows, err)
	}
	deep, err := r.ReviewSnapshotRequests(ctx, got.s.ID, run.ID, count-5, "", false, 20)
	if err != nil || len(deep) != 5 {
		t.Fatalf("deep keyset page: %+v %v", deep, err)
	}
	tail := fmt.Sprintf("snap-%06d", count-1)
	members, err := r.ReviewSnapshotMembers(ctx, got.s.ID, run.ID, []string{tail, "after-freeze-19"})
	if err != nil || !members[tail] || members["after-freeze-19"] {
		t.Fatalf("frozen scope drifted: %v %v", members, err)
	}
}

func TestSnapshotSharedFollowupsAndLastReferenceRetention(t *testing.T) {
	r, e, run := seedSnapshotTask(t, 5)
	second, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "overlapping", Conditions: benchmark.Conditions{Agent: "b"}, TraceIDs: []string{"snapshot-source"}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run), snapshotTask(second)})
	if err != nil {
		t.Fatal(err)
	}
	if overlap, err := r.ReviewSnapshotOverlap(context.Background(), s.ID); err != nil || !overlap {
		t.Fatalf("snapshot overlap lost: %v %v", overlap, err)
	}
	if overlap, err := r.ExperimentRunsOverlap(context.Background(), []string{run.ID, second.ID}); err != nil || !overlap {
		t.Fatalf("basic overlap lost: %v %v", overlap, err)
	}
	parent := snapshotReport(s, e.ID, "parent")
	if err := r.CreateReviewReport(parent); err != nil {
		t.Fatal(err)
	}
	child := parent
	child.ID = "child"
	child.ParentID = parent.ID
	if err := r.CreateReviewReport(child); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteReviewReport(parent.ID, e.ID); err != nil {
		t.Fatal(err)
	}
	if err := r.DiscardReviewSnapshot(s.ID); err != nil {
		t.Fatal(err)
	}
	members, err := r.ReviewSnapshotMembers(context.Background(), s.ID, run.ID, []string{"snap-000000"})
	if err != nil || !members["snap-000000"] {
		t.Fatalf("parent deletion removed child's directory: %v %v", members, err)
	}
	if err := r.DeleteReviewReport(child.ID, e.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, table := range []string{"review_snapshots", "review_snapshot_logs"} {
		if err := r.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("directory leaked in %s: %d %v", table, n, err)
		}
	}
	if err := r.db.QueryRow("SELECT COUNT(*) FROM review_card_logs WHERE experiment_id=?", e.ID).Scan(&n); err != nil || n != 5 {
		t.Fatalf("last deletion changed card retention: %d %v", n, err)
	}
	applyManagement(t, r, manageRequest("experiment", "delete", e.ID))
	if err := r.db.QueryRow("SELECT COUNT(*) FROM review_card_logs").Scan(&n); err != nil || n != 0 {
		t.Fatal("card protection did not clear with card")
	}
}

func TestSnapshotFailureAndSourceChangeLeaveNoPartialDirectory(t *testing.T) {
	for _, scenario := range []string{"insert-failure", "deleted-source", "changed-run"} {
		t.Run(scenario, func(t *testing.T) {
			r, e, run := seedSnapshotTask(t, 1005)
			body := `SELECT RAISE(ABORT,'synthetic insert failure');`
			position := 1001
			if scenario == "deleted-source" {
				body = `DELETE FROM request_logs WHERE id='snap-001004';`
				position = 1000
			}
			if scenario == "changed-run" {
				body = `UPDATE experiment_runs SET document=json_set(document,'$.revision',revision+1),revision=revision+1;`
				position = 1000
			}
			if _, err := r.db.Exec(fmt.Sprintf(`CREATE TRIGGER interrupt_snapshot AFTER INSERT ON review_snapshot_logs WHEN NEW.position=%d BEGIN %s END`, position, body)); err != nil {
				t.Fatal(err)
			}
			if _, err := r.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)}); err == nil {
				t.Fatal("partial directory was accepted")
			}
			for _, table := range []string{"review_snapshots", "review_snapshot_logs", "review_card_logs", "review_reports"} {
				var n int
				if err := r.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
					t.Fatalf("failure leaked %s: %d %v", table, n, err)
				}
			}
		})
	}
	t.Run("canceled", func(t *testing.T) {
		r, e, run := seedSnapshotTask(t, 5)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := r.PrepareReviewSnapshot(ctx, e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	})
}

func TestSnapshotRestartAndLegacyReportsRemainReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	e := testExperiment(t, r)
	entry := newTestLog("saved", time.Now().Add(-time.Hour))
	entry.TraceID = "saved"
	entry.StatusCode = 200
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "saved", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"saved"}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)})
	if err != nil {
		t.Fatal(err)
	}
	report := snapshotReport(s, e.ID, "running")
	report.Status = "running"
	if err := r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	legacy := benchmark.ReviewReport{ID: "legacy", ExperimentID: e.ID, Status: "completed", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: run.ID, Metrics: benchmark.Metrics{LogIDs: []string{entry.ID}}}}}
	if err := r.CreateReviewReport(legacy); err != nil {
		t.Fatal(err)
	}
	orphan, err := r.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.GetReviewReport(report.ID)
	if err != nil || got.Status != "interrupted" || got.SnapshotID != s.ID {
		t.Fatalf("restart report: %+v %v", got, err)
	}
	var n int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM review_snapshots WHERE id=?", orphan.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("unfinished directory survived restart")
	}
	got, err = r.GetReviewReport(legacy.ID)
	if err != nil || got.SnapshotID != "" || len(got.Tasks[0].Metrics.LogIDs) != 1 {
		t.Fatalf("legacy report changed: %+v %v", got, err)
	}
	if _, err := r.GetReviewReport("missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	var raw string
	if err := r.db.QueryRow("SELECT document FROM review_reports WHERE id=?", legacy.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var original benchmark.ReviewReport
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedSnapshotProtectsLogsAndBlocksDestructiveManagement(t *testing.T) {
	r, e, run := seedSnapshotTask(t, 5)
	e.Keep = false
	var err error
	e, err = r.SaveExperiment(e)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.PrepareReviewSnapshot(context.Background(), e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)})
	if err != nil {
		t.Fatal(err)
	}
	request := manageRequest("experiment", "delete", e.ID)
	_, preview := previewManagement(t, r, request)
	if preview.Items[0].BlockedReason != "task_active" {
		t.Fatalf("preparing card was not active: %+v", preview)
	}
	request = manageRequest("run", "delete", run.ID)
	request.ExperimentID = e.ID
	_, preview = previewManagement(t, r, request)
	if preview.Items[0].BlockedReason != "task_active" {
		t.Fatalf("preparing run was not active: %+v", preview)
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 0 {
		t.Fatalf("prepared evidence was pruned: %d %v", n, err)
	}
	if err := r.DiscardReviewSnapshot(s.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 5 {
		t.Fatalf("abandoned directory left retention protection: %d %v", n, err)
	}
}

type legacyReviewSnapshotRepository struct {
	Repository
	ExperimentRepository
	ReviewRepository
}

func TestLegacyAsyncRepositoryKeepsItsExplicitEvidenceList(t *testing.T) {
	r, _, run := seedSnapshotTask(t, 5)
	legacy := legacyReviewSnapshotRepository{Repository: r, ExperimentRepository: r, ReviewRepository: r}
	a := &AsyncRepository{inner: legacy}
	if a.SupportsReviewSnapshots() {
		t.Fatal("legacy integration incorrectly advertised snapshot support")
	}
	metrics, err := a.MeasureFullExperimentRun(context.Background(), run.ID)
	if err != nil || metrics.Requests != 5 || len(metrics.LogIDs) != 5 {
		t.Fatalf("legacy evidence compatibility was lost: %+v %v", metrics, err)
	}
}
