package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

func (r *SQLiteRepository) refreshTraceSummaries() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`INSERT INTO trace_summaries
 SELECT trace_id,COUNT(*),MIN(created_at_unix_ms),MAX(created_at_unix_ms),SUM(latency_ms),SUM(CASE WHEN COALESCE(error,'')!='' OR status_code>=400 THEN 1 ELSE 0 END),SUM(usage_input_tokens),SUM(usage_output_tokens),SUM(usage_total_tokens),json_group_array(DISTINCT upstream),json_group_array(DISTINCT tag) FILTER(WHERE tag!=''),json_group_array(DISTINCT CASE WHEN explicit_trace_id!='' THEN 'explicit' ELSE COALESCE(json_extract(NULLIF(trace_info,''),'$.source'),'explicit') END)
 FROM request_logs WHERE trace_id IN (SELECT trace_id FROM trace_summary_dirty) GROUP BY trace_id
 ON CONFLICT(trace_id) DO UPDATE SET request_count=excluded.request_count,first_time=excluded.first_time,last_time=excluded.last_time,total_latency=excluded.total_latency,errors=excluded.errors,input_tokens=excluded.input_tokens,output_tokens=excluded.output_tokens,total_tokens=excluded.total_tokens,upstreams=excluded.upstreams,tags=excluded.tags,sources=excluded.sources`); err != nil {
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
	args := []any{}
	add := func(clause string, values ...any) {
		conditions = append(conditions, clause)
		args = append(args, values...)
	}
	if filter.TraceID != "" {
		add("s.trace_id LIKE ?", "%"+filter.TraceID+"%")
	}
	if filter.Query != "" {
		add("(s.trace_id LIKE ? OR COALESCE(json_extract(m.document,'$.name'),'') LIKE ? OR COALESCE(json_extract(m.document,'$.notes'),'') LIKE ?)", "%"+filter.Query+"%", "%"+filter.Query+"%", "%"+filter.Query+"%")
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
	rows, err := r.db.Query(`SELECT s.trace_id,s.request_count,s.first_time,s.last_time,s.total_latency,s.errors,s.input_tokens,s.output_tokens,s.total_tokens,s.upstreams,s.tags,s.sources,COALESCE(m.document,'')`+from+" ORDER BY s.last_time DESC,s.trace_id LIMIT ? OFFSET ?", append(args, limit, max(0, filter.Offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []TraceSummary{}
	for rows.Next() {
		var s TraceSummary
		var in, out, totalTokens sql.NullInt64
		var upstreams, tags, sources, metadata string
		if err = rows.Scan(&s.TraceID, &s.RequestCount, &s.FirstTime, &s.LastTime, &s.TotalLatency, &s.ErrorCount, &in, &out, &totalTokens, &upstreams, &tags, &sources, &metadata); err != nil {
			return nil, 0, err
		}
		s.UsageInputTokens = nullIntPtr(in)
		s.UsageOutputTokens = nullIntPtr(out)
		s.UsageTotalTokens = nullIntPtr(totalTokens)
		_ = json.Unmarshal([]byte(upstreams), &s.Upstreams)
		_ = json.Unmarshal([]byte(tags), &s.Tags)
		_ = json.Unmarshal([]byte(sources), &s.Sources)
		if metadata != "" {
			if err = json.Unmarshal([]byte(metadata), &s.Metadata); err != nil {
				return nil, 0, fmt.Errorf("trace metadata: %w", err)
			}
		}
		items = append(items, s)
	}
	return items, total, rows.Err()
}
