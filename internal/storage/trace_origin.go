package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// TraceOrigin describes how a trace was created, independently of request
// assignment sources and user-editable labels. Only recorded splits qualify.
type TraceOrigin struct {
	Kind           string   `json:"kind"`
	SourceTraceIDs []string `json:"source_trace_ids"`
	RequestCount   int64    `json:"request_count"`
	CreatedAt      int64    `json:"created_at"`
}

func splitTraceOrigin(sources string, count, created int64) (*TraceOrigin, error) {
	if sources == "" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(sources), &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 || count <= 0 || created <= 0 {
		return nil, nil
	}
	return &TraceOrigin{Kind: "manual_split", SourceTraceIDs: ids, RequestCount: count, CreatedAt: created}, nil
}

func (r *SQLiteRepository) traceOrigin(id string) (*TraceOrigin, error) {
	var sources string
	var count, created int64
	err := r.db.QueryRow(`SELECT sources,log_count,created_at FROM trace_group_operations WHERE target=? AND action='split' ORDER BY created_at,id LIMIT 1`, id).Scan(&sources, &count, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return splitTraceOrigin(sources, count, created)
}
