package benchmark

import (
	"fmt"
	"testing"
)

func TestBasicMetricsDeduplicateAndUseElapsedTime(t *testing.T) {
	in, out := int64(100), int64(20)
	samples := []Sample{
		{ID: "a", TraceID: "task", CreatedAt: 1000, Latency: 100, Status: 200, Input: &in, Output: &out},
		{ID: "b", TraceID: "task", CreatedAt: 1050, Latency: 100, Status: 500},
		{ID: "c", CreatedAt: 1300, Latency: 50, Status: 0, Error: true},
	}
	samples = append(samples, samples[0])
	m := Measure(samples, false)
	if m.Requests != 3 || m.Errors != 2 || len(m.TraceIDs) != 1 || len(m.LogIDs) != 3 {
		t.Fatalf("duplicated or lost logs: %+v", m)
	}
	if m.SpanMS == nil || *m.SpanMS != 350 {
		t.Fatalf("elapsed time is wrong: %+v", m)
	}
	if m.InputTokens == nil || *m.InputTokens != 100 || m.OutputTokens == nil || *m.OutputTokens != 20 || m.TotalTokens == nil || *m.TotalTokens != 120 || m.UsageKnown != 1 {
		t.Fatalf("unknown usage became zero or totals were lost: %+v", m)
	}
	if Measure(nil, false).SpanMS != nil || Measure(nil, false).TotalTokens != nil {
		t.Fatal("empty run has fabricated metrics")
	}
	pending := Measure([]Sample{{ID: "pending", CreatedAt: 1000}}, false)
	if pending.SpanMS != nil || pending.Incomplete != 1 {
		t.Fatal("unfinished request has a final duration")
	}
}

func TestComparisonWarnsAboutPartialAndSharedData(t *testing.T) {
	run := Run{ID: "one", TaskHash: "same", State: "completed"}
	other := run
	other.ID = "two"
	other.TaskHash = "changed"
	comparison := Compare(Experiment{}, []Result{
		{Run: run, Metrics: Metrics{Requests: 1, LogIDs: []string{"shared"}, Sampled: true}},
		{Run: other, Metrics: Metrics{Requests: 1, UsageKnown: 1, LogIDs: []string{"shared"}, Incomplete: 1}},
	})
	contains := func(items []string, want string) bool {
		for _, item := range items {
			if item == want {
				return true
			}
		}
		return false
	}
	if !contains(comparison.Warnings, "task_changed") || !contains(comparison.Warnings, "overlapping_logs") || !contains(comparison.Results[0].Warnings, "partial_usage") || !contains(comparison.Results[0].Warnings, "sampled") || !contains(comparison.Results[1].Warnings, "run_not_completed") {
		t.Fatalf("missing data warnings: %+v", comparison)
	}
}

func TestSummaryAccumulatorDoesNotRetainMembership(t *testing.T) {
	a := NewSummaryAccumulator()
	for i := range 10000 {
		a.Add(Sample{ID: fmt.Sprint(i), TraceID: fmt.Sprint(i), Status: 200, CreatedAt: int64(i), Latency: 5})
	}
	m := a.Finish(false)
	if len(a.seen) != 0 || len(m.LogIDs) != 0 || len(m.TraceIDs) > 20 || m.Requests != 10000 || m.SpanMS == nil || *m.SpanMS != 10004 {
		t.Fatalf("summary grew with membership: %+v", m)
	}
}
