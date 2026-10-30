package storage

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

func reviewTestConfig() benchmark.ReviewConfig {
	return benchmark.ReviewConfig{TemplateName: "Custom reverse review", Points: []benchmark.ReviewPoint{{ID: "logic", Name: "Key logic", Instructions: "Cite evidence"}}, Selected: []string{"logic"}}
}
func TestReviewTemplateSnapshotAndLightweightCatalog(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	v, err := r.SaveReviewTemplate(benchmark.ReviewTemplate{Name: "Reverse", Config: reviewTestConfig()})
	if err != nil {
		t.Fatal(err)
	}
	old := v
	e := testExperiment(t, r)
	e.Review = &v.Config
	e, err = r.SaveExperiment(e)
	if err != nil {
		t.Fatal(err)
	}
	v.Config.Points[0].Name = "Different question"
	v, err = r.SaveReviewTemplate(v)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := r.GetExperiment(e.ID)
	if err != nil || loaded.Review.Points[0].Name != "Key logic" {
		t.Fatalf("template edit mutated card: %+v %v", loaded, err)
	}
	if _, err = r.SaveReviewTemplate(old); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatalf("stale template save accepted: %v", err)
	}
	cards, _, err := r.ListExperiments("", false, 0, 20)
	if err != nil || len(cards) != 1 || cards[0].Review != nil || cards[0].ReviewSummary.PointCount != 1 || cards[0].ReviewSummary.TemplateID != loaded.Review.TemplateID {
		t.Fatalf("catalog loaded full config: %+v %v", cards, err)
	}
	v.Archived = true
	if _, err = r.SaveReviewTemplate(v); err != nil {
		t.Fatal(err)
	}
	templates, total, err := r.ListReviewTemplates("", 0, 20)
	if err != nil || total != 0 || len(templates) != 0 {
		t.Fatal("archived template remained visible")
	}
	if _, err = r.GetExperiment(e.ID); err != nil {
		t.Fatal("archiving template removed card")
	}
}
func TestReviewReportKeepsOriginalEvidenceAndCaptureCanBeReusedAcrossCards(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	other := testExperiment(t, r)
	capture, err := r.StartCaptureRun("task", "a", false)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "a", Conditions: benchmark.Conditions{Agent: "a"}, CaptureRunID: capture.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.SaveExperimentRun(benchmark.Run{ExperimentID: other.ID, Name: "b", Conditions: benchmark.Conditions{Agent: "a"}, CaptureRunID: capture.ID}); err != nil {
		t.Fatalf("capture could not be reused across cards: %v", err)
	}
	entry := newTestLog("old-evidence", time.Now().Add(-72*time.Hour))
	entry.TraceID = "source"
	if err = r.SaveLog(entry); err != nil {
		t.Fatal(err)
	}
	report := benchmark.ReviewReport{ID: "report", ExperimentID: e.ID, Status: "running", Config: reviewTestConfig(), CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{RunID: run.ID, Metrics: benchmark.Metrics{LogIDs: []string{entry.ID}}}}}
	if err = r.CreateReviewReport(report); err != nil {
		t.Fatal(err)
	}
	second := report
	second.ID = "duplicate"
	if err = r.CreateReviewReport(second); !errors.Is(err, ErrCorrelationConflict) {
		t.Fatalf("second active report accepted: %v", err)
	}
	report.Status = "completed"
	report.Output = &benchmark.ReviewOutput{Summary: "review result"}
	if err = r.FinishReviewReport(report); err != nil {
		t.Fatal(err)
	}
	summary, total, err := r.ListReviewReports(e.ID, 0, 10)
	if err != nil || total != 1 || summary[0].Status != "completed" {
		t.Fatal("report history not persisted")
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 0 {
		t.Fatalf("original report evidence pruned: %d %v", n, err)
	}
	e.Keep = false
	if _, err = r.SaveExperiment(e); err != nil {
		t.Fatal(err)
	}
	if n, err := r.DeleteLogsBefore(time.Now()); err != nil || n != 1 {
		t.Fatalf("unprotected evidence not eligible: %d %v", n, err)
	}
	saved, err := r.GetReviewReport(report.ID)
	if err != nil || saved.Output.Summary != "review result" {
		t.Fatal("cleanup deleted saved report")
	}
}
func TestReviewRestartMarksUnfinishedReportsInterrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reports.db")
	r, err := NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	e := testExperiment(t, r)
	if err = r.CreateReviewReport(benchmark.ReviewReport{ID: "interrupted", ExperimentID: e.ID, Status: "running", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	r.Close()
	r, err = NewSQLiteRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	v, err := r.GetReviewReport("interrupted")
	if err != nil || v.Status != "interrupted" {
		t.Fatalf("restart left a phantom analysis: %+v %v", v, err)
	}
	if err = r.CreateReviewReport(benchmark.ReviewReport{ID: "retry", ExperimentID: e.ID, Status: "running", CreatedAt: time.Now()}); err != nil {
		t.Fatalf("interrupted report blocked retry: %v", err)
	}
}

func TestDeleteReviewReport(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	other := testExperiment(t, r)

	// 运行中的报告拒绝删除
	if err := r.CreateReviewReport(benchmark.ReviewReport{ID: "active", ExperimentID: e.ID, Status: "running", Config: reviewTestConfig(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteReviewReport("active", e.ID); !errors.Is(err, ErrReportActive) {
		t.Fatalf("running report deleted: %v", err)
	}
	// 归属校验:别的卡片的报告按不存在处理
	if err := r.DeleteReviewReport("active", other.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("foreign experiment accepted: %v", err)
	}

	// 完成后可删除,历史随之消失
	if err := r.FinishReviewReport(benchmark.ReviewReport{ID: "active", ExperimentID: e.ID, Status: "completed", Config: reviewTestConfig(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := r.DeleteReviewReport("active", e.ID); err != nil {
		t.Fatalf("delete completed report: %v", err)
	}
	if _, total, err := r.ListReviewReports(e.ID, 0, 10); err != nil || total != 0 {
		t.Fatalf("deleted report still listed: %d %v", total, err)
	}
	if err := r.DeleteReviewReport("active", e.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("double delete accepted: %v", err)
	}
}
