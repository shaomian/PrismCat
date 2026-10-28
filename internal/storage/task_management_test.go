package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
)

func manageRequest(kind, action string, ids ...string) TaskManagementRequest {
	request := TaskManagementRequest{Kind: kind, Action: action}
	for _, id := range ids {
		request.Items = append(request.Items, TaskManagementItem{ID: id})
	}
	return request
}
func previewManagement(t *testing.T, repo TaskManagementRepository, request TaskManagementRequest) (TaskManagementRequest, TaskManagementPreview) {
	t.Helper()
	preview, err := repo.PreviewTaskManagement(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range preview.Items {
		revision := item.Revision
		request.Items[i].Revision = &revision
	}
	return request, preview
}
func applyManagement(t *testing.T, repo TaskManagementRepository, request TaskManagementRequest) TaskManagementResult {
	t.Helper()
	request, preview := previewManagement(t, repo, request)
	if !preview.CanApply {
		t.Fatalf("blocked preview: %+v", preview)
	}
	result, err := repo.ApplyTaskManagement(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func managementLog(t *testing.T, r *SQLiteRepository, id, traceID, capture string) *RequestLog {
	t.Helper()
	entry := newTestLog(id, time.Now().Add(-time.Hour))
	entry.TraceID = traceID
	entry.CaptureRunID = capture
	entry.StatusCode = 200
	if err := r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestTraceManagementPreservesMetadataAndRejectsChangedDeleteScope(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	entry := managementLog(t, r, "one", "trace-a", "")
	_, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "trace-a", Name: "Important task", Notes: "keep notes", Labels: []string{"coding"}, Status: "todo", Pinned: true, TopAt: 1})
	if err != nil {
		t.Fatal(err)
	}
	applyManagement(t, r, manageRequest("trace", "archive", "trace-a"))
	metadata, err := r.GetTraceMetadataDocument("trace-a")
	if err != nil || !metadata.Archived || metadata.TopAt != 0 || metadata.Notes != "keep notes" || !metadata.Pinned || metadata.Status != "todo" {
		t.Fatalf("archive overwrote metadata: %+v %v", metadata, err)
	}
	applyManagement(t, r, manageRequest("trace", "restore", "trace-a"))
	metadata, err = r.GetTraceMetadataDocument("trace-a")
	if err != nil || metadata.Archived || metadata.TopAt != 0 || !metadata.Pinned {
		t.Fatalf("restore changed favorite or reinstated top position: %+v %v", metadata, err)
	}
	before, preview := previewManagement(t, r, manageRequest("trace", "delete", "trace-a"))
	if !preview.CanApply || preview.LogCount != 1 {
		t.Fatal(preview)
	}
	entry.ResponseBody = "later update"
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	_, err = r.ApplyTaskManagement(context.Background(), before)
	var blocked *TaskManagementBlockedError
	if !errors.As(err, &blocked) || blocked.Preview.Items[0].BlockedReason != "task_changed" {
		t.Fatalf("stale deletion accepted: %v", err)
	}
	before, _ = previewManagement(t, r, manageRequest("trace", "delete", "trace-a"))
	managementLog(t, r, "two", "trace-a", "")
	if _, err = r.ApplyTaskManagement(context.Background(), before); !errors.As(err, &blocked) {
		t.Fatal("changed membership was deleted")
	}
	invalid := manageRequest("trace", "delete", "trace-a", "missing")
	if _, err = r.ApplyTaskManagement(context.Background(), invalid); !errors.As(err, &blocked) {
		t.Fatal("partial batch accepted")
	}
	if _, err = r.GetLog("one"); err != nil {
		t.Fatal("atomic batch deleted first item")
	}
}

func TestTraceDeletionPreventsQueuedSnapshotResurrectionButAllowsReusedTraceID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := managementLog(t, r, "old-log", "reused", "")
	if _, err = r.SaveLogAnnotation(entry.ID, LogAnnotation{Saved: true, Note: "saved"}); err != nil {
		t.Fatal(err)
	}
	result := applyManagement(t, r, manageRequest("trace", "delete", "reused"))
	if result.DeletedLogs != 1 {
		t.Fatal(result)
	}
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if _, err = r.GetLog(entry.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("stale snapshot resurrected deleted log")
	}
	var annotations int
	if err = r.db.QueryRow("SELECT COUNT(*) FROM log_annotations WHERE log_id=?", entry.ID).Scan(&annotations); err != nil || annotations != 0 {
		t.Fatal("orphan annotation remained")
	}
	r.Close()
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	if _, err = r.GetLog(entry.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("restart lost deletion guard")
	}
	managementLog(t, r, "new-log", "reused", "")
	traces, total, err := r.ListTraces(TraceFilter{TraceID: "reused"})
	if err != nil || total != 1 || len(traces) != 1 || traces[0].RequestCount != 1 {
		t.Fatalf("new legitimate trace call blocked: %+v %d %v", traces, total, err)
	}
}

func TestTaskReferencesIncludeArchivedCardsReportsAndCrossCaptureImports(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	capture, err := r.StartCaptureRun("capture", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	entry := managementLog(t, r, "source", "trace-source", capture.ID)
	card := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: card.ID, Name: "Imported", State: "completed", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{entry.TraceID}})
	if err != nil {
		t.Fatal(err)
	}
	applyManagement(t, r, manageRequest("experiment", "archive", card.ID))
	for _, request := range []TaskManagementRequest{manageRequest("trace", "delete", entry.TraceID), manageRequest("capture", "delete", capture.ID)} {
		_, preview := previewManagement(t, r, request)
		if preview.CanApply || preview.Items[0].BlockedReason != "task_referenced" || len(preview.Items[0].ReferencedBy) != 1 || preview.Items[0].ReferencedBy[0].ID != card.ID {
			t.Fatalf("archived card reference missed: %+v", preview)
		}
	}
	report := benchmark.ReviewReport{ID: "saved-report", ExperimentID: card.ID, Status: "running", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: run.ID, Name: "Original name", Metrics: benchmark.Metrics{LogIDs: []string{entry.ID}, TraceIDs: []string{entry.TraceID}}}}}
	if err = r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	report.Status = "completed"
	report.Output = &benchmark.ReviewOutput{Summary: "Frozen report"}
	if err = r.FinishReviewReport(report); err != nil {
		t.Fatal(err)
	}
	request := manageRequest("run", "delete", run.ID)
	request.ExperimentID = card.ID
	applyManagement(t, r, request)
	snapshot, err := r.GetReviewReport(report.ID)
	if err != nil || snapshot.Tasks[0].Name != "Original name" || snapshot.Output.Summary != "Frozen report" {
		t.Fatal("run removal changed saved report")
	}
	_, preview := previewManagement(t, r, manageRequest("trace", "delete", entry.TraceID))
	if preview.CanApply {
		t.Fatal("removed run discarded report evidence reference")
	}
	applyManagement(t, r, manageRequest("experiment", "delete", card.ID))
	if _, err = r.GetReviewReport(report.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("card reports were orphaned")
	}
	if _, err = r.GetLog(entry.ID); err != nil {
		t.Fatal("card deletion cascaded to source logs")
	}
	_, preview = previewManagement(t, r, manageRequest("trace", "delete", entry.TraceID))
	if !preview.CanApply {
		t.Fatalf("deleted card still protects source: %+v", preview)
	}
}

