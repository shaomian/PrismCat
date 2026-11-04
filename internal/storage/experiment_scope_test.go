package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

func seedExperimentScopeLogs(t testing.TB, r *SQLiteRepository, count int, prefix, traceID string) {
	t.Helper()
	tx, err := r.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO request_logs(id,created_at,created_at_unix_ms,upstream,target_url,method,path,status_code,latency_ms,error,trace_id,usage_input_tokens,usage_output_tokens,usage_total_tokens) VALUES(?,?,?,'scope','','POST','/v1/responses',200,5,'',?,100,10,110)`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	at := time.Now().Add(-time.Hour)
	for i := range count {
		created := at.Add(time.Duration(i) * time.Millisecond)
		if _, err := stmt.Exec(fmt.Sprintf("%s-%06d", prefix, i), created, created.UnixMilli(), traceID); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestIndexedExperimentScopePreservesUnionAndFrozenMembership(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	capture, err := r.StartCaptureRun("scope", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-time.Hour)
	for _, row := range []struct{ id, trace, capture string }{
		{"both", "live", capture.ID},
		{"capture-only", "", capture.ID},
		{"trace-only", "live", ""},
		{"frozen-member", "moved-away", ""},
		{"frozen-new", "frozen", ""},
		{"foreign", "unrelated", ""},
	} {
		if _, err := r.db.Exec(`INSERT INTO request_logs(id,created_at,created_at_unix_ms,upstream,target_url,method,path,status_code,error,trace_id,capture_run_id,response_body) VALUES(?,?,?,'scope','','POST','/v1/responses',200,'',?,?,'needle')`, row.id, at, at.UnixMilli(), row.trace, row.capture); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.db.Exec(`UPDATE request_logs SET status_code=503 WHERE id='frozen-member'`); err != nil {
		t.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "union", Conditions: benchmark.Conditions{Agent: "a"}, CaptureRunID: capture.ID, TraceIDs: []string{"live", "frozen"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`INSERT INTO experiment_trace_snapshots VALUES(?,'frozen')`, run.ID); err != nil {
		t.Fatal(err)
	}
	// Multiple frozen references and the capture/trace overlap must each count once.
	for _, source := range []string{"frozen", "previous-source"} {
		if _, err := r.db.Exec(`INSERT INTO experiment_trace_samples VALUES(?,?,'frozen-member')`, run.ID, source); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	want := []string{"both", "capture-only", "frozen-member", "trace-only"}
	assertIDs := func(logs []*RequestLog, err error) {
		t.Helper()
		ids := []string{}
		for _, row := range logs {
			ids = append(ids, row.ID)
		}
		if err != nil || !reflect.DeepEqual(ids, want) {
			t.Fatalf("scope changed: got %v want %v (%v)", ids, want, err)
		}
	}
	assertIDs(r.ExperimentLogs(ctx, run.ID, 100))
	assertIDs(r.SearchExperimentLogs(ctx, run.ID, "needle", false, 100, 0))
	metrics, err := r.MeasureFullExperimentRun(ctx, run.ID)
	if err != nil || metrics.Requests != 4 || metrics.Errors != 1 {
		t.Fatalf("union statistics: %+v %v", metrics, err)
	}
	filtered, err := r.SearchExperimentLogs(ctx, run.ID, "needle", true, 100, 0)
	if err != nil || len(filtered) != 1 || filtered[0].ID != "frozen-member" {
		t.Fatalf("frozen error search: %+v %v", filtered, err)
	}
	snapshot, err := r.PrepareReviewSnapshot(ctx, e.ID, e.Revision, []benchmark.ReviewTask{snapshotTask(run)})
	if err != nil {
		t.Fatal(err)
	}
	defer r.DiscardReviewSnapshot(snapshot.ID)
	page, err := r.ReviewSnapshotRequests(ctx, snapshot.ID, run.ID, 0, "", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	logs := make([]*RequestLog, 0, len(page))
	for _, row := range page {
		logs = append(logs, row.Log)
	}
	assertIDs(logs, nil)
	for _, trace := range []string{"live", "frozen"} {
		other, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: trace, Conditions: benchmark.Conditions{Agent: "b"}, TraceIDs: []string{trace}})
		if err != nil {
			t.Fatal(err)
		}
		overlap, err := r.ExperimentRunsOverlap(ctx, []string{run.ID, other.ID})
		if err != nil || overlap != (trace == "live") {
			t.Fatalf("%s overlap=%v err=%v", trace, overlap, err)
		}
	}
}

func TestExperimentScopeUsesIndexedCandidates(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	seedExperimentScopeLogs(t, r, 10, "selected", "selected")
	seedExperimentScopeLogs(t, r, 500, "unrelated", "unrelated")
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "indexed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"selected"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []string{"", " AND method='POST'"} {
		rows, err := r.db.Query(`EXPLAIN QUERY PLAN SELECT l.id FROM request_logs l WHERE `+experimentLogScope+` AND COALESCE(tag,'') NOT LIKE 'prismcat-analysis%'`+filter+` ORDER BY created_at_unix_ms,id`, run.ID, run.ID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		plan := ""
		boundLogLookup := false
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan += detail + "\n"
			boundLogLookup = boundLogLookup || strings.HasPrefix(detail, "SEARCH l ") && strings.Contains(detail, "rowid=?")
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		// Check access paths rather than wall-clock deadlines, which are noisy under -race.
		// The POST filter used by search/overlap must not select a whole-method scan.
		for _, table := range []string{"l", "cl", "tl", "fl"} {
			if strings.Contains(plan, "SCAN "+table+" ") || !strings.Contains(plan, "SEARCH "+table+" ") {
				t.Fatalf("log scope scans unrelated history (%s):\n%s", table, plan)
			}
		}
		if !boundLogLookup || !strings.Contains(plan, "idx_experiment_sample_run_log") {
			t.Fatalf("scope lacks indexed candidate lookups:\n%s", plan)
		}
	}
}

func TestExistingDatabaseAddsExperimentMemberIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "upgrade.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	seedExperimentScopeLogs(t, r, 5, "member", "source")
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "upgrade", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"source"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`INSERT INTO experiment_trace_snapshots VALUES(?,'source')`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`INSERT INTO experiment_trace_samples SELECT ?,trace_id,id FROM request_logs`, run.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := r.db.Exec(`DROP INDEX idx_experiment_sample_run_log`); err != nil {
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
	var indexCount int
	if err := r.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_experiment_sample_run_log'`).Scan(&indexCount); err != nil || indexCount != 1 {
		t.Fatalf("index not added on upgrade: %d %v", indexCount, err)
	}
	metrics, err := r.MeasureFullExperimentRun(context.Background(), run.ID)
	if err != nil || metrics.Requests != 5 || metrics.TotalTokens == nil || *metrics.TotalTokens != 550 {
		t.Fatalf("upgrade changed frozen data: %+v %v", metrics, err)
	}
}
