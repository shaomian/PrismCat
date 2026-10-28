package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func (r *SQLiteRepository) migrateTraceOrdering() error {
	exists, err := r.hasColumn("trace_summaries", "top_at")
	if err != nil {
		return err
	}
	if !exists {
		if _, err = r.db.Exec(`ALTER TABLE trace_summaries ADD COLUMN top_at INTEGER NOT NULL DEFAULT 0;
 UPDATE trace_summaries SET top_at=COALESCE((SELECT json_extract(document,'$.top_at') FROM trace_metadata m WHERE m.trace_id=trace_summaries.trace_id),0)`); err != nil {
			return err
		}
	}
	_, err = r.db.Exec(`CREATE INDEX IF NOT EXISTS idx_trace_summary_top_recent ON trace_summaries(top_at DESC,last_time DESC,trace_id);
 CREATE TRIGGER IF NOT EXISTS trace_metadata_top_insert AFTER INSERT ON trace_metadata BEGIN
 UPDATE trace_summaries SET top_at=COALESCE(json_extract(NEW.document,'$.top_at'),0) WHERE trace_id=NEW.trace_id; END;
 CREATE TRIGGER IF NOT EXISTS trace_metadata_top_update AFTER UPDATE OF document ON trace_metadata BEGIN
 UPDATE trace_summaries SET top_at=COALESCE(json_extract(NEW.document,'$.top_at'),0) WHERE trace_id=NEW.trace_id; END;
 CREATE TRIGGER IF NOT EXISTS trace_metadata_top_delete AFTER DELETE ON trace_metadata BEGIN
 UPDATE trace_summaries SET top_at=0 WHERE trace_id=OLD.trace_id; END;`)
	return err
}