func TestCaptureArchiveRestoreFiltersAndDeletionKeepLogs(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	capture, err := r.StartCaptureRun("capture", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"archive", "delete"} {
		_, preview := previewManagement(t, r, manageRequest("capture", action, capture.ID))
		if preview.CanApply || preview.Items[0].BlockedReason != "task_active" {
			t.Fatalf("running capture %s allowed", action)
		}
	}
	entry := managementLog(t, r, "captured", "inside", capture.ID)
	if err = r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	applyManagement(t, r, manageRequest("capture", "archive", capture.ID))
	ordinary, total, err := r.ListCaptureRunsPageArchived(20, 0, false)
	if err != nil || total != 0 || len(ordinary) != 0 {
		t.Fatal("archive remained in normal list")
	}
	archived, total, err := r.ListCaptureRunsPageArchived(20, 0, true)
	if err != nil || total != 1 || !archived[0].Archived || archived[0].Revision == 0 {
		t.Fatal("archive missing from archived list")
	}
	applyManagement(t, r, manageRequest("capture", "restore", capture.ID))
	applyManagement(t, r, manageRequest("capture", "delete", capture.ID))
	if _, err = r.GetLog(entry.ID); err != nil {
		t.Fatal("capture deletion deleted logs")
	}
	rows, total, err := r.ListCaptureRunsPage(20, 0)
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatal("capture deletion left record")
	}
}

