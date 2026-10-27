package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

func testExperiment(t *testing.T, r *SQLiteRepository) benchmark.Experiment {
	t.Helper()
	e, err := r.SaveExperiment(benchmark.Experiment{Name: "Fix login", Keep: true, Task: benchmark.Task{Prompt: "Fix login", BaseCommit: "base", Environment: "Go", TestCommand: "go test", Rubric: []benchmark.Criterion{{ID: "correct", Name: "Correct", Weight: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestExperimentSnapshotsRevisionsCaptureAndEvidenceUnion(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "Agent A", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	e.Task.Prompt = "Different task"
	if _, err = r.SaveExperiment(e); err != nil {
		t.Fatal(err)
	}
	run, err = r.GetExperimentRun(run.ID)
	if err != nil || run.Task.Prompt != "Fix login" {
		t.Fatalf("snapshot mutated: %+v %v", run, err)
	}
	if _, err = r.StartExperimentCapture(run.ID, 0, "agent"); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatal("stale start accepted")
	}
	run, err = r.StartExperimentCapture(run.ID, run.Revision, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if r.CaptureRunForUpstream("agent") != run.CaptureRunID {
		t.Fatal("capture registry not updated")
	}
	entry := newTestLog("sample", time.Now())
	entry.TraceID = "task"
	entry.CaptureRunID = run.CaptureRunID
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	run.TraceIDs = []string{"task"}
	run, err = r.SaveExperimentRun(run)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := r.ExperimentLogs(context.Background(), run.ID, 10)
	if err != nil || len(logs) != 1 {
		t.Fatalf("capture + trace duplicated logs: %d %v", len(logs), err)
	}
	old := run
	run, err = r.StopExperimentCapture(run.ID, run.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if run.State != "completed" || run.FinishedAt == nil || r.CaptureRunForUpstream("agent") != "" {
		t.Fatal("stop not atomic")
	}
	if _, err = r.SaveExperimentRun(old); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatal("stale edit accepted")
	}
	loaded, _ := r.GetExperiment(e.ID)
	if loaded.RunCount != 1 {
		t.Fatal("run count not maintained")
	}
}

func TestTraceRetentionIsWholeTaskAndProtectsExperiments(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	old := time.Now().Add(-72 * time.Hour)
	for _, id := range []string{"one", "two"} {
		entry := newTestLog(id, old)
		entry.TraceID = "whole"
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	m, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "whole", Name: "Task name", Pinned: true})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 0 {
		t.Fatalf("pinned trace was pruned: %d %v", n, err)
	}
	m.Pinned = false
	if _, err = r.SaveTraceMetadata(m); err != nil {
		t.Fatal(err)
	}
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "imported", State: "completed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"whole"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteOldestLogs(1); err != nil || n != 0 {
		t.Fatalf("experiment evidence pruned: %d %v", n, err)
	}
	e.Keep = false
	if _, err = r.SaveExperiment(e); err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteOldestLogs(1); err != nil || n != 2 {
		t.Fatalf("trace was split to meet row budget: %d %v", n, err)
	}
	logs, err := r.ExperimentLogs(context.Background(), run.ID, 10)
	if err != nil || len(logs) != 0 {
		t.Fatal("deleted evidence appeared available")
	}
	run.Assessment.Notes = "Evidence expired under retention policy"
	if _, err = r.SaveExperimentRun(run); err != nil {
		t.Fatalf("expired logs prevented assessment edits: %v", err)
	}
	run, _ = r.GetExperimentRun(run.ID)
	run.TraceIDs = append(run.TraceIDs, "missing-new-trace")
	if _, err = r.SaveExperimentRun(run); !errors.Is(err, benchmark.ErrInvalid) {
		t.Fatalf("nonexistent new trace was accepted: %v", err)
	}
}

func TestImportedCaptureUsesRecordedStateAndTimes(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	capture, err := r.StartCaptureRun("Existing task", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	run := benchmark.Run{ExperimentID: e.ID, Name: "Imported", Conditions: benchmark.Conditions{Agent: "a"}, State: "planned", CaptureRunID: capture.ID}
	if _, err = r.SaveExperimentRun(run); !errors.Is(err, benchmark.ErrInvalid) {
		t.Fatalf("active capture accepted as planned: %v", err)
	}
	if err = r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	saved, err := r.SaveExperimentRun(run)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != "completed" || saved.StartedAt == nil || saved.FinishedAt == nil || saved.StartedAt.UnixMilli() != capture.StartedAt.UnixMilli() || saved.FinishedAt.Before(*saved.StartedAt) || saved.Assessment.Verdict != "unknown" {
		t.Fatalf("import did not preserve capture evidence: %+v", saved)
	}
	if _, err = r.SaveExperimentRun(run); !errors.Is(err, benchmark.ErrInvalid) {
		t.Fatalf("same capture accepted into a second run: %v", err)
	}
}

func TestExperimentRunCollectionsRemainEditableAfterReload(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "API run", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if run.TraceIDs == nil || run.Assessment.Scores == nil {
		t.Fatal("new API run contains null collections")
	}
	// Older documents and external scripts may have stored explicit JSON null.
	if _, err = r.db.Exec(`UPDATE experiment_runs SET document=json_set(document,'$.trace_ids',NULL,'$.assessment.scores',NULL) WHERE id=?`, run.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err := r.GetExperimentRun(run.ID)
	if err != nil || loaded.TraceIDs == nil || loaded.Assessment.Scores == nil {
		t.Fatalf("legacy run cannot be edited: %+v %v", loaded, err)
	}
	items, _, err := r.ListExperimentRuns(e.ID, 0, 20)
	if err != nil || len(items) != 1 || items[0].TraceIDs == nil || items[0].Assessment.Scores == nil {
		t.Fatalf("list returned null collections: %+v %v", items, err)
	}
}

func TestRecentOrSavedChildProtectsEntireTraceAndCatalogRefreshes(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	old := time.Now().Add(-72 * time.Hour)
	first := newTestLog("first", old)
	first.TraceID = "t"
	second := newTestLog("second", time.Now())
	second.TraceID = "t"
	for _, entry := range []*RequestLog{first, second} {
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	if n, err := r.DeleteLogsBefore(cutoff); err != nil || n != 0 {
		t.Fatal("old prefix of active trace deleted")
	}
	m, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "t", Name: "Login timeout", Labels: []string{"regression"}, Status: "todo", Archived: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveTraceMetadata(TraceMetadata{TraceID: "t"}); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatal("stale metadata edit accepted")
	}
	items, total, err := r.ListTraces(TraceFilter{Query: "Login", StartTime: &cutoff, Label: "regression"})
	if err != nil || total != 1 || len(items) != 1 || items[0].RequestCount != 2 || items[0].Metadata.Revision != m.Revision {
		t.Fatalf("catalog returned partial aggregates: %+v %d %v", items, total, err)
	}
	second.StatusCode = 500
	if err = r.SaveLog(second); err != nil {
		t.Fatal(err)
	}
	items, _, err = r.ListTraces(TraceFilter{})
	if err != nil || items[0].ErrorCount != 1 {
		t.Fatal("dirty summary was not refreshed")
	}
	if _, err = r.SaveLogAnnotation(first.ID, LogAnnotation{Saved: true}); err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteLogsBefore(time.Now().Add(time.Hour)); err != nil || n != 0 {
		t.Fatal("saved child did not protect whole trace")
	}
}
