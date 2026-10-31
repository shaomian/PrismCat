package storage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

// Hide the optional streaming method to exercise older repository integrations.
type legacyMetricsRepository struct{ ExperimentRepository }

func TestStreamingExperimentMetricsPreserveRecordedValues(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	e := testExperiment(t, r)
	at := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	in, out, zero, total := int64(100), int64(20), int64(0), int64(9)
	logs := []*RequestLog{
		{ID: "a", Method: "POST", Path: "/v1/chat/completions", StatusCode: 200, Latency: 100, UsageInputTokens: &in, UsageOutputTokens: &out},
		{ID: "b", Method: "POST", Path: "/v1/responses", StatusCode: 500, Latency: 100, UsageInputTokens: &zero},
		{ID: "c", Method: "POST", Path: "/v1/messages"},
		{ID: "d", Method: "POST", Path: "/v1beta/models/gemini:generateContent", Error: "canceled", Latency: -10, UsageTotalTokens: &total},
		{ID: "e", Method: "POST", Path: "/v1/responses", StatusCode: 200, Latency: 9000, WebSocket: &WebSocketInfo{Kind: "response", ObservationError: "disconnected"}, UsageInputTokens: &zero, UsageOutputTokens: &zero},
		{ID: "f", Method: "POST", Path: "/v1/responses", StatusCode: 200},
		{ID: "get", Method: "GET", Path: "/v1/responses", StatusCode: 500},
		{ID: "aux", Method: "POST", Path: "/telemetry", StatusCode: 500},
		{ID: "analysis", Method: "POST", Path: "/v1/responses", Tag: "prismcat-analysis-review", StatusCode: 500},
	}
	for i, log := range logs {
		log.CreatedAt, log.Upstream, log.TraceID = at.Add(time.Duration(i)*50*time.Millisecond), "agent", "stats"
		if err := r.SaveLog(log); err != nil {
			t.Fatal(err)
		}
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "stats", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"stats"}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := MeasureExperimentRun(context.Background(), legacyMetricsRepository{r}, run.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.MeasureExperimentRun(context.Background(), run.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("streaming changed statistics: got %+v want %+v", got, want)
	}
	if got.Requests != 6 || got.Errors != 2 || got.Incomplete != 2 || got.UsageKnown != 2 || got.Sampled || *got.SpanMS != 250 || *got.InputTokens != 100 || *got.OutputTokens != 20 || *got.TotalTokens != 129 {
		t.Fatalf("recorded values were changed: %+v", got)
	}
	// Re-reading changed metadata through AsyncRepository must not reuse old fields.
	if _, err := r.db.Exec("UPDATE request_logs SET websocket_info='' WHERE id='e'"); err != nil {
		t.Fatal(err)
	}
	a := NewAsyncRepository(r, nil, 16)
	defer a.Close()
	wrapped, err := MeasureExperimentRun(context.Background(), a, run.ID, 100)
	if err != nil || wrapped.Incomplete != 1 || *wrapped.SpanMS != 9200 {
		t.Fatalf("async streaming or row reuse failed: %+v %v", wrapped, err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureExperimentRun(canceled, a, run.ID, 100); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled statistics returned data: %v", err)
	}
	if _, err := r.MeasureExperimentRun(context.Background(), run.ID, 0); !errors.Is(err, benchmark.ErrInvalid) {
		t.Fatalf("invalid limit accepted: %v", err)
	}
}

func TestStreamingStatisticsLimitCountsModelRequestsOnly(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	at := time.Now().Add(-time.Hour)
	for i, path := range []string{"/telemetry", "/telemetry", "/telemetry", "/v1/responses", "/v1/messages", "/telemetry", "/v1/chat/completions"} {
		entry := newTestLog(string(rune('a'+i)), at.Add(time.Duration(i)*time.Second))
		entry.TraceID, entry.Path, entry.StatusCode = "limit", path, 200
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "limit", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"limit"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{1, 2, 3, 4} {
		got, err := r.MeasureExperimentRun(context.Background(), run.ID, limit)
		if err != nil {
			t.Fatal(err)
		}
		if got.Requests != min(limit, 3) || got.Sampled != (limit < 3) || got.LogIDs[0] != "d" {
			t.Fatalf("auxiliary requests consumed limit %d: %+v", limit, got)
		}
	}
}

func TestStreamingStatisticsKeepCaptureUnionAndFrozenMembership(t *testing.T) {
	r := mustNewSQLiteRepoForTest(t)
	defer r.Close()
	e := testExperiment(t, r)
	capture, err := r.StartCaptureRun("stats", "agent", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"both", "capture-only", "trace-only"} {
		entry := newTestLog(id, time.Now().Add(-time.Hour))
		entry.StatusCode = 200
		if id != "trace-only" {
			entry.CaptureRunID = capture.ID
		}
		if err := r.SaveLog(entry); err != nil {
			t.Fatal(err)
		}
		if id != "capture-only" {
			if _, err := r.storeObservation(trace.Observation{LogID: id, CreatedAt: entry.CreatedAt, Assignment: trace.Assignment{TraceID: "source", Source: "auto", State: "inferred"}}, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := r.StopCaptureRun(capture.ID); err != nil {
		t.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "union", CaptureRunID: capture.ID, Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"source"}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.MeasureExperimentRun(context.Background(), run.ID, 100)
	if err != nil || got.Requests != 3 {
		t.Fatalf("capture union duplicated/lost logs: %+v %v", got, err)
	}
	groupingLog(t, r, "target-log", "target", false)
	applyGrouping(t, r, TraceGroupingRequest{Action: "merge", TraceIDs: []string{"source", "target"}, Target: "target"})
	groupingLog(t, r, "late-log", "target", false)
	frozen, err := r.MeasureExperimentRun(context.Background(), run.ID, 100)
	if err != nil || frozen.Requests != got.Requests || !reflect.DeepEqual(got.LogIDs, frozen.LogIDs) {
		t.Fatalf("streaming escaped frozen membership: got %+v want %+v (%v)", frozen, got, err)
	}
}
