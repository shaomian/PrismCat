package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/benchmark"
)

// No raw log/body copies: a snapshot stores ordered membership only.
type ReviewSnapshot struct {
	ID        string
	CreatedAt time.Time
	Tasks     []benchmark.ReviewTask
}

type ReviewSnapshotRequest struct {
	Position int
	Missing  bool
	Log      *RequestLog
}

type ReviewSnapshotRepository interface {
	PrepareReviewSnapshot(context.Context, string, int64, []benchmark.ReviewTask) (ReviewSnapshot, error)
	DiscardReviewSnapshot(string) error
	ReviewSnapshotRequests(context.Context, string, string, int, string, bool, int) ([]ReviewSnapshotRequest, error)
	ReviewSnapshotSamples(context.Context, string, string, []int) (map[int]string, error)
	ReviewSnapshotMembers(context.Context, string, string, []string) (map[string]bool, error)
	ReviewSnapshotOverlap(context.Context, string) (bool, error)
}

// The guard also supports older catalog fixtures/documents that are not JSON.
const reportSnapshotExpr = `COALESCE(CASE WHEN json_valid(document) THEN json_extract(document,'$.snapshot_id') END,'')`

func (r *SQLiteRepository) migrateReviewSnapshots() error {
	_, err := r.db.Exec(`
 CREATE TABLE IF NOT EXISTS review_snapshots(id TEXT PRIMARY KEY,experiment_id TEXT NOT NULL,state TEXT NOT NULL,created_at INTEGER NOT NULL,run_ids TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_review_snapshots_card ON review_snapshots(experiment_id);
 CREATE TABLE IF NOT EXISTS review_snapshot_logs(snapshot_id TEXT NOT NULL,run_id TEXT NOT NULL,position INTEGER NOT NULL,log_id TEXT NOT NULL,PRIMARY KEY(snapshot_id,run_id,position),UNIQUE(snapshot_id,run_id,log_id));
 CREATE INDEX IF NOT EXISTS idx_review_snapshot_log ON review_snapshot_logs(log_id,snapshot_id);
 CREATE INDEX IF NOT EXISTS idx_review_reports_snapshot ON review_reports((` + reportSnapshotExpr + `));
 CREATE TRIGGER IF NOT EXISTS cleanup_review_snapshot AFTER DELETE ON review_reports WHEN json_valid(OLD.document) BEGIN
 DELETE FROM review_snapshot_logs WHERE snapshot_id=json_extract(OLD.document,'$.snapshot_id') AND NOT EXISTS(SELECT 1 FROM review_reports WHERE ` + reportSnapshotExpr + `=json_extract(OLD.document,'$.snapshot_id'));
 DELETE FROM review_snapshots WHERE id=json_extract(OLD.document,'$.snapshot_id') AND NOT EXISTS(SELECT 1 FROM review_reports WHERE ` + reportSnapshotExpr + `=json_extract(OLD.document,'$.snapshot_id'));
 END;
 DELETE FROM review_snapshot_logs WHERE snapshot_id IN (SELECT id FROM review_snapshots WHERE state!='ready' OR NOT EXISTS(SELECT 1 FROM review_reports WHERE ` + reportSnapshotExpr + `=review_snapshots.id));
 DELETE FROM review_snapshots WHERE state!='ready' OR NOT EXISTS(SELECT 1 FROM review_reports WHERE ` + reportSnapshotExpr + `=review_snapshots.id);
 `)
	return err
}

