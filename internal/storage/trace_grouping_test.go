package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

func groupingLog(t *testing.T, r *SQLiteRepository, id, group string, explicit bool) *RequestLog {
	t.Helper()
	entry := newTestLog(id, time.Now().Add(-time.Minute))
	entry.StatusCode = 200
	if explicit {
		entry.TraceID = group
		entry.RequestHeaders = map[string][]string{"X-PrismCat-Trace-ID": {group}}
	}
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if !explicit {
		a := trace.Assignment{TraceID: group, Source: "auto", Reason: "new_task", State: "inferred"}
		if _, err := r.storeObservation(trace.Observation{LogID: id, Scope: entry.Upstream, CreatedAt: entry.CreatedAt, Assignment: a}, false); err != nil {
			t.Fatal(err)
		}
	}
	return entry
}
func applyGrouping(t *testing.T, r *SQLiteRepository, v TraceGroupingRequest) TraceGroupingResult {
	t.Helper()
	p, err := r.PreviewTraceGrouping(context.Background(), v)
	if err != nil || !p.CanApply {
		t.Fatalf("preview: %+v %v", p, err)
	}
	v.Version = p.Version
	result, err := r.ApplyTraceGrouping(context.Background(), v)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestTraceGroupingMergePreservesSamplesFavoriteAliasesAndFutureCalls(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	ctx := context.Background()
	a := groupingLog(t, r, "a1", "a", false)
	groupingLog(t, r, "a2", "a", false)
	groupingLog(t, r, "b1", "b", false)
	if _, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "a", Name: "source", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "b", Name: "target", Labels: []string{"target"}}); err != nil {
		t.Fatal(err)
	}
	e := testExperiment(t, r)
	runA, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "Agent A", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	runB, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "Agent B", Conditions: benchmark.Conditions{Agent: "b"}, TraceIDs: []string{"b"}})
	if err != nil {
		t.Fatal(err)
	}
	result := applyGrouping(t, r, TraceGroupingRequest{Action: "merge", TraceIDs: []string{"a", "b"}, Target: "b"})
	if result.LogCount != 2 || result.Target != "b" {
		t.Fatalf("merge result: %+v", result)
	}
	for _, run := range []struct {
		id    string
		count int
	}{{runA.ID, 2}, {runB.ID, 1}} {
		logs, err := r.ExperimentLogs(ctx, run.id, 100)
		if err != nil || len(logs) != run.count {
			t.Fatalf("review sample changed: %s %d %v", run.id, len(logs), err)
		}
	}
	m, err := r.GetTraceMetadataDocument("b")
	if err != nil || !m.Pinned || m.Name != "target" || len(m.Labels) != 1 {
		t.Fatalf("metadata lost: %+v %v", m, err)
	}
	if id, err := r.ResolveTrace("a"); err != nil || id != "b" {
		t.Fatalf("old link: %q %v", id, err)
	}
	// Async completion can carry a stale source snapshot without reverting its grouping.
	a.ResponseBody = "completed"
	if err = r.SaveLog(a); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetLog(a.ID)
	if got.TraceID != "b" || got.ResponseBody != "completed" {
		t.Fatalf("stale writer: %+v", got)
	}
	future := groupingLog(t, r, "future", "a", false)
	got, _ = r.GetLog(future.ID)
	if got.TraceID != "b" {
		t.Fatal("late call recreated source trace")
	}
	logs, _ := r.ExperimentLogs(ctx, runB.ID, 100)
	if len(logs) != 1 {
		t.Fatal("late call contaminated frozen target sample")
	}
	runA.Name = "edited"
	if _, err = r.SaveExperimentRun(runA); err != nil {
		t.Fatal(err)
	}
	logs, _ = r.ExperimentLogs(ctx, runA.ID, 100)
	if len(logs) != 2 {
		t.Fatal("editing run discarded its snapshot")
	}
	// Flatten a second merge; target metadata and original links remain usable.
	groupingLog(t, r, "c1", "c", false)
	applyGrouping(t, r, TraceGroupingRequest{Action: "merge", TraceIDs: []string{"b"}, Target: "c"})
	if id, _ := r.ResolveTrace("a"); id != "c" {
		t.Fatal("chained merge lost old redirect")
	}
	preview, err := r.PreviewTaskManagement(ctx, TaskManagementRequest{Kind: "trace", Action: "delete", Items: []TaskManagementItem{{ID: "c"}}})
	if err != nil || preview.CanApply {
		t.Fatalf("sample no longer protects manual deletion: %+v %v", preview, err)
	}
	var eligible int
	if err = r.db.QueryRow("SELECT COUNT(*) FROM trace_retention_groups WHERE group_key='trace:c'").Scan(&eligible); err != nil || eligible != 0 {
		t.Fatalf("retention protection lost: %d %v", eligible, err)
	}
}
func TestTraceGroupingExplicitSplitAndStaleCorrelation(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	first := groupingLog(t, r, "explicit-1", "client", true)
	groupingLog(t, r, "explicit-2", "client", true)
	groupingLog(t, r, "explicit-3", "client", true)
	groupingLog(t, r, "automatic", "auto", false)
	for _, v := range []TraceGroupingRequest{{Action: "merge", TraceIDs: []string{"client"}, Target: "auto"}, {Action: "merge", TraceIDs: []string{"auto"}, Target: "client"}, {Action: "move", TraceIDs: []string{"client"}, LogIDs: []string{"explicit-1"}, Target: "auto"}} {
		p, err := r.PreviewTraceGrouping(context.Background(), v)
		if err != nil || p.CanApply || p.Reason != "explicit_split_only" {
			t.Fatalf("explicit merge/move accepted: %+v %v", p, err)
		}
	}
	result := applyGrouping(t, r, TraceGroupingRequest{Action: "split", TraceIDs: []string{"client"}, LogIDs: []string{"explicit-1", "explicit-2"}})
	for _, id := range []string{"explicit-1", "explicit-2"} {
		got, _ := r.GetLog(id)
		if got.TraceID != result.Target || !got.TraceAssociation.Locked || got.RequestHeaders["X-PrismCat-Trace-ID"][0] != "client" {
			t.Fatalf("split lost client evidence: %+v", got)
		}
	}
	untouched, _ := r.GetLog("explicit-3")
	if untouched.TraceID != "client" {
		t.Fatal("split moved unselected request")
	}
	if err := r.SaveLog(first); err != nil {
		t.Fatal(err)
	}
	assignment, err := r.storeObservation(trace.Observation{LogID: first.ID, Assignment: trace.Assignment{TraceID: "client", Source: "explicit", Reason: "explicit_header"}}, false)
	if err != nil || assignment.TraceID != result.Target {
		t.Fatalf("correlator reverted split: %+v %v", assignment, err)
	}
	var original string
	if err = r.db.QueryRow("SELECT explicit_trace_id FROM request_logs WHERE id=?", first.ID).Scan(&original); err != nil || original != "client" {
		t.Fatal("original trace id changed")
	}
}
func TestTraceGroupingPreviewVersionAtomicityActiveAndSelection(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	ctx := context.Background()
	groupingLog(t, r, "a", "a", false)
	groupingLog(t, r, "b", "b", false)
	v := TraceGroupingRequest{Action: "merge", TraceIDs: []string{"a"}, Target: "b"}
	p, err := r.PreviewTraceGrouping(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	v.Version = p.Version
	groupingLog(t, r, "late", "a", false)
	if _, err = r.ApplyTraceGrouping(ctx, v); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatal("stale preview accepted")
	}
	got, _ := r.GetLog("a")
	if got.TraceID != "a" {
		t.Fatal("failed batch partially changed data")
	}
	p, err = r.PreviewTraceGrouping(ctx, TraceGroupingRequest{Action: "split", TraceIDs: []string{"a"}, LogIDs: []string{"a", "b"}})
	if err != nil || p.CanApply {
		t.Fatal("cross-trace selection accepted")
	}
	capture, err := r.StartCaptureRun("recording", "active", false)
	if err != nil {
		t.Fatal(err)
	}
	entry := groupingLog(t, r, "active", "active", false)
	entry.CaptureRunID = capture.ID
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	p, err = r.PreviewTraceGrouping(ctx, TraceGroupingRequest{Action: "split", TraceIDs: []string{"active"}, LogIDs: []string{entry.ID}})
	if err != nil || p.CanApply || p.Reason != "active" {
		t.Fatalf("active capture allowed: %+v %v", p, err)
	}
}
func TestTraceGroupingMovePreservesInternalEdgesAndRemovesCrossGroupEdges(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	for _, id := range []string{"p", "q", "r"} {
		groupingLog(t, r, id, "a", false)
	}
	groupingLog(t, r, "target", "b", false)
	for id, parent := range map[string]string{"q": "p", "r": "q"} {
		var info string
		r.db.QueryRow("SELECT info FROM trace_assignments WHERE log_id=?", id).Scan(&info)
		var a trace.Assignment
		json.Unmarshal([]byte(info), &a)
		a.Parents = []string{parent}
		raw, _ := json.Marshal(a)
		r.db.Exec("UPDATE trace_assignments SET info=?,parent_log_id=? WHERE log_id=?", string(raw), parent, id)
		r.db.Exec("UPDATE request_logs SET trace_info=?,parent_log_id=? WHERE id=?", string(raw), parent, id)
	}
	applyGrouping(t, r, TraceGroupingRequest{Action: "move", TraceIDs: []string{"a"}, LogIDs: []string{"p", "q"}, Target: "b"})
	q, _ := r.GetLog("q")
	last, _ := r.GetLog("r")
	if len(q.TraceAssociation.Parents) != 1 || q.TraceAssociation.Parents[0] != "p" || len(last.TraceAssociation.Parents) != 0 {
		t.Fatalf("invalid edges: %+v %+v", q.TraceAssociation, last.TraceAssociation)
	}
}

