package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/trace"
)

var ErrCorrelationConflict = errors.New("trace assignment conflicts with explicit or locked assignment")

type CaptureRun struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Upstream     string     `json:"upstream"`
	StartedAt    time.Time  `json:"started_at"`
	StoppedAt    *time.Time `json:"stopped_at,omitempty"`
	SingleTask   bool       `json:"single_task"`
	RequestCount int64      `json:"request_count"`
	TotalLatency int64      `json:"total_latency_ms"`
	WallTime     int64      `json:"wall_time_ms"`
	UsageInput   *int64     `json:"usage_input_tokens,omitempty"`
	UsageOutput  *int64     `json:"usage_output_tokens,omitempty"`
	UsageMissing int64      `json:"usage_missing_count"`
	Archived     bool       `json:"archived"`
	Revision     int64      `json:"revision"`
}

// CorrelationRepository is optional so existing integrations remain compatible.
type CorrelationRepository interface {
	ListCaptureRuns() ([]CaptureRun, error)
	StartCaptureRun(name, upstream string, singleTask bool) (CaptureRun, error)
	StopCaptureRun(id string) error
	CaptureRunForUpstream(upstream string) string
	AssignTrace(logID, traceID string, revision int64) (trace.Assignment, error)
}

type captureRegistry struct {
	mu     sync.RWMutex
	active map[string]CaptureRun
}