func TestTemplateManagementKeepsCardSnapshotsAndSupportsRecovery(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	template, err := r.SaveReviewTemplate(benchmark.ReviewTemplate{Name: "My template", Config: reviewTestConfig()})
	if err != nil {
		t.Fatal(err)
	}
	card := testExperiment(t, r)
	card.Review = &template.Config
	if _, err = r.SaveExperiment(card); err != nil {
		t.Fatal(err)
	}
	applyManagement(t, r, manageRequest("template", "archive", template.ID))
	templates, total, err := r.ListReviewTemplatesArchived("", true, 0, 20)
	if err != nil || total != 1 || !templates[0].Archived {
		t.Fatalf("archived templates inaccessible: %+v %v", templates, err)
	}
	applyManagement(t, r, manageRequest("template", "restore", template.ID))
	applyManagement(t, r, manageRequest("template", "delete", template.ID))
	saved, err := r.GetExperiment(card.ID)
	if err != nil || saved.Review.TemplateName != "My template" || saved.Review.Points[0].Name != "Key logic" {
		t.Fatal("template deletion changed a snapshot")
	}
	_, preview := previewManagement(t, r, manageRequest("template", "delete", "builtin:pentest"))
	if preview.CanApply || preview.Items[0].BlockedReason != "task_builtin" {
		t.Fatal("built-in template deletion allowed")
	}
}

func TestRunningReportsBlockCardAndRunDeletionAndLateReportsCannotRecreateCard(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	card := testExperiment(t, r)
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: card.ID, Name: "a", Conditions: benchmark.Conditions{Agent: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	report := benchmark.ReviewReport{ID: "working", ExperimentID: card.ID, Status: "running", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: run.ID}}}
	if err = r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	request := manageRequest("run", "delete", run.ID)
	request.ExperimentID = card.ID
	for _, value := range []TaskManagementRequest{manageRequest("experiment", "delete", card.ID), request} {
		_, preview := previewManagement(t, r, value)
		if preview.CanApply || preview.Items[0].BlockedReason != "task_active" {
			t.Fatal("working analysis could be removed")
		}
	}
	report.Status = "completed"
	if err = r.FinishReviewReport(report); err != nil {
		t.Fatal(err)
	}
	applyManagement(t, r, manageRequest("experiment", "delete", card.ID))
	report.ID = "late"
	report.Status = "running"
	if err = r.CreateReviewReport(report); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("late report recreated orphan card: %v", err)
	}
}