func TestTraceGroupingRollbackAndTargetSearchBeforePagination(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	ctx := context.Background()
	groupingLog(t, r, "first", "source", false)
	groupingLog(t, r, "second", "source", false)
	groupingLog(t, r, "target", "target", false)
	for i := 0; i < 12; i++ {
		groupingLog(t, r, fmt.Sprintf("explicit-%d", i), fmt.Sprintf("client-%d", i), true)
	}
	choices, total, err := r.ListTraces(TraceFilter{Mergeable: true, Limit: 1})
	if err != nil || len(choices) != 1 || total != 2 {
		t.Fatalf("picker filtered after pagination: %d %d %v", len(choices), total, err)
	}
	_, err = r.db.Exec(`CREATE TRIGGER fail_grouping_test BEFORE UPDATE OF trace_id ON request_logs WHEN NEW.id='second' AND NEW.trace_id='target' BEGIN SELECT RAISE(ABORT,'simulated write failure'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	v := TraceGroupingRequest{Action: "merge", TraceIDs: []string{"source"}, Target: "target"}
	p, err := r.PreviewTraceGrouping(ctx, v)
	if err != nil || !p.CanApply {
		t.Fatal(err)
	}
	v.Version = p.Version
	if _, err = r.ApplyTraceGrouping(ctx, v); err == nil {
		t.Fatal("expected write failure")
	}
	for _, id := range []string{"first", "second"} {
		log, _ := r.GetLog(id)
		if log.TraceID != "source" {
			t.Fatal("failed transaction moved a request")
		}
	}
	var count int
	if err = r.db.QueryRow("SELECT COUNT(*) FROM trace_group_operations").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed transaction left an operation")
	}
	if resolved, _ := r.ResolveTrace("source"); resolved != "source" {
		t.Fatal("failed transaction left a redirect")
	}
}

func TestTraceGroupingBlocksRunningReviewAndDetectsNewReference(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	ctx := context.Background()
	groupingLog(t, r, "a", "a", false)
	groupingLog(t, r, "b", "b", false)
	v := TraceGroupingRequest{Action: "merge", TraceIDs: []string{"a"}, Target: "b"}
	p, err := r.PreviewTraceGrouping(ctx, v)
	if err != nil || !p.CanApply {
		t.Fatal(err)
	}
	v.Version = p.Version
	e := testExperiment(t, r)
	_, err = r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "A", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyTraceGrouping(ctx, v); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatal("new review reference did not invalidate preview")
	}
	_, err = r.db.Exec("INSERT INTO review_reports VALUES(?,?,?,?,?,?,?,?)", "running", e.ID, "running", "model", "", "", time.Now().UnixMilli(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	p, err = r.PreviewTraceGrouping(ctx, v)
	if err != nil || p.CanApply || p.Reason != "active" {
		t.Fatalf("running review allowed: %+v %v", p, err)
	}
}