func (r *SQLiteRepository) DiscardReviewSnapshot(id string) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`DELETE FROM review_snapshot_logs WHERE snapshot_id=? AND NOT EXISTS(SELECT 1 FROM review_reports WHERE `+reportSnapshotExpr+`=?)`, id, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM review_snapshots WHERE id=? AND NOT EXISTS(SELECT 1 FROM review_reports WHERE `+reportSnapshotExpr+`=?)`, id, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) createSnapshotReport(v benchmark.ReviewReport) error {
	// New reports keep one directory reference, never a parallel JSON ID list.
	for _, task := range v.Tasks {
		if len(task.Metrics.LogIDs) > 0 {
			return benchmark.ErrInvalid
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE review_snapshots SET state=state WHERE id=?", v.SnapshotID); err != nil {
		return err
	}
	var owner, state, runIDs string
	if err = tx.QueryRow("SELECT experiment_id,state,run_ids FROM review_snapshots WHERE id=?", v.SnapshotID).Scan(&owner, &state, &runIDs); err != nil {
		return err
	}
	if owner != v.ExperimentID || (state != "prepared" && state != "ready") {
		return ErrCorrelationConflict
	}
	var exists int
	if err = tx.QueryRow("SELECT COUNT(*) FROM experiments WHERE id=?", owner).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	var ids []string
	if err = json.Unmarshal([]byte(runIDs), &ids); err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		allowed[id] = true
	}
	if len(v.Tasks) != len(ids) {
		return ErrCorrelationConflict
	}
	for _, task := range v.Tasks {
		if !allowed[task.RunID] {
			return ErrCorrelationConflict
		}
		delete(allowed, task.RunID)
		if v.ParentID == "" {
			if err = tx.QueryRow("SELECT COUNT(*) FROM experiment_runs WHERE id=? AND experiment_id=?", task.RunID, owner).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return ErrCorrelationConflict
			}
		}
	}
	if v.ParentID == "" {
		var missing bool
		if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM review_snapshot_logs m LEFT JOIN request_logs l ON l.id=m.log_id WHERE m.snapshot_id=? AND (l.id IS NULL OR EXISTS(SELECT 1 FROM deleted_log_ids d WHERE d.id=m.log_id)))`, v.SnapshotID).Scan(&missing); err != nil {
			return err
		}
		if missing {
			return ErrCorrelationConflict
		}
	}
	if _, err = tx.Exec("INSERT INTO review_reports VALUES(?,?,?,?,?,?,?,?)", v.ID, v.ExperimentID, v.Status, v.Model, v.Question, v.ParentID, v.CreatedAt.UnixMilli(), string(raw)); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrCorrelationConflict
		}
		return err
	}
	if _, err = tx.Exec("UPDATE review_snapshots SET state='ready' WHERE id=?", v.SnapshotID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) PrepareReviewSnapshot(ctx context.Context, card string, revision int64, tasks []benchmark.ReviewTask) (snapshot ReviewSnapshot, err error) {
	if len(tasks) == 0 || len(tasks) > 8 {
		return snapshot, benchmark.ErrInvalid
	}
	runs := make([]string, 0, len(tasks))
	seen := map[string]bool{}
	for _, task := range tasks {
		if task.RunID == "" || seen[task.RunID] {
			return snapshot, benchmark.ErrInvalid
		}
		seen[task.RunID] = true
		runs = append(runs, task.RunID)
	}
	rawRuns, _ := json.Marshal(runs)
	snapshot.ID = uuid.NewString()
	r.writeMu.Lock()
	_, err = r.db.ExecContext(ctx, `INSERT INTO review_snapshots VALUES(?,?,'building',?,?)`, snapshot.ID, card, time.Now().UnixMilli(), string(rawRuns))
	r.writeMu.Unlock()
	if err != nil {
		return snapshot, err
	}
	complete := false
	defer func() {
		if !complete {
			if cleanupErr := r.DiscardReviewSnapshot(snapshot.ID); cleanupErr != nil {
				log.Printf("review snapshot cleanup failed: %v", cleanupErr)
			}
		}
	}()
	reader, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return snapshot, err
	}
	defer reader.Rollback()
	var cardRevision int64
	// This first read fixes one SQLite/WAL view for every selected task.
	if err = reader.QueryRowContext(ctx, "SELECT revision FROM experiments WHERE id=?", card).Scan(&cardRevision); err != nil {
		return snapshot, err
	}
	if cardRevision != revision {
		return snapshot, ErrCorrelationConflict
	}
	snapshot.CreatedAt = time.Now().UTC()
	snapshot.Tasks = append([]benchmark.ReviewTask(nil), tasks...)
	type member struct {
		Run      string `json:"run"`
		Position int    `json:"position"`
		ID       string `json:"id"`
	}
	batch := make([]member, 0, 1000)
	flush := func() error {
		if len(batch) == 0 {
			return ctx.Err()
		}
		raw, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		r.writeMu.Lock()
		defer r.writeMu.Unlock()
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// Reservation and validation share the batch transaction: a removed log
		// cannot be silently accepted from the older read view.
		if _, err = tx.ExecContext(ctx, "UPDATE review_snapshots SET state=state WHERE id=?", snapshot.ID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO review_snapshot_logs(snapshot_id,run_id,position,log_id)
		 SELECT ?,json_extract(v.value,'$.run'),json_extract(v.value,'$.position'),l.id FROM json_each(?) v JOIN request_logs l ON l.id=json_extract(v.value,'$.id')
		 WHERE NOT EXISTS(SELECT 1 FROM deleted_log_ids d WHERE d.id=l.id)`, snapshot.ID, string(raw))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != int64(len(batch)) {
			return ErrCorrelationConflict
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	for i := range snapshot.Tasks {
		task := &snapshot.Tasks[i]
		var runRevision int64
		if err = reader.QueryRowContext(ctx, "SELECT revision FROM experiment_runs WHERE id=? AND experiment_id=?", task.RunID, card).Scan(&runRevision); err != nil {
			return snapshot, err
		}
		if runRevision != task.Revision {
			return snapshot, ErrCorrelationConflict
		}
		accumulator := benchmark.NewSummaryAccumulator()
		err = walkExperimentSamples(ctx, reader, task.RunID, func(sample benchmark.Sample) (bool, error) {
			accumulator.Add(sample)
			batch = append(batch, member{task.RunID, accumulator.Count(), sample.ID})
			if len(batch) == cap(batch) {
				return true, flush()
			}
			return true, nil
		})
		if err != nil {
			return snapshot, err
		}
		task.Metrics = accumulator.Finish(false)
	}
	if err = flush(); err != nil {
		return snapshot, err
	}
	// Release the old read view before publishing; long reads must not pin WAL
	// during the model request. No write is ever attempted through reader.
	if err = reader.Rollback(); err != nil {
		return snapshot, err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return snapshot, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE review_snapshots SET state=state WHERE id=?", snapshot.ID); err != nil {
		return snapshot, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM experiments WHERE id=?", card).Scan(&cardRevision); err != nil {
		return snapshot, err
	}
	if cardRevision != revision {
		return snapshot, ErrCorrelationConflict
	}
	for _, task := range tasks {
		var current int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM experiment_runs WHERE id=? AND experiment_id=?", task.RunID, card).Scan(&current); err != nil {
			return snapshot, err
		}
		if current != task.Revision {
			return snapshot, ErrCorrelationConflict
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE review_snapshots SET state='prepared',created_at=? WHERE id=?", snapshot.CreatedAt.UnixMilli(), snapshot.ID); err != nil {
		return snapshot, err
	}
	if err = tx.Commit(); err != nil {
		return snapshot, err
	}
	complete = true
	return snapshot, nil
}

func (r *SQLiteRepository) ReviewSnapshotMembers(ctx context.Context, id, run string, ids []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(ids) == 0 {
		return result, ctx.Err()
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	query := `SELECT m.log_id FROM review_snapshot_logs m JOIN review_snapshots s ON s.id=m.snapshot_id WHERE s.state IN ('prepared','ready') AND m.snapshot_id=? AND m.log_id IN (SELECT value FROM json_each(?))`
	args := []any{id, string(raw)}
	if run != "" {
		query += " AND m.run_id=?"
		args = append(args, run)
	}
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var logID string
		if err := rows.Scan(&logID); err != nil {
			return nil, err
		}
		result[logID] = true
	}
	return result, rows.Err()
}

func (r *SQLiteRepository) ReviewSnapshotSamples(ctx context.Context, id, run string, positions []int) (map[int]string, error) {
	result := map[int]string{}
	if len(positions) == 0 {
		return result, ctx.Err()
	}
	raw, _ := json.Marshal(positions)
	rows, err := r.db.QueryContext(ctx, `SELECT m.position,m.log_id FROM review_snapshot_logs m JOIN review_snapshots s ON s.id=m.snapshot_id WHERE s.state IN ('prepared','ready') AND m.snapshot_id=? AND m.run_id=? AND m.position IN (SELECT value FROM json_each(?))`, id, run, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p int
		var logID string
		if err := rows.Scan(&p, &logID); err != nil {
			return nil, err
		}
		result[p] = logID
	}
	return result, rows.Err()
}

func (r *SQLiteRepository) ReviewSnapshotOverlap(ctx context.Context, id string) (bool, error) {
	var overlap bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_snapshot_logs m JOIN review_snapshots s ON s.id=m.snapshot_id WHERE s.state IN ('prepared','ready') AND m.snapshot_id=? GROUP BY m.log_id HAVING COUNT(*)>1)`, id).Scan(&overlap)
	return overlap, err
}

