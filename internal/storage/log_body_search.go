package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type LogBodyPosition struct {
	TimeMS int64  `json:"time_ms"`
	ID     string `json:"id"`
}

type TraceBodySearchRepository interface {
	VisitTraceLogBodies(context.Context, string, *LogBodyPosition, int, func(*RequestLog, LogBodyPosition) error) error
}

func (a *AsyncRepository) VisitTraceLogBodies(ctx context.Context, traceID string, before *LogBodyPosition, limit int, visit func(*RequestLog, LogBodyPosition) error) error {
	if repo, ok := a.inner.(TraceBodySearchRepository); ok {
		return repo.VisitTraceLogBodies(ctx, traceID, before, limit, visit)
	}
	return errors.New("body search is unavailable")
}

// VisitTraceLogBodies selects candidate IDs before fetching bodies. No SQLite
// cursor or transaction stays open while the caller restores a Blob.
func (r *SQLiteRepository) VisitTraceLogBodies(ctx context.Context, traceID string, before *LogBodyPosition, limit int, visit func(*RequestLog, LogBodyPosition) error) error {
	query := `SELECT id, created_at_unix_ms FROM request_logs WHERE trace_id = ?`
	args := []any{traceID}
	if before != nil {
		query += ` AND (created_at_unix_ms, id) < (?, ?)`
		args = append(args, before.TimeMS, before.ID)
	}
	rows, err := r.db.QueryContext(ctx, query+` ORDER BY created_at_unix_ms DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return err
	}
	defer rows.Close()
	var positions []LogBodyPosition
	for rows.Next() {
		var position LogBodyPosition
		if err := rows.Scan(&position.ID, &position.TimeMS); err != nil {
			return err
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, position := range positions {
		var log RequestLog
		var requestHeaders, responseHeaders string
		err := r.db.QueryRowContext(ctx, `
			SELECT id, created_at, upstream, method, path, COALESCE(streaming, 0), COALESCE(truncated, 0),
				COALESCE(request_headers, ''), COALESCE(request_body, ''), COALESCE(request_body_ref, ''),
				COALESCE(response_headers, ''), COALESCE(response_body, ''), COALESCE(response_body_ref, '')
			FROM request_logs WHERE id = ? AND trace_id = ?
		`, position.ID, traceID).Scan(&log.ID, &log.CreatedAt, &log.Upstream, &log.Method, &log.Path, &log.Streaming, &log.Truncated,
			&requestHeaders, &log.RequestBody, &log.RequestBodyRef,
			&responseHeaders, &log.ResponseBody, &log.ResponseBodyRef)
		if errors.Is(err, sql.ErrNoRows) {
			continue // Retention may remove a candidate after selection.
		}
		if err != nil {
			return err
		}
		_ = json.Unmarshal([]byte(requestHeaders), &log.RequestHeaders)
		_ = json.Unmarshal([]byte(responseHeaders), &log.ResponseHeaders)
		if err := visit(&log, position); err != nil {
			return err
		}
	}
	return nil
}