func TestAsyncDeletedSnapshotsDoNotRestoreTraceFeatures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "async.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := managementLog(t, r, "async-deleted", "async-trace", "")
	cfg := &config.Config{Correlation: config.CorrelationConfig{Enabled: true}}
	async := NewAsyncRepository(r, cfg, 8)
	defer async.Close()
	applyManagement(t, async, manageRequest("trace", "delete", entry.TraceID))
	entry.Path = "/v1/responses"
	entry.RequestHeaders = map[string][]string{"Content-Type": {"application/json"}}
	entry.ResponseHeaders = map[string][]string{"Content-Type": {"application/json"}}
	entry.RequestBodyRaw = []byte(`{"input":"old instruction"}`)
	entry.ResponseBodyRaw = []byte(`{"id":"deleted-response","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"old response"}]}]}`)
	entry.CorrelationFinal = true
	if err := async.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	child := newTestLog("new-followup", time.Now())
	child.Path = "/v1/responses"
	child.StatusCode = 200
	child.Upstream = entry.Upstream
	child.RequestHeaders = entry.RequestHeaders
	child.ResponseHeaders = entry.ResponseHeaders
	child.CorrelationFinal = true
	child.RequestBodyRaw = []byte(`{"previous_response_id":"deleted-response","input":"follow-up"}`)
	child.ResponseBodyRaw = []byte(`{"id":"child-response","output":[]}`)
	if err := async.SaveLog(child); err != nil {
		t.Fatal(err)
	}
	// Closing drains both bounded queues, including the observation queued after deletion.
	if err := async.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.GetLog(entry.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("queued snapshot restored deleted log")
	}
	successor, err := reopened.GetLog(child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if successor.TraceID == "" || successor.TraceID == entry.TraceID {
		t.Fatalf("deleted observation influenced later grouping: %+v", successor.TraceAssociation)
	}
	var features int
	if err = reopened.db.QueryRow("SELECT COUNT(*) FROM trace_assignments WHERE log_id=?", entry.ID).Scan(&features); err != nil || features != 0 {
		t.Fatal("queued observation restored deleted features")
	}
}

func TestCardDeleteVersionTracksNewRunsAndReportHistory(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	card := testExperiment(t, r)
	before, preview := previewManagement(t, r, manageRequest("experiment", "delete", card.ID))
	if !preview.CanApply {
		t.Fatal(preview)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: card.ID, Name: "new run", Conditions: benchmark.Conditions{Agent: "agent"}})
	if err != nil {
		t.Fatal(err)
	}
	var blocked *TaskManagementBlockedError
	if _, err = r.ApplyTaskManagement(context.Background(), before); !errors.As(err, &blocked) || blocked.Preview.Items[0].BlockedReason != "task_changed" {
		t.Fatalf("new run was deleted outside confirmed scope: %v", err)
	}
	before, _ = previewManagement(t, r, manageRequest("experiment", "delete", card.ID))
	report := benchmark.ReviewReport{ID: "later-report", ExperimentID: card.ID, CreatedAt: time.Now(), Status: "running", Tasks: []benchmark.ReviewTask{{RunID: run.ID}}}
	if err = r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	report.Status = "completed"
	if err = r.FinishReviewReport(report); err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyTaskManagement(context.Background(), before); !errors.As(err, &blocked) || blocked.Preview.Items[0].BlockedReason != "task_changed" {
		t.Fatalf("new report was deleted outside confirmed scope: %v", err)
	}
}

func TestTraceDeleteVersionDoesNotCollideAfterMetadataDeletionAndIDReuse(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	managementLog(t, r, "original", "shared-id", "")
	metadata, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "shared-id", Name: "first"})
	if err != nil {
		t.Fatal(err)
	}
	metadata.Name = "second"
	if _, err = r.SaveTraceMetadata(metadata); err != nil {
		t.Fatal(err)
	}
	before, _ := previewManagement(t, r, manageRequest("trace", "delete", "shared-id"))
	applyManagement(t, r, manageRequest("trace", "delete", "shared-id"))
	managementLog(t, r, "replacement", "shared-id", "")
	var blocked *TaskManagementBlockedError
	if _, err = r.ApplyTaskManagement(context.Background(), before); !errors.As(err, &blocked) || blocked.Preview.Items[0].BlockedReason != "task_changed" {
		t.Fatalf("stale token deleted reused trace ID: %v", err)
	}
}
