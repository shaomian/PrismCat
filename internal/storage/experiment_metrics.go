package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

const experimentLogScope = `((capture_run_id!='' AND capture_run_id=(SELECT capture_run_id FROM experiment_runs WHERE id=?)) OR trace_id IN (SELECT et.trace_id FROM experiment_run_traces et WHERE run_id=? AND NOT EXISTS(SELECT 1 FROM experiment_trace_snapshots s WHERE s.run_id=et.run_id AND s.trace_id=et.trace_id)) OR EXISTS(SELECT 1 FROM experiment_trace_samples s WHERE s.run_id=? AND s.log_id=l.id))`

// MeasureExperimentRun keeps older repository integrations working while SQLite
// streams lightweight rows instead of retaining a RequestLog and Sample per row.
func MeasureExperimentRun(ctx context.Context, repo ExperimentRepository, id string, limit int) (benchmark.Metrics, error) {
	if limit <= 0 {
		return benchmark.Metrics{}, benchmark.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return benchmark.Metrics{}, err
	}
	if measurer, ok := repo.(interface {
		MeasureExperimentRun(context.Context, string, int) (benchmark.Metrics, error)
	}); ok {
		return measurer.MeasureExperimentRun(ctx, id, limit)
	}
	logs, err := repo.ExperimentLogs(ctx, id, limit+1)
	if err != nil {
		return benchmark.Metrics{}, err
	}
	// The compatibility path retains its existing raw-row limit.
	sampled := len(logs) > limit
	if sampled {
		logs = logs[:limit]
	}
	var accumulator benchmark.Accumulator
	for _, log := range logs {
		if err := ctx.Err(); err != nil {
			return benchmark.Metrics{}, err
		}
		if log.Method != "POST" || !trace.SupportedPath(log.Path) {
			continue
		}
		accumulator.Add(benchmark.Sample{ID: log.ID, TraceID: log.TraceID, CreatedAt: log.CreatedAt.UnixMilli(),
			Latency: log.Latency, Status: log.StatusCode, Error: log.Error != "",
			Incomplete: log.WebSocket != nil && log.WebSocket.ObservationError != "",
			Input:      log.UsageInputTokens, Output: log.UsageOutputTokens, Total: log.UsageTotalTokens})
	}
	return accumulator.Finish(sampled), ctx.Err()
}

type sampleQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func walkExperimentSamples(ctx context.Context, reader sampleQueryer, id string, visit func(benchmark.Sample) (bool, error)) error {
	// Filter method/path in Go: using the method index here makes SQLite sort
	// the entire task before yielding rows instead of walking its time index.
	rows, err := reader.QueryContext(ctx, `SELECT id,created_at,method,path,status_code,latency_ms,COALESCE(error,'')!='',trace_id,COALESCE(websocket_info,''),usage_input_tokens,usage_output_tokens,usage_total_tokens
		FROM request_logs l WHERE `+experimentLogScope+` AND COALESCE(tag,'') NOT LIKE 'prismcat-analysis%' ORDER BY created_at_unix_ms,id`, id, id, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	var sample benchmark.Sample
	var at time.Time
	var method, path, websocket string
	var input, output, total sql.NullInt64
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rows.Scan(&sample.ID, &at, &method, &path, &sample.Status, &sample.Latency, &sample.Error, &sample.TraceID, &websocket, &input, &output, &total); err != nil {
			return err
		}
		if method != "POST" || !trace.SupportedPath(path) {
			continue
		}
		sample.CreatedAt = at.UnixMilli()
		sample.Incomplete = false
		if websocket != "" {
			var info WebSocketInfo
			_ = json.Unmarshal([]byte(websocket), &info)
			sample.Incomplete = info.ObservationError != ""
		}
		// Add consumes values immediately; reuse scan storage without per-token allocations.
		sample.Input, sample.Output, sample.Total = nil, nil, nil
		if input.Valid {
			sample.Input = &input.Int64
		}
		if output.Valid {
			sample.Output = &output.Int64
		}
		if total.Valid {
			sample.Total = &total.Int64
		}
		more, err := visit(sample)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *SQLiteRepository) MeasureExperimentRun(ctx context.Context, id string, limit int) (benchmark.Metrics, error) {
	if limit <= 0 {
		return benchmark.Metrics{}, benchmark.ErrInvalid
	}
	var accumulator benchmark.Accumulator
	sampled := false
	err := walkExperimentSamples(ctx, r.db, id, func(s benchmark.Sample) (bool, error) {
		if accumulator.Count() >= limit {
			sampled = true
			return false, nil
		}
		accumulator.Add(s)
		return true, nil
	})
	if err != nil {
		return benchmark.Metrics{}, err
	}
	return accumulator.Finish(sampled), nil
}

func (r *SQLiteRepository) MeasureFullExperimentRun(ctx context.Context, id string) (benchmark.Metrics, error) {
	accumulator := benchmark.NewSummaryAccumulator()
	err := walkExperimentSamples(ctx, r.db, id, func(s benchmark.Sample) (bool, error) { accumulator.Add(s); return true, nil })
	if err != nil {
		return benchmark.Metrics{}, err
	}
	return accumulator.Finish(false), nil
}

func FullExperimentMetrics(ctx context.Context, repo ExperimentRepository, id string) (benchmark.Metrics, error) {
	if measurer, ok := repo.(interface {
		MeasureFullExperimentRun(context.Context, string) (benchmark.Metrics, error)
	}); ok {
		return measurer.MeasureFullExperimentRun(ctx, id)
	}
	// Older integrations retain an explicit bounded/sampled compatibility path.
	return MeasureExperimentRun(ctx, repo, id, 200000)
}