func (r *SQLiteRepository) refreshTraceSummaries() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO trace_summaries(trace_id,request_count,first_time,last_time,total_latency,errors,input_tokens,output_tokens,total_tokens,upstreams,tags,sources,top_at)
 SELECT trace_id,COUNT(*),MIN(created_at_unix_ms),MAX(created_at_unix_ms),SUM(latency_ms),SUM(CASE WHEN COALESCE(error,'')!='' OR status_code>=400 THEN 1 ELSE 0 END),SUM(usage_input_tokens),SUM(usage_output_tokens),SUM(usage_total_tokens),json_group_array(DISTINCT upstream),json_group_array(DISTINCT tag) FILTER(WHERE tag!=''),json_group_array(DISTINCT CASE WHEN explicit_trace_id!='' THEN 'explicit' ELSE COALESCE(json_extract(NULLIF(trace_info,''),'$.source'),'explicit') END),COALESCE((SELECT json_extract(document,'$.top_at') FROM trace_metadata m WHERE m.trace_id=request_logs.trace_id),0)
 FROM request_logs WHERE trace_id IN (SELECT trace_id FROM trace_summary_dirty) GROUP BY trace_id
 ON CONFLICT(trace_id) DO UPDATE SET request_count=excluded.request_count,first_time=excluded.first_time,last_time=excluded.last_time,total_latency=excluded.total_latency,errors=excluded.errors,input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,total_tokens=excluded.total_tokens,upstreams=excluded.upstreams,tags=excluded.tags,sources=excluded.sources,top_at=excluded.top_at`); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM trace_summaries WHERE trace_id IN (SELECT trace_id FROM trace_summary_dirty) AND NOT EXISTS(SELECT 1 FROM request_logs l WHERE l.trace_id=trace_summaries.trace_id)"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM trace_summary_dirty"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) listTraceCatalog(filter TraceFilter) ([]TraceSummary, int64, error) {
	if err := r.refreshTraceSummaries(); err != nil {
		return nil, 0, err
	}
	conditions := []string{"1=1"}
	if filter.Mergeable {
		conditions = append(conditions, "NOT EXISTS(SELECT 1 FROM json_each(s.sources) WHERE value='explicit')")
	}
	args := []any{}
	add := func(clause string, values ...any) {
		conditions = append(conditions, clause)
		args = append(args, values...)
	}
	if filter.TraceID != "" {
		add("s.trace_id LIKE ?", "%"+filter.TraceID+"%")
	}
	if filter.Query != "" {
		q := "%" + filter.Query + "%"
		add("(s.trace_id LIKE ? OR COALESCE(json_extract(m.document,'$.name'),'') LIKE ? OR COALESCE(json_extract(m.document,'$.notes'),'') LIKE ? OR EXISTS(SELECT 1 FROM json_each(json_extract(m.document,'$.labels')) WHERE value LIKE ?))", q, q, q, q)
	}
	if filter.Upstream != "" {
		add("EXISTS(SELECT 1 FROM json_each(s.upstreams) WHERE value=?)", filter.Upstream)
	}
	if filter.Tag != "" {
		add("EXISTS(SELECT 1 FROM json_each(s.tags) WHERE value=?)", filter.Tag)
	}
	if filter.Label != "" {
		add("EXISTS(SELECT 1 FROM json_each(json_extract(m.document,'$.labels')) WHERE value=?)", filter.Label)
	}
	if filter.Status != "" {
		add("COALESCE(json_extract(m.document,'$.status'),'none')=?", filter.Status)
	}
	if filter.Archived != nil {
		add("COALESCE(json_extract(m.document,'$.archived'),0)=?", *filter.Archived)
	}
	if filter.Pinned != nil {
		add("COALESCE(json_extract(m.document,'$.pinned'),0)=?", *filter.Pinned)
	}
	if filter.HasError != nil {
		if *filter.HasError {
			add("s.errors>0")
		} else {
			add("s.errors=0")
		}
	}
	// Filter task activity, then return complete task aggregates, not a slice of its logs.
	if filter.StartTime != nil {
		add("s.last_time>=?", filter.StartTime.UnixMilli())
	}
	if filter.EndTime != nil {
		add("s.last_time<=?", filter.EndTime.UnixMilli())
	}
	from := " FROM trace_summaries s LEFT JOIN trace_metadata m ON m.trace_id=s.trace_id WHERE " + strings.Join(conditions, " AND ")
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, 1000)
	// Resolve provenance only for the requested page, using the operation index.
	// Historical splits need no log scan or per-item API/database round trips.
	rows, err := r.db.Query(`WITH page AS (SELECT s.*,COALESCE(m.document,'') AS metadata`+from+` ORDER BY s.top_at DESC,s.last_time DESC,s.trace_id LIMIT ? OFFSET ?)
 SELECT p.trace_id,p.request_count,p.first_time,p.last_time,p.total_latency,p.errors,p.input_tokens,p.output_tokens,p.total_tokens,p.upstreams,p.tags,p.sources,p.metadata,COALESCE(origin.sources,''),COALESCE(origin.log_count,0),COALESCE(origin.created_at,0)
 FROM page p LEFT JOIN trace_group_operations origin ON origin.id=(SELECT id FROM trace_group_operations WHERE target=p.trace_id AND action='split' ORDER BY created_at,id LIMIT 1)
 ORDER BY p.top_at DESC,p.last_time DESC,p.trace_id`, append(args, limit, max(0, filter.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []TraceSummary{}
	for rows.Next() {
		var s TraceSummary
		var in, out, totalTokens sql.NullInt64
		var upstreams, tags, sources, metadata, originSources string
		var originCount, originCreated int64
		if err = rows.Scan(&s.TraceID, &s.RequestCount, &s.FirstTime, &s.LastTime, &s.TotalLatency, &s.ErrorCount, &in, &out, &totalTokens, &upstreams, &tags, &sources, &metadata, &originSources, &originCount, &originCreated); err != nil {
			return nil, 0, err
		}
		s.UsageInputTokens = nullIntPtr(in)
		s.UsageOutputTokens = nullIntPtr(out)
		s.UsageTotalTokens = nullIntPtr(totalTokens)
		_ = json.Unmarshal([]byte(upstreams), &s.Upstreams)
		_ = json.Unmarshal([]byte(tags), &s.Tags)
		_ = json.Unmarshal([]byte(sources), &s.Sources)
		s.Origin, err = splitTraceOrigin(originSources, originCount, originCreated)
		if err != nil {
			return nil, 0, fmt.Errorf("trace origin: %w", err)
		}
		if metadata != "" {
			if err = json.Unmarshal([]byte(metadata), &s.Metadata); err != nil {
				return nil, 0, fmt.Errorf("trace metadata: %w", err)
			}
			if s.Metadata != nil {
				s.Metadata.Origin = s.Origin
			}
		}
		items = append(items, s)
	}
	return items, total, rows.Err()
}