func (r *SQLiteRepository) migrateCorrelation() error {
	has, err := r.hasColumn("request_logs", "explicit_trace_id")
	if err != nil {
		return err
	}
	for _, col := range []string{"explicit_trace_id", "trace_info", "capture_run_id", "websocket_info"} {
		if err := r.ensureLogColumn(col, col+" TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	if !has {
		if _, err := r.db.Exec("UPDATE request_logs SET explicit_trace_id = COALESCE(trace_id, '')"); err != nil {
			return err
		}
	}
	_, err = r.db.Exec(`
 CREATE TABLE IF NOT EXISTS trace_assignments(log_id TEXT PRIMARY KEY, trace_id TEXT NOT NULL, trace_seq INTEGER NOT NULL, parent_log_id TEXT NOT NULL DEFAULT '', info TEXT NOT NULL, locked INTEGER NOT NULL DEFAULT 0, features TEXT NOT NULL DEFAULT '', updated_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS trace_revisions(trace_id TEXT PRIMARY KEY, revision INTEGER NOT NULL DEFAULT 0, next_seq INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS trace_assignment_history(id INTEGER PRIMARY KEY AUTOINCREMENT, log_id TEXT NOT NULL, old_info TEXT NOT NULL, new_info TEXT NOT NULL, changed_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS capture_runs(id TEXT PRIMARY KEY, name TEXT NOT NULL, upstream TEXT NOT NULL, started_at INTEGER NOT NULL, stopped_at INTEGER, single_task INTEGER NOT NULL DEFAULT 0);
 CREATE UNIQUE INDEX IF NOT EXISTS idx_capture_active ON capture_runs(upstream) WHERE stopped_at IS NULL;
 CREATE INDEX IF NOT EXISTS idx_logs_capture ON request_logs(capture_run_id);
 CREATE INDEX IF NOT EXISTS idx_assignment_updated ON trace_assignments(updated_at);
 CREATE TRIGGER IF NOT EXISTS restore_trace_assignment AFTER UPDATE OF trace_id,parent_log_id,trace_seq ON request_logs
 WHEN EXISTS(SELECT 1 FROM trace_assignments a WHERE a.log_id=NEW.id AND (NEW.explicit_trace_id='' OR a.trace_id=NEW.explicit_trace_id) AND (a.trace_id != COALESCE(NEW.trace_id,'') OR a.trace_seq != COALESCE(NEW.trace_seq,0) OR a.parent_log_id != COALESCE(NEW.parent_log_id,'')))
 BEGIN
 UPDATE request_logs SET trace_id=(SELECT trace_id FROM trace_assignments WHERE log_id=NEW.id), trace_seq=(SELECT trace_seq FROM trace_assignments WHERE log_id=NEW.id), parent_log_id=(SELECT parent_log_id FROM trace_assignments WHERE log_id=NEW.id), trace_info=(SELECT info FROM trace_assignments WHERE log_id=NEW.id) WHERE id=NEW.id;
 END;
 CREATE TRIGGER IF NOT EXISTS trace_revision_insert AFTER INSERT ON request_logs WHEN COALESCE(NEW.trace_id,'') != '' BEGIN
 INSERT INTO trace_revisions(trace_id,revision) VALUES(NEW.trace_id,1) ON CONFLICT(trace_id) DO UPDATE SET revision=revision+1;
 END;
 CREATE TRIGGER IF NOT EXISTS trace_revision_update AFTER UPDATE ON request_logs BEGIN
 INSERT INTO trace_revisions(trace_id,revision) SELECT NEW.trace_id,1 WHERE COALESCE(NEW.trace_id,'') != '' ON CONFLICT(trace_id) DO UPDATE SET revision=revision+1;
 INSERT INTO trace_revisions(trace_id,revision) SELECT OLD.trace_id,1 WHERE COALESCE(OLD.trace_id,'') != '' AND OLD.trace_id != COALESCE(NEW.trace_id,'') ON CONFLICT(trace_id) DO UPDATE SET revision=revision+1;
 END;
 CREATE TRIGGER IF NOT EXISTS trace_cleanup AFTER DELETE ON request_logs BEGIN
 INSERT INTO trace_revisions(trace_id,revision) SELECT OLD.trace_id,1 WHERE COALESCE(OLD.trace_id,'') != '' ON CONFLICT(trace_id) DO UPDATE SET revision=revision+1;
 DELETE FROM trace_assignments WHERE log_id=OLD.id;
 DELETE FROM trace_assignment_history WHERE log_id=OLD.id;
 END;`)
	if err != nil {
		return fmt.Errorf("correlation migration: %w", err)
	}
	r.captures.active = map[string]CaptureRun{}
	rows, err := r.db.Query("SELECT id,name,upstream,started_at,single_task FROM capture_runs WHERE stopped_at IS NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var run CaptureRun
		var ms int64
		if err := rows.Scan(&run.ID, &run.Name, &run.Upstream, &ms, &run.SingleTask); err != nil {
			return err
		}
		run.StartedAt = time.UnixMilli(ms).UTC()
		r.captures.active[run.Upstream] = run
	}
	return rows.Err()
}

func (r *SQLiteRepository) CaptureRunForUpstream(upstream string) string {
	r.captures.mu.RLock()
	defer r.captures.mu.RUnlock()
	return r.captures.active[upstream].ID
}

// ActiveCaptureRuns reads the in-memory registry without aggregating request logs.
func (r *SQLiteRepository) ActiveCaptureRuns() []CaptureRun {
	r.captures.mu.RLock()
	defer r.captures.mu.RUnlock()
	runs := make([]CaptureRun, 0, len(r.captures.active))
	for _, run := range r.captures.active {
		runs = append(runs, run)
	}
	return runs
}

func (r *SQLiteRepository) StartCaptureRun(name, upstream string, singleTask bool) (CaptureRun, error) {
	run := CaptureRun{ID: "run-" + uuid.NewString(), Name: strings.TrimSpace(name), Upstream: upstream, StartedAt: time.Now().UTC(), SingleTask: singleTask}
	if run.Name == "" || len(run.Name) > 200 || upstream == "" {
		return CaptureRun{}, errors.New("name and upstream are required (name max 200 bytes)")
	}
	r.captures.mu.Lock()
	defer r.captures.mu.Unlock()
	if _, ok := r.captures.active[upstream]; ok {
		return CaptureRun{}, errors.New("this upstream already has an active capture")
	}
	if _, err := r.db.Exec("INSERT INTO capture_runs(id,name,upstream,started_at,single_task) VALUES(?,?,?,?,?)", run.ID, run.Name, run.Upstream, run.StartedAt.UnixMilli(), run.SingleTask); err != nil {
		return CaptureRun{}, err
	}
	r.captures.active[upstream] = run
	return run, nil
}

func (r *SQLiteRepository) StopCaptureRun(id string) error {
	r.captures.mu.Lock()
	defer r.captures.mu.Unlock()
	result, err := r.db.Exec("UPDATE capture_runs SET stopped_at=?,revision=revision+1 WHERE id=? AND stopped_at IS NULL", time.Now().UnixMilli(), id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	for upstream, run := range r.captures.active {
		if run.ID == id {
			delete(r.captures.active, upstream)
		}
	}
	return nil
}

func (r *SQLiteRepository) ListCaptureRuns() ([]CaptureRun, error) {
	runs, _, err := r.ListCaptureRunsPage(100, 0)
	return runs, err
}

func (r *SQLiteRepository) ListCaptureRunsPage(limit, offset int) ([]CaptureRun, int, error) {
	return r.listCaptureRunsPage(limit, offset, nil)
}
func (r *SQLiteRepository) ListCaptureRunsPageArchived(limit, offset int, archived bool) ([]CaptureRun, int, error) {
	return r.listCaptureRunsPage(limit, offset, &archived)
}
func (r *SQLiteRepository) listCaptureRunsPage(limit, offset int, archived *bool) ([]CaptureRun, int, error) {
	if limit < 1 || limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	where := ""
	args := []any{}
	if archived != nil {
		where = " WHERE archived=?"
		args = append(args, *archived)
	}
	var total int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM capture_runs"+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// Select the page before joining logs so old batches do not add aggregation work.
	rows, err := r.db.Query(`WITH page AS MATERIALIZED (
		SELECT * FROM capture_runs`+where+` ORDER BY (stopped_at IS NULL) DESC, started_at DESC, id DESC LIMIT ? OFFSET ?
	) SELECT c.id,c.name,c.upstream,c.started_at,c.stopped_at,c.single_task,c.archived,c.revision,COUNT(l.id),COALESCE(SUM(l.latency_ms),0),COALESCE(MAX(l.created_at_unix_ms+l.latency_ms)-MIN(l.created_at_unix_ms),0),SUM(l.usage_input_tokens),SUM(l.usage_output_tokens),COALESCE(SUM(CASE WHEN l.id IS NOT NULL AND (l.usage_input_tokens IS NULL OR l.usage_output_tokens IS NULL) THEN 1 ELSE 0 END),0)
	FROM page c LEFT JOIN request_logs l ON l.capture_run_id=c.id GROUP BY c.id
	ORDER BY (c.stopped_at IS NULL) DESC, c.started_at DESC, c.id DESC`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	runs := []CaptureRun{}
	for rows.Next() {
		var run CaptureRun
		var start int64
		var stop, input, output sql.NullInt64
		if err := rows.Scan(&run.ID, &run.Name, &run.Upstream, &start, &stop, &run.SingleTask, &run.Archived, &run.Revision, &run.RequestCount, &run.TotalLatency, &run.WallTime, &input, &output, &run.UsageMissing); err != nil {
			return nil, 0, err
		}
		run.StartedAt = time.UnixMilli(start).UTC()
		if stop.Valid {
			t := time.UnixMilli(stop.Int64).UTC()
			run.StoppedAt = &t
		}
		run.UsageInput = nullIntPtr(input)
		run.UsageOutput = nullIntPtr(output)
		runs = append(runs, run)
	}
	return runs, total, rows.Err()
}

func (r *SQLiteRepository) captureAssignment(id string) (trace.Assignment, bool) {
	var single bool
	err := r.db.QueryRow("SELECT single_task FROM capture_runs WHERE id=?", id).Scan(&single)
	return trace.Assignment{TraceID: "capture-" + id, SessionID: "session-" + id, Source: "capture", Reason: "capture_single_task", State: "declared"}, err == nil && single
}

func (r *SQLiteRepository) storeObservation(o trace.Observation, manual bool) (trace.Assignment, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return trace.Assignment{}, err
	}
	defer tx.Rollback()
	// Acquire SQLite's write reservation before reading the assignment revision.
	if _, err := tx.Exec("UPDATE trace_revisions SET revision=revision WHERE trace_id=''"); err != nil {
		return trace.Assignment{}, err
	}
	var explicit string
	if err := tx.QueryRow("SELECT explicit_trace_id FROM request_logs WHERE id=?", o.LogID).Scan(&explicit); err != nil {
		return trace.Assignment{}, err
	}
	var old trace.Assignment
	var oldJSON, oldFeatures string
	var seq int
	var locked bool
	err = tx.QueryRow("SELECT info,trace_seq,locked,features FROM trace_assignments WHERE log_id=?", o.LogID).Scan(&oldJSON, &seq, &locked, &oldFeatures)
	if err != nil && err != sql.ErrNoRows {
		return trace.Assignment{}, err
	}
	_ = json.Unmarshal([]byte(oldJSON), &old)
	if locked && !manual {
		o.Assignment = old
	}
	if explicit != "" && o.Assignment.Source != "explicit" && !(locked && !manual) {
		return trace.Assignment{}, ErrCorrelationConflict
	}
	if o.Assignment.Source != "explicit" {
		o.Assignment.TraceID, err = resolveTrace(context.Background(), tx, o.Assignment.TraceID)
		if err != nil {
			return trace.Assignment{}, err
		}
	}
	if manual && old.Revision != o.Assignment.Revision {
		return trace.Assignment{}, ErrCorrelationConflict
	}
	if manual {
		if err = freezeTraceSamples(context.Background(), tx, []string{old.TraceID, o.Assignment.TraceID}); err != nil {
			return trace.Assignment{}, err
		}
		o.Assignment.Locked = true
	}
	if old.TraceID != o.Assignment.TraceID || seq == 0 {
		_, err = tx.Exec(`INSERT INTO trace_revisions(trace_id,revision,next_seq) VALUES(?,0,0) ON CONFLICT(trace_id) DO NOTHING`, o.Assignment.TraceID)
		if err != nil {
			return trace.Assignment{}, err
		}
		if _, err = tx.Exec(`UPDATE trace_revisions SET next_seq=MAX(next_seq,COALESCE((SELECT MAX(trace_seq) FROM request_logs WHERE trace_id=? AND id!=?),0))+1 WHERE trace_id=?`, o.Assignment.TraceID, o.LogID, o.Assignment.TraceID); err != nil {
			return trace.Assignment{}, err
		}
		if err = tx.QueryRow("SELECT next_seq FROM trace_revisions WHERE trace_id=?", o.Assignment.TraceID).Scan(&seq); err != nil {
			return trace.Assignment{}, err
		}
	}
	o.Assignment.Revision = old.Revision
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(o.Assignment)
	if string(before) != string(after) {
		o.Assignment.Revision++
	}
	info, _ := json.Marshal(o.Assignment)
	o.Assignment.Revision = 0
	if !o.Request.Valid && oldFeatures != "" {
		var previous trace.Observation
		if json.Unmarshal([]byte(oldFeatures), &previous) == nil {
			o.Request = previous.Request
			o.Response = previous.Response
		}
	}
	features, _ := json.Marshal(o) // assignment is restored from info on load
	var parent string
	if len(o.Assignment.Parents) == 1 {
		parent = o.Assignment.Parents[0]
	}
	if _, err = tx.Exec(`INSERT INTO trace_assignments(log_id,trace_id,trace_seq,parent_log_id,info,locked,features,updated_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(log_id) DO UPDATE SET trace_id=excluded.trace_id,trace_seq=excluded.trace_seq,parent_log_id=excluded.parent_log_id,info=excluded.info,locked=excluded.locked,features=CASE WHEN excluded.features='' THEN trace_assignments.features ELSE excluded.features END,updated_at=excluded.updated_at`, o.LogID, o.Assignment.TraceID, seq, parent, string(info), o.Assignment.Locked, string(features), time.Now().UnixMilli()); err != nil {
		return trace.Assignment{}, err
	}
	if _, err = tx.Exec("UPDATE request_logs SET trace_id=?,trace_seq=?,parent_log_id=?,trace_info=? WHERE id=?", o.Assignment.TraceID, seq, parent, string(info), o.LogID); err != nil {
		return trace.Assignment{}, err
	}
	if manual {
		if _, err = tx.Exec("INSERT INTO trace_assignment_history(log_id,old_info,new_info,changed_at) VALUES(?,?,?,?)", o.LogID, oldJSON, string(info), time.Now().UnixMilli()); err != nil {
			return trace.Assignment{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return trace.Assignment{}, err
	}
	var result trace.Assignment
	_ = json.Unmarshal(info, &result)
	return result, nil
}

func (r *SQLiteRepository) AssignTrace(logID, traceID string, revision int64) (trace.Assignment, error) {
	if len(traceID) > 200 {
		return trace.Assignment{}, errors.New("trace ID too long")
	}
	if traceID == "" {
		traceID = "manual-" + uuid.NewString()
	}
	log, err := r.GetLog(logID)
	if err != nil {
		return trace.Assignment{}, err
	}
	a := trace.Assignment{TraceID: traceID, SessionID: "session-" + uuid.NewString(), Source: "manual", Reason: "manual_assignment", State: "declared", Locked: true, Revision: revision}
	if log.TraceAssociation != nil {
		a.SessionID = log.TraceAssociation.SessionID
	}
	return r.storeObservation(trace.Observation{LogID: logID, Scope: log.Upstream, CreatedAt: log.CreatedAt, Assignment: a}, true)
}

func (r *SQLiteRepository) loadObservations() ([]trace.Observation, error) {
	rows, err := r.db.Query("SELECT features,info FROM trace_assignments WHERE updated_at>? AND features!='' ORDER BY updated_at DESC LIMIT 2048", time.Now().Add(-30*time.Minute).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var observations []trace.Observation
	events := 0
	for rows.Next() {
		var features, info string
		if err := rows.Scan(&features, &info); err != nil {
			return nil, err
		}
		var o trace.Observation
		if json.Unmarshal([]byte(features), &o) == nil {
			_ = json.Unmarshal([]byte(info), &o.Assignment)
			events += len(o.Request.Events) + len(o.Response.Events)
			if events > 65536 {
				break
			}
			observations = append(observations, o)
		}
	}
	return observations, rows.Err()
}

// GetTraceMetadata avoids loading body previews during correlation UI polling.
func (r *SQLiteRepository) GetTraceMetadata(id string) (*RequestLog, error) {
	entry := &RequestLog{ID: id}
	var info string
	if err := r.db.QueryRow("SELECT trace_id,trace_seq,parent_log_id,trace_info,capture_run_id FROM request_logs WHERE id=?", id).Scan(&entry.TraceID, &entry.TraceSeq, &entry.ParentLogID, &info, &entry.CaptureRunID); err != nil {
		return nil, err
	}
	decodeTraceInfo(entry, info)
	return entry, nil
}