func (r *SQLiteRepository) ReviewSnapshotRequests(ctx context.Context, id, run string, cursor int, query string, errorsOnly bool, limit int) ([]ReviewSnapshotRequest, error) {
	limit = min(max(limit, 1), 200)
	conditions := []string{"m.snapshot_id=?", "m.run_id=?", "m.position>?", "s.state IN ('prepared','ready')"}
	args := []any{id, run, max(cursor, 0)}
	if errorsOnly {
		conditions = append(conditions, "(l.status_code>=400 OR COALESCE(l.error,'')!='')")
	}
	if query != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
		conditions = append(conditions, `(l.path LIKE ? ESCAPE '\' OR COALESCE(l.error,'') LIKE ? ESCAPE '\' OR SUBSTR(COALESCE(l.request_body,''),1,4096) LIKE ? ESCAPE '\' OR SUBSTR(COALESCE(l.response_body,''),1,4096) LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT m.position,m.log_id,l.id IS NULL,l.created_at,COALESCE(l.method,''),COALESCE(l.path,''),COALESCE(l.status_code,0),COALESCE(l.latency_ms,0),COALESCE(l.error,''),COALESCE(l.trace_id,''),SUBSTR(COALESCE(l.request_body,''),1,4096),SUBSTR(COALESCE(l.response_body,''),1,4096)
	 FROM review_snapshot_logs m JOIN review_snapshots s ON s.id=m.snapshot_id LEFT JOIN request_logs l ON l.id=m.log_id WHERE `+strings.Join(conditions, " AND ")+` ORDER BY m.position LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []ReviewSnapshotRequest{}
	for rows.Next() {
		row := ReviewSnapshotRequest{Log: &RequestLog{}}
		var at sql.NullTime
		if err := rows.Scan(&row.Position, &row.Log.ID, &row.Missing, &at, &row.Log.Method, &row.Log.Path, &row.Log.StatusCode, &row.Log.Latency, &row.Log.Error, &row.Log.TraceID, &row.Log.RequestBody, &row.Log.ResponseBody); err != nil {
			return nil, err
		}
		if at.Valid {
			row.Log.CreatedAt = at.Time.UTC()
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

var _ ReviewSnapshotRepository = (*SQLiteRepository)(nil)
