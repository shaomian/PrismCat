package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ExpiredCleanupPreview contains counts only. Bodies and individual log IDs are
// not read or sent to the console while estimating a cleanup.
type ExpiredCleanupPreview struct {
	ExpiredLogs    int64 `json:"expired_logs"`
	EligibleLogs   int64 `json:"eligible_logs"`
	EligibleTraces int64 `json:"eligible_traces"`
	RetainedLogs   int64 `json:"retained_logs"`
}

func (r *SQLiteRepository) PreviewExpiredCleanup(ctx context.Context, before time.Time) (ExpiredCleanupPreview, error) {
	var result ExpiredCleanupPreview
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(log_count),0),COALESCE(SUM(CASE WHEN group_key LIKE 'trace:%' THEN 1 ELSE 0 END),0)
		FROM trace_retention_groups WHERE last_time<?`, before.UnixMilli()).Scan(&result.EligibleLogs, &result.EligibleTraces); err != nil {
		return result, err
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs WHERE created_at_unix_ms<?", before.UnixMilli()).Scan(&result.ExpiredLogs); err != nil {
		return result, err
	}
	result.RetainedLogs = max(0, result.ExpiredLogs-result.EligibleLogs)
	return result, tx.Commit()
}

func (a *AsyncRepository) PreviewExpiredCleanup(ctx context.Context, before time.Time) (ExpiredCleanupPreview, error) {
	if repo, ok := a.inner.(interface {
		PreviewExpiredCleanup(context.Context, time.Time) (ExpiredCleanupPreview, error)
	}); ok {
		return repo.PreviewExpiredCleanup(ctx, before)
	}
	return ExpiredCleanupPreview{}, errors.New("expired cleanup preview unavailable")
}
