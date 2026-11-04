package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
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

// Keep the released v2.4.0 scope only as a benchmark baseline. Its correlated
// frozen-member lookup and full history scan are the bottlenecks under test.
const release240ExperimentScope = `((capture_run_id!='' AND capture_run_id=(SELECT capture_run_id FROM experiment_runs WHERE id=?)) OR trace_id IN (SELECT et.trace_id FROM experiment_run_traces et WHERE run_id=? AND NOT EXISTS(SELECT 1 FROM experiment_trace_snapshots s WHERE s.run_id=et.run_id AND s.trace_id=et.trace_id)) OR EXISTS(SELECT 1 FROM experiment_trace_samples s WHERE s.run_id=? AND s.log_id=l.id))`

type release240ScopeReader struct{ repo *SQLiteRepository }

func (r release240ScopeReader) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.repo.db.QueryContext(ctx, strings.Replace(query, experimentLogScope, release240ExperimentScope, 1), args...)
}

func BenchmarkExperimentScope(b *testing.B) {
	for _, fixture := range []struct {
		name         string
		count, noise int
		frozen       bool
	}{{"frozen_10000", 10000, 0, true}, {"sparse_200000", 10, 200000, false}, {"dense_100000", 100000, 0, false}} {
		b.Run(fixture.name, func(b *testing.B) {
			r, err := NewSQLiteRepository(filepath.Join(b.TempDir(), "scope.db"))
			if err != nil {
				b.Fatal(err)
			}
			defer r.Close()
			seedExperimentScopeLogs(b, r, fixture.count, "selected", "source")
			seedExperimentScopeLogs(b, r, fixture.noise, "noise", "unrelated")
			e, err := r.SaveExperiment(benchmark.Experiment{Name: "scope"})
			if err != nil {
				b.Fatal(err)
			}
			run, err := r.SaveExperimentRun(benchmark.Run{ExperimentID: e.ID, Name: fixture.name, Conditions: benchmark.Conditions{Agent: "a"}, TraceIDs: []string{"source"}})
			if err != nil {
				b.Fatal(err)
			}
			if fixture.frozen {
				if _, err := r.db.Exec(`INSERT INTO experiment_trace_snapshots VALUES(?,'source')`, run.ID); err != nil {
					b.Fatal(err)
				}
				if _, err := r.db.Exec(`INSERT INTO experiment_trace_samples SELECT ?,trace_id,id FROM request_logs WHERE trace_id='source'`, run.ID); err != nil {
					b.Fatal(err)
				}
			}
			for _, legacy := range []bool{true, false} {
				name := "indexed_candidates"
				if legacy {
					name = "v240"
				}
				b.Run(name, func(b *testing.B) {
					// Restore each version's index set outside the measured operation.
					statement := `CREATE INDEX IF NOT EXISTS idx_experiment_sample_run_log ON experiment_trace_samples(run_id,log_id)`
					if legacy {
						statement = `DROP INDEX IF EXISTS idx_experiment_sample_run_log`
					}
					if _, err := r.db.Exec(statement); err != nil {
						b.Fatal(err)
					}
					var reader sampleQueryer = r.db
					if legacy {
						reader = release240ScopeReader{r}
					}
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						accumulator := benchmark.NewSummaryAccumulator()
						err := walkExperimentSamples(context.Background(), reader, run.ID, func(s benchmark.Sample) (bool, error) {
							accumulator.Add(s)
							return true, nil
						})
						metrics := accumulator.Finish(false)
						if err != nil || metrics.Requests != fixture.count || metrics.TotalTokens == nil || *metrics.TotalTokens != int64(fixture.count)*110 {
							b.Fatalf("scope changed statistics: %+v %v", metrics, err)
						}
					}
				})
			}
		})
	}
}
