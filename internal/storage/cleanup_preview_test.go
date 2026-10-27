package storage

import (
	"context"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
)

func TestExpiredCleanupPreviewSharesWholeTraceProtection(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	now := time.Now()
	old := now.Add(-72 * time.Hour)
	add := func(id, traceID string, created time.Time, status int) *RequestLog {
		t.Helper()
		entry := newTestLog(id, created)
		entry.TraceID, entry.StatusCode = traceID, status
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
		return entry
	}
	add("free-one", "free", old, 200)
	add("free-two", "free", old, 200)
	add("standalone", "", old, 200)
	add("pinned", "pinned-trace", old, 200)
	if _, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "pinned-trace", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	add("saved", "saved-trace", old, 200)
	if _, err := r.SaveLogAnnotation("saved", LogAnnotation{Saved: true}); err != nil {
		t.Fatal(err)
	}
	add("mixed-old", "mixed", old, 200)
	add("mixed-new", "mixed", now, 200)
	run, err := r.StartCaptureRun("active capture", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	active := add("active", "active-trace", old, 200)
	active.CaptureRunID = run.ID
	if err := r.SaveLog(active); err != nil {
		t.Fatal(err)
	}
	add("retained", "review-trace", old, 200)
	experiment := testExperiment(t, r)
	if _, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: experiment.ID, Name: "review task", State: "completed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"review-trace"}}); err != nil {
		t.Fatal(err)
	}
	add("inflight", "inflight", now.Add(-2*time.Hour), 0)
	before := now.Add(-time.Hour)
	preview, err := r.PreviewExpiredCleanup(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ExpiredLogs != 9 || preview.EligibleLogs != 3 || preview.EligibleTraces != 1 || preview.RetainedLogs != 6 {
		t.Fatalf("preview failed to preserve complete/protected groups: %+v", preview)
	}
	deleted, err := r.DeleteLogsBefore(before)
	if err != nil || deleted != preview.EligibleLogs {
		t.Fatalf("delete/preview mismatch: %d %+v %v", deleted, preview, err)
	}
	for _, id := range []string{"pinned", "saved", "mixed-old", "active", "retained", "inflight"} {
		if _, err := r.GetLog(id); err != nil {
			t.Fatalf("protected %s disappeared: %v", id, err)
		}
	}
}

func TestAsyncExpiredCleanupPreviewDelegatesAndCancels(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	async := NewAsyncRepository(r, &config.Config{}, 8)
	defer async.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := async.PreviewExpiredCleanup(ctx, time.Now()); err == nil {
		t.Fatal("cancelled preview continued")
	}
	preview, err := async.PreviewExpiredCleanup(context.Background(), time.Now())
	if err != nil || preview.EligibleLogs != 0 || preview.RetainedLogs != 0 {
		t.Fatalf("empty preview: %+v %v", preview, err)
	}
}

func TestRunningReviewProtectsEvidenceWithoutLongTermKeep(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	old := time.Now().Add(-72 * time.Hour)
	for _, id := range []string{"evidence", "same-trace"} {
		entry := newTestLog(id, old)
		entry.TraceID, entry.StatusCode = "analysis-trace", 200
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	experiment, err := r.SaveExperiment(benchmark.Experiment{Name: "Temporary review", Keep: false})
	if err != nil {
		t.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: experiment.ID, Name: "Evidence task", State: "completed", Conditions: benchmark.Conditions{Agent: "agent"}, TraceIDs: []string{"analysis-trace"}})
	if err != nil {
		t.Fatal(err)
	}
	report := benchmark.ReviewReport{ID: "running-cleanup-review", ExperimentID: experiment.ID, Status: "running", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: run.ID, Metrics: benchmark.Metrics{LogIDs: []string{"evidence"}}}}}
	if err := r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-24 * time.Hour)
	preview, err := r.PreviewExpiredCleanup(context.Background(), before)
	if err != nil || preview.EligibleLogs != 0 || preview.RetainedLogs != 2 {
		t.Fatalf("running review evidence unprotected: %+v %v", preview, err)
	}
	if deleted, err := r.DeleteLogsBefore(before); err != nil || deleted != 0 {
		t.Fatalf("time cleanup deleted running review: %d %v", deleted, err)
	}
	if deleted, err := r.DeleteOldestLogs(1); err != nil || deleted != 0 {
		t.Fatalf("size cleanup deleted running review: %d %v", deleted, err)
	}
	report.Status = "completed"
	if err := r.FinishReviewReport(report); err != nil {
		t.Fatal(err)
	}
	preview, err = r.PreviewExpiredCleanup(context.Background(), before)
	if err != nil || preview.EligibleLogs != 2 || preview.RetainedLogs != 0 {
		t.Fatalf("completed review kept temporary protection: %+v %v", preview, err)
	}
	if deleted, err := r.DeleteLogsBefore(before); err != nil || deleted != 2 {
		t.Fatalf("completed keep=false evidence remains protected: %d %v", deleted, err)
	}
}
