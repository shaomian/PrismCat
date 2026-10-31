package storage

import (
	"context"
	"strings"

	"github.com/shaomian/PrismCat/internal/trace"
)

// Intersect memberships inside SQLite; no complete per-run ID sets in Go.
func (r *SQLiteRepository) ExperimentRunsOverlap(ctx context.Context, ids []string) (bool, error) {
	if len(ids) < 2 {
		return false, ctx.Err()
	}
	queries := make([]string, 0, len(ids))
	args := []any{}
	for _, id := range ids {
		queries = append(queries, `SELECT l.id,l.method,l.path FROM request_logs l WHERE `+experimentLogScope+` AND COALESCE(tag,'') NOT LIKE 'prismcat-analysis%'`)
		args = append(args, id, id, id)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT path FROM (`+strings.Join(queries, " UNION ALL ")+`) WHERE method='POST' GROUP BY id HAVING COUNT(*)>1`, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return false, err
		}
		if trace.SupportedPath(path) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (a *AsyncRepository) ExperimentRunsOverlap(ctx context.Context, ids []string) (bool, error) {
	r, e := a.experimentStore()
	if e != nil {
		return false, e
	}
	if v, ok := r.(interface {
		ExperimentRunsOverlap(context.Context, []string) (bool, error)
	}); ok {
		return v.ExperimentRunsOverlap(ctx, ids)
	}
	// Compatibility repositories still provide LogIDs to benchmark.Compare.
	return false, nil
}
