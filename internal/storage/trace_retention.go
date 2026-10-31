package storage

import "time"

func (r *SQLiteRepository) migrateTraceRetention() error {
	_, err := r.db.Exec(`DROP VIEW IF EXISTS trace_retention_groups; CREATE VIEW trace_retention_groups AS
 SELECT CASE WHEN COALESCE(l.trace_id,'')='' THEN 'log:'||l.id ELSE 'trace:'||l.trace_id END AS group_key,
 MIN(l.created_at_unix_ms) AS first_time,MAX(l.created_at_unix_ms) AS last_time,COUNT(*) AS log_count
 FROM request_logs l
 GROUP BY group_key
 HAVING SUM(CASE WHEN
 EXISTS(SELECT 1 FROM log_annotations a WHERE a.log_id=l.id AND a.saved=1)
 OR EXISTS(SELECT 1 FROM trace_metadata m WHERE m.trace_id=l.trace_id AND json_extract(m.document,'$.pinned')=1)
 OR EXISTS(SELECT 1 FROM experiment_runs er JOIN experiments e ON e.id=er.experiment_id WHERE json_extract(e.document,'$.keep')=1 AND ((er.capture_run_id!='' AND er.capture_run_id=l.capture_run_id) OR EXISTS(SELECT 1 FROM experiment_run_traces et WHERE et.run_id=er.id AND et.trace_id=l.trace_id AND NOT EXISTS(SELECT 1 FROM experiment_trace_snapshots s WHERE s.run_id=et.run_id AND s.trace_id=et.trace_id)) OR EXISTS(SELECT 1 FROM experiment_trace_samples s WHERE s.run_id=er.id AND s.log_id=l.id)))
 OR EXISTS(SELECT 1 FROM review_card_logs cl JOIN experiments ce ON ce.id=cl.experiment_id WHERE cl.log_id=l.id AND json_extract(ce.document,'$.keep')=1)
 OR EXISTS(SELECT 1 FROM review_card_logs cl JOIN review_reports rr ON rr.experiment_id=cl.experiment_id WHERE cl.log_id=l.id AND rr.status='running')
 OR EXISTS(SELECT 1 FROM review_snapshot_logs sl JOIN review_snapshots ss ON ss.id=sl.snapshot_id JOIN experiments se ON se.id=ss.experiment_id WHERE sl.log_id=l.id AND (ss.state IN ('building','prepared') OR json_extract(se.document,'$.keep')=1 OR EXISTS(SELECT 1 FROM review_reports WHERE experiment_id=ss.experiment_id AND status='running' AND ` + reportSnapshotExpr + `=ss.id)))
 OR EXISTS(SELECT 1 FROM capture_runs c WHERE c.id=l.capture_run_id AND c.stopped_at IS NULL)
 OR ((l.status_code=0 AND COALESCE(l.error,'')='') AND l.created_at_unix_ms>(CAST(strftime('%s','now') AS INTEGER)*1000-86400000))
 THEN 1 ELSE 0 END)=0`)
	return err
}

// Delete whole eligible groups. A single Trace can exceed the requested row
// budget; splitting it would destroy evidence. Standalone logs remain individual.
func (r *SQLiteRepository) deleteTraceGroups(before *time.Time, count int64) (int64, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	var total int64
	for count > 0 || before != nil {
		tx, err := r.db.Begin()
		if err != nil {
			return total, err
		}
		if _, err = tx.Exec("UPDATE trace_metadata SET revision=revision WHERE trace_id='' "); err != nil {
			tx.Rollback()
			return total, err
		}
		selection := "SELECT group_key FROM trace_retention_groups WHERE last_time<? ORDER BY first_time,group_key LIMIT 100"
		args := []any{}
		if before != nil {
			args = append(args, before.UnixMilli())
		} else {
			selection = "SELECT group_key FROM (SELECT group_key,log_count,SUM(log_count) OVER(ORDER BY first_time,group_key)-log_count AS preceding FROM trace_retention_groups) WHERE preceding<? LIMIT 100"
			args = append(args, min(count, 5000))
		}
		result, err := tx.Exec("DELETE FROM request_logs WHERE (CASE WHEN COALESCE(trace_id,'')='' THEN 'log:'||id ELSE 'trace:'||trace_id END) IN ("+selection+")", args...)
		if err != nil {
			tx.Rollback()
			return total, err
		}
		if _, err = tx.Exec("DELETE FROM log_annotations WHERE log_id NOT IN (SELECT id FROM request_logs)"); err != nil {
			tx.Rollback()
			return total, err
		}
		if err = tx.Commit(); err != nil {
			return total, err
		}
		n, _ := result.RowsAffected()
		total += n
		count -= n
		if n == 0 {
			break
		}
	}
	return total, nil
}
