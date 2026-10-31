package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

// Baseline from the previous API implementation: metadata slice, Sample slice,
// then aggregation. Keep it only in benchmarks to make before/after repeatable.
func materializedExperimentMetrics(ctx context.Context, repo ExperimentRepository, id string, limit int) (benchmark.Metrics, error) {
	logs, err := repo.ExperimentLogs(ctx, id, limit+1)
	if err != nil {
		return benchmark.Metrics{}, err
	}
	sampled := len(logs) > limit
	if sampled {
		logs = logs[:limit]
	}
	samples := []benchmark.Sample{}
	for _, log := range logs {
		if err := ctx.Err(); err != nil {
			return benchmark.Metrics{}, err
		}
		if log.Method != "POST" || !trace.SupportedPath(log.Path) {
			continue
		}
		samples = append(samples, benchmark.Sample{ID: log.ID, TraceID: log.TraceID, CreatedAt: log.CreatedAt.UnixMilli(),
			Latency: log.Latency, Status: log.StatusCode, Error: log.Error != "",
			Incomplete: log.WebSocket != nil && log.WebSocket.ObservationError != "",
			Input:      log.UsageInputTokens, Output: log.UsageOutputTokens, Total: log.UsageTotalTokens})
	}
	return benchmark.Measure(samples, sampled), nil
}

// Run explicitly with -run '^$' -bench 'BenchmarkExperimentMetrics|BenchmarkReviewSnapshot'
// -benchtime=1x -benchmem. All data lives in temporary SQLite databases.
func BenchmarkExperimentMetrics(b *testing.B) {
	r, err := NewSQLiteRepository(filepath.Join(b.TempDir(), "metrics.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	tx, err := r.db.Begin()
	if err != nil {
		b.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO request_logs(id,created_at,created_at_unix_ms,upstream,target_url,method,path,status_code,latency_ms,error,trace_id,usage_input_tokens,usage_output_tokens,usage_total_tokens) VALUES(?,?,?,'bench','','POST','/v1/chat/completions',200,5,'','large',100,10,110)`)
	if err != nil {
		b.Fatal(err)
	}
	const count = 100000
	at := time.Now().Add(-time.Hour)
	for i := range count {
		created := at.Add(time.Duration(i) * time.Millisecond)
		if _, err := statement.Exec(fmt.Sprintf("log-%06d", i), created, created.UnixMilli()); err != nil {
			b.Fatal(err)
		}
	}
	statement.Close()
	if err := tx.Commit(); err != nil {
		b.Fatal(err)
	}
	e, err := r.SaveExperiment(benchmark.Experiment{Name: "bench"})
	if err != nil {
		b.Fatal(err)
	}
	run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: "large", Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"large"}})
	if err != nil {
		b.Fatal(err)
	}
	for _, implementation := range []struct {
		name    string
		measure func(context.Context, ExperimentRepository, string, int) (benchmark.Metrics, error)
	}{{"metadata_slice", materializedExperimentMetrics}, {"streaming", MeasureExperimentRun}, {"full_summary", func(ctx context.Context, repo ExperimentRepository, id string, _ int) (benchmark.Metrics, error) {
		return FullExperimentMetrics(ctx, repo, id)
	}}} {
		b.Run(implementation.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				metrics, err := implementation.measure(context.Background(), r, run.ID, 200000)
				if err != nil || metrics.Requests != count || metrics.TotalTokens == nil || *metrics.TotalTokens != count*110 {
					b.Fatalf("invalid statistics: %+v %v", metrics, err)
				}
			}
		})
	}
}

func BenchmarkReviewSnapshot(b *testing.B) {
	r, err := NewSQLiteRepository(filepath.Join(b.TempDir(), "review.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close()
	e, err := r.SaveExperiment(benchmark.Experiment{Name: "bench"})
	if err != nil {
		b.Fatal(err)
	}
	ids := make([]string, 100000)
	for i := range ids {
		ids[i] = fmt.Sprintf("snapshot-%06d", i)
	}
	report := benchmark.ReviewReport{ExperimentID: e.ID, Status: "completed", CreatedAt: time.Now(), Tasks: []benchmark.ReviewTask{{Metrics: benchmark.Metrics{LogIDs: ids}}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		report.ID = fmt.Sprintf("snapshot-%d", i)
		if err := r.CreateReviewReport(report); err != nil {
			b.Fatal(err)
		}
	}
}
