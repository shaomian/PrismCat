package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shaomian/PrismCat/internal/benchmark"
)

func TestCapturePreviewIsScopedBoundedAndIncludesTransportErrors(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	capture, err := r.StartCaptureRun("capture", "entry", false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 7 {
		entry := newTestLog(fmt.Sprint(i), time.UnixMilli(1000+int64(i)))
		entry.CaptureRunID, entry.TraceID, entry.StatusCode = capture.ID, fmt.Sprintf("trace-%d", i%4), 200
		entry.RequestBody = strings.Repeat("DO_NOT_INCLUDE_BODY", 10000)
		entry.ResponseBodyRef = "DO_NOT_LOAD_BLOB"
		if i == 1 {
			entry.StatusCode = 429
		}
		if i == 5 {
			entry.Error = "DO_NOT_INCLUDE_ERROR_CONTENT"
			entry.StatusCode = 0
		}
		if i == 6 {
			entry.CaptureRunID = "different-capture"
			entry.TraceID, entry.StatusCode = "other-trace", 503
		}
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.SaveTraceMetadata(TraceMetadata{TraceID: "trace-0", Name: "Main task"}); err != nil {
		t.Fatal(err)
	}
	async := &AsyncRepository{inner: r}
	preview, err := async.GetCaptureRunPreview(context.Background(), capture.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TraceCount != 4 || len(preview.Traces) != 3 || preview.Traces[0].Name != "Main task" || preview.ErrorCount != 2 || preview.LatestError == nil || preview.LatestError.StatusCode != 0 {
		t.Fatalf("incorrect preview: %+v", preview)
	}
	data, _ := json.Marshal(preview)
	if len(data) > 2048 || strings.Contains(string(data), "DO_NOT_") || strings.Contains(string(data), "other-trace") {
		t.Fatalf("preview included full or unrelated data: %s", data)
	}
}

func TestExperimentPreviewReadsSelectedPointsAndTaskMetadataOnly(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	e.Task.Prompt = strings.Repeat("任务背景🧪", 60)
	e.Notes = "DO_NOT_INCLUDE_NOTES"
	e.Review = &benchmark.ReviewConfig{TemplateName: "Custom"}
	for i := range 6 {
		e.Review.Points = append(e.Review.Points, benchmark.ReviewPoint{ID: fmt.Sprint(i), Name: fmt.Sprintf("Observation %d", i), Instructions: "DO_NOT_INCLUDE_INSTRUCTIONS"})
	}
	e.Review.Selected = []string{"5", "0", "4", "2", "3"}
	var err error
	e, err = r.SaveExperiment(e)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		if _, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: fmt.Sprintf("Task %d", i), Conditions: benchmark.Conditions{Agent: "Agent", Model: "Model"}, ReviewNotes: "DO_NOT_INCLUDE_RUN_NOTES"}); err != nil {
			t.Fatal(err)
		}
	}
	// 旧报告内容可以很大甚至不可解析；悬停只读取索引列中的完成状态与时间。
	for _, report := range []struct {
		id, status string
		at         int64
	}{{"completed", "completed", 1000}, {"failed", "failed", 2000}} {
		if _, err := r.db.Exec(`INSERT INTO review_reports(id,experiment_id,status,model,question,parent_id,created_at,document) VALUES(?,?,?,'','','',?,'DO_NOT_PARSE_REPORT')`, report.id, e.ID, report.status, report.at); err != nil {
			t.Fatal(err)
		}
	}
	preview, err := (&AsyncRepository{inner: r}).GetExperimentPreview(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TaskCount != 5 || len(preview.Tasks) != 3 || preview.Tasks[0].Agent != "Agent" || preview.Tasks[0].Model != "Model" || preview.PointCount != 5 || strings.Join(preview.Points, ",") != "Observation 5,Observation 0,Observation 4,Observation 2" {
		t.Fatalf("incorrect preview: %+v", preview)
	}
	if preview.LastReportAt == nil || preview.LastReportAt.UnixMilli() != 1000 || !strings.HasSuffix(preview.Background, "…") || utf8.RuneCountInString(preview.Background) > 241 || !utf8.ValidString(preview.Background) {
		t.Fatalf("incorrect excerpt or report time: %+v", preview)
	}
	data, _ := json.Marshal(preview)
	if len(data) > 4096 || strings.Contains(string(data), "DO_NOT_") || strings.Contains(string(data), "Observation 1") {
		t.Fatalf("preview leaked full/unselected data: %s", data)
	}
}

func TestTaskPreviewEmptyMissingLegacyAndCancellation(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	capture, err := r.StartCaptureRun("empty", "entry", false)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := r.GetCaptureRunPreview(context.Background(), capture.ID)
	if err != nil || preview.TraceCount != 0 || preview.ErrorCount != 0 || preview.LatestError != nil || preview.Traces == nil {
		t.Fatalf("empty capture: %+v %v", preview, err)
	}
	e, err := r.SaveExperiment(benchmark.Experiment{Name: "legacy"})
	if err != nil {
		t.Fatal(err)
	}
	experiment, err := r.GetExperimentPreview(context.Background(), e.ID)
	if err != nil || experiment.Background != "" || experiment.PointCount != 0 || experiment.TaskCount != 0 || experiment.LastReportAt != nil || experiment.Tasks == nil || experiment.Points == nil {
		t.Fatalf("legacy preview: %+v %v", experiment, err)
	}
	if _, err := r.GetCaptureRunPreview(context.Background(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing capture returned %v", err)
	}
	if _, err := r.GetExperimentPreview(context.Background(), "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing experiment returned %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.GetCaptureRunPreview(ctx, capture.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled capture query returned %v", err)
	}
	if _, err := r.GetExperimentPreview(ctx, e.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled experiment query returned %v", err)
	}
}
