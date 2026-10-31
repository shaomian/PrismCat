package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

type TraceGroupingRequest struct {
	Action   string   `json:"action"`
	TraceIDs []string `json:"trace_ids"`
	LogIDs   []string `json:"log_ids,omitempty"`
	Target   string   `json:"target_trace_id,omitempty"`
	Version  string   `json:"version,omitempty"`
}
type TraceGroupingPreview struct {
	Version    string `json:"version"`
	LogCount   int    `json:"log_count"`
	TraceCount int    `json:"trace_count"`
	Target     string `json:"target_trace_id"`
	TargetName string `json:"target_name"`
	CanApply   bool   `json:"can_apply"`
	Reason     string `json:"reason,omitempty"`
}
type TraceGroupingResult struct {
	Target   string `json:"target_trace_id"`
	LogCount int    `json:"log_count"`
}
type TraceGroupingRepository interface {
	PreviewTraceGrouping(context.Context, TraceGroupingRequest) (TraceGroupingPreview, error)
	ApplyTraceGrouping(context.Context, TraceGroupingRequest) (TraceGroupingResult, error)
	ResolveTrace(string) (string, error)
}

func (r *SQLiteRepository) migrateTraceGrouping() error {
	_, err := r.db.Exec(`
 CREATE TABLE IF NOT EXISTS trace_redirects(source TEXT PRIMARY KEY,target TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_trace_redirect_target ON trace_redirects(target);
 CREATE TABLE IF NOT EXISTS trace_group_operations(id TEXT PRIMARY KEY,action TEXT NOT NULL,sources TEXT NOT NULL,target TEXT NOT NULL,log_count INTEGER NOT NULL,created_at INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_trace_operation_origin ON trace_group_operations(target,action,created_at,id);
 CREATE TABLE IF NOT EXISTS experiment_trace_snapshots(run_id TEXT NOT NULL,trace_id TEXT NOT NULL,PRIMARY KEY(run_id,trace_id));
 CREATE TABLE IF NOT EXISTS experiment_trace_samples(run_id TEXT NOT NULL,trace_id TEXT NOT NULL,log_id TEXT NOT NULL,PRIMARY KEY(run_id,trace_id,log_id));
 CREATE INDEX IF NOT EXISTS idx_experiment_sample_log ON experiment_trace_samples(log_id);
 DROP TRIGGER IF EXISTS restore_trace_assignment;
 CREATE TRIGGER restore_trace_assignment AFTER UPDATE OF trace_id,parent_log_id,trace_seq ON request_logs
 WHEN EXISTS(SELECT 1 FROM trace_assignments a WHERE a.log_id=NEW.id AND (a.trace_id != COALESCE(NEW.trace_id,'') OR a.trace_seq != COALESCE(NEW.trace_seq,0) OR a.parent_log_id != COALESCE(NEW.parent_log_id,'')))
 BEGIN
 UPDATE request_logs SET trace_id=(SELECT trace_id FROM trace_assignments WHERE log_id=NEW.id),trace_seq=(SELECT trace_seq FROM trace_assignments WHERE log_id=NEW.id),parent_log_id=(SELECT parent_log_id FROM trace_assignments WHERE log_id=NEW.id),trace_info=(SELECT info FROM trace_assignments WHERE log_id=NEW.id) WHERE id=NEW.id;
 END;
 CREATE TRIGGER IF NOT EXISTS clean_trace_samples AFTER DELETE ON request_logs BEGIN DELETE FROM experiment_trace_samples WHERE log_id=OLD.id; END;
 CREATE TRIGGER IF NOT EXISTS clean_run_samples AFTER DELETE ON experiment_runs BEGIN
 DELETE FROM experiment_trace_samples WHERE run_id=OLD.id; DELETE FROM experiment_trace_snapshots WHERE run_id=OLD.id; END;
 `)
	return err
}

type traceQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveTrace(ctx context.Context, q traceQuery, id string) (string, error) {
	for i := 0; i < 32; i++ {
		var target string
		err := q.QueryRowContext(ctx, "SELECT target FROM trace_redirects WHERE source=?", id).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
		id = target
	}
	return "", fmt.Errorf("trace redirect cycle")
}
func (r *SQLiteRepository) ResolveTrace(id string) (string, error) {
	return resolveTrace(context.Background(), r.db, id)
}

func (v TraceGroupingRequest) validate() error {
	if v.Action != "merge" && v.Action != "move" && v.Action != "split" {
		return fmt.Errorf("%w: invalid grouping action", benchmark.ErrInvalid)
	}
	if len(v.TraceIDs) < 1 || len(v.TraceIDs) > 100 || v.Action != "merge" && len(v.TraceIDs) != 1 || len(v.LogIDs) > 10000 || v.Action != "merge" && len(v.LogIDs) == 0 || v.Action == "merge" && len(v.LogIDs) > 0 {
		return fmt.Errorf("%w: invalid grouping selection", benchmark.ErrInvalid)
	}
	for _, ids := range [][]string{v.TraceIDs, v.LogIDs} {
		seen := map[string]bool{}
		for _, id := range ids {
			if strings.TrimSpace(id) == "" || len(id) > 200 || seen[id] {
				return fmt.Errorf("%w: invalid grouping identifier", benchmark.ErrInvalid)
			}
			seen[id] = true
		}
	}
	if v.Action == "split" && v.Target != "" || v.Action != "split" && (strings.TrimSpace(v.Target) == "" || len(v.Target) > 200) {
		return fmt.Errorf("%w: invalid grouping target", benchmark.ErrInvalid)
	}
	return nil
}
func groupingSelection(v TraceGroupingRequest) (string, string) {
	if v.Action == "merge" {
		ids := []string{}
		for _, id := range v.TraceIDs {
			if id != v.Target {
				ids = append(ids, id)
			}
		}
		raw, _ := json.Marshal(ids)
		return "trace_id IN (SELECT value FROM json_each(?))", string(raw)
	}
	raw, _ := json.Marshal(v.LogIDs)
	return "trace_id='" + strings.ReplaceAll(v.TraceIDs[0], "'", "''") + "' AND id IN (SELECT value FROM json_each(?))", string(raw)
}

func previewTraceGrouping(ctx context.Context, tx *sql.Tx, v TraceGroupingRequest) (TraceGroupingPreview, error) {
	p := TraceGroupingPreview{CanApply: true, Target: v.Target, TraceCount: len(v.TraceIDs)}
	block := func(reason string) {
		if p.Reason == "" {
			p.Reason = reason
			p.CanApply = false
		}
	}
	ids := append([]string{}, v.TraceIDs...)
	if v.Target != "" {
		ids = append(ids, v.Target)
	}
	sort.Strings(ids)
	h := sha256.New()
	selection, _ := json.Marshal(struct {
		Action       string
		Traces, Logs []string
		Target       string
	}{v.Action, v.TraceIDs, v.LogIDs, v.Target})
	h.Write(selection)
	previous := ""
	for _, id := range ids {
		if id == previous {
			continue
		}
		previous = id
		resolved, err := resolveTrace(ctx, tx, id)
		if err != nil {
			return p, err
		}
		if resolved != id {
			block("changed")
		}
		var count, revision, metadata int64
		var pending, explicit bool
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE((SELECT revision FROM trace_revisions WHERE trace_id=?),0),COALESCE((SELECT revision FROM trace_metadata WHERE trace_id=?),0),COALESCE(MAX(CASE WHEN (l.status_code=0 AND COALESCE(l.error,'')='' AND l.created_at_unix_ms>?) OR EXISTS(SELECT 1 FROM capture_runs c WHERE c.id=l.capture_run_id AND c.stopped_at IS NULL) THEN 1 ELSE 0 END),0),COALESCE(MAX(explicit_trace_id!=''),0) FROM request_logs l WHERE trace_id=?`, id, id, time.Now().Add(-24*time.Hour).UnixMilli(), id).Scan(&count, &revision, &metadata, &pending, &explicit)
		if err != nil {
			return p, err
		}
		fmt.Fprintf(h, "%s:%d:%d:%d:%t;", id, count, revision, metadata, pending)
		if count == 0 {
			block("missing")
		}
		if pending {
			block("active")
		}
		if explicit && v.Action != "split" {
			block("explicit_split_only")
		}
		if id == v.Target {
			_ = tx.QueryRowContext(ctx, "SELECT COALESCE(json_extract(document,'$.name'),'') FROM trace_metadata WHERE trace_id=?", id).Scan(&p.TargetName)
		}
		// Include current experiment references in the preview version, not report bodies.
		rows, err := tx.QueryContext(ctx, `SELECT er.id,er.revision,e.revision,json_extract(er.document,'$.state')='running' OR EXISTS(SELECT 1 FROM review_reports rr WHERE rr.experiment_id=e.id AND rr.status='running') OR EXISTS(SELECT 1 FROM review_snapshots ss WHERE ss.experiment_id=e.id AND ss.state IN ('building','prepared')) FROM experiment_run_traces et JOIN experiment_runs er ON er.id=et.run_id JOIN experiments e ON e.id=er.experiment_id WHERE et.trace_id=? ORDER BY er.id`, id)
		if err != nil {
			return p, err
		}
		for rows.Next() {
			var run string
			var revision, parent int64
			var active bool
			if err = rows.Scan(&run, &revision, &parent, &active); err != nil {
				rows.Close()
				return p, err
			}
			fmt.Fprintf(h, "%s:%d:%d:%t;", run, revision, parent, active)
			if active {
				block("active")
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return p, err
		}
		// Capture-based and historical evidence references also protect running reviews.
		var reviewing bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM review_reports rr WHERE rr.status='running' AND (EXISTS(SELECT 1 FROM review_card_logs cl JOIN request_logs l ON l.id=cl.log_id WHERE cl.experiment_id=rr.experiment_id AND l.trace_id=?) OR EXISTS(SELECT 1 FROM experiment_runs er JOIN request_logs l ON l.capture_run_id=er.capture_run_id WHERE er.experiment_id=rr.experiment_id AND er.capture_run_id!='' AND l.trace_id=?) OR EXISTS(SELECT 1 FROM experiment_trace_samples s JOIN experiment_runs er ON er.id=s.run_id JOIN request_logs l ON l.id=s.log_id WHERE er.experiment_id=rr.experiment_id AND l.trace_id=?)))`, id, id, id).Scan(&reviewing)
		if err != nil {
			return p, err
		}
		if reviewing {
			block("active")
		}
	}
	predicate, raw := groupingSelection(v)
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM request_logs WHERE "+predicate, raw).Scan(&p.LogCount); err != nil {
		return p, err
	}
	if p.LogCount == 0 || v.Action != "merge" && p.LogCount != len(v.LogIDs) {
		block("changed")
	}
	p.Version = fmt.Sprintf("%x", h.Sum(nil))
	return p, nil
}
func (r *SQLiteRepository) PreviewTraceGrouping(ctx context.Context, v TraceGroupingRequest) (TraceGroupingPreview, error) {
	if err := v.validate(); err != nil {
		return TraceGroupingPreview{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TraceGroupingPreview{}, err
	}
	defer tx.Rollback()
	return previewTraceGrouping(ctx, tx, v)
}

func freezeTraceSamples(ctx context.Context, tx *sql.Tx, ids []string) error {
	for _, id := range ids {
		// A marker distinguishes an empty frozen sample from a live trace reference.
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO experiment_trace_samples SELECT et.run_id,et.trace_id,l.id FROM experiment_run_traces et JOIN request_logs l ON l.trace_id=et.trace_id WHERE et.trace_id=? AND NOT EXISTS(SELECT 1 FROM experiment_trace_snapshots s WHERE s.run_id=et.run_id AND s.trace_id=et.trace_id)`, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO experiment_trace_snapshots SELECT run_id,trace_id FROM experiment_run_traces WHERE trace_id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func (r *SQLiteRepository) ApplyTraceGrouping(ctx context.Context, v TraceGroupingRequest) (TraceGroupingResult, error) {
	result := TraceGroupingResult{}
	if err := v.validate(); err != nil {
		return result, err
	}
	if v.Version == "" {
		return result, ErrCorrelationConflict
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE trace_revisions SET revision=revision WHERE trace_id=''"); err != nil {
		return result, err
	}
	p, err := previewTraceGrouping(ctx, tx, v)
	if err != nil {
		return result, err
	}
	if !p.CanApply || p.Version != v.Version {
		return result, ErrCorrelationConflict
	}
	target := v.Target
	if v.Action == "split" {
		target = "manual-" + uuid.NewString()
	}
	affected := append(append([]string{}, v.TraceIDs...), target)
	if err = freezeTraceSamples(ctx, tx, affected); err != nil {
		return result, err
	}
	predicate, raw := groupingSelection(v)
	rows, err := tx.QueryContext(ctx, "SELECT id,trace_info FROM request_logs WHERE "+predicate+" ORDER BY created_at_unix_ms,id", raw)
	if err != nil {
		return result, err
	}
	type movedLog struct{ id, info string }
	moved := []movedLog{}
	for rows.Next() {
		var row movedLog
		if err = rows.Scan(&row.id, &row.info); err != nil {
			rows.Close()
			return result, err
		}
		moved = append(moved, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, row := range moved {
		var a trace.Assignment
		_ = json.Unmarshal([]byte(row.info), &a)
		a.TraceID = target
		a.Source = "manual"
		a.Reason = "manual_" + v.Action
		a.State = "declared"
		a.Locked = true
		a.Revision++
		info, _ := json.Marshal(a)
		if _, err = tx.ExecContext(ctx, `INSERT INTO trace_assignments(log_id,trace_id,trace_seq,parent_log_id,info,locked,features,updated_at) VALUES(?,?,0,'',?,1,'',?) ON CONFLICT(log_id) DO UPDATE SET trace_id=excluded.trace_id,info=excluded.info,locked=1`, row.id, target, string(info), time.Now().UnixMilli()); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE request_logs SET trace_id=?,trace_info=? WHERE id=?", target, string(info), row.id); err != nil {
			return result, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO trace_assignment_history(log_id,old_info,new_info,changed_at) VALUES(?,?,?,?)", row.id, row.info, string(info), time.Now().UnixMilli()); err != nil {
			return result, err
		}
	}
	if err = normalizeGroupedRequests(ctx, tx, affected, target); err != nil {
		return result, err
	}
	// Never drop retention protection when moving logs out of a favorited trace.
	pinned := false
	for _, id := range affected {
		var value bool
		err = tx.QueryRowContext(ctx, "SELECT COALESCE(json_extract(document,'$.pinned'),0) FROM trace_metadata WHERE trace_id=?", id).Scan(&value)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		pinned = pinned || value
	}
	if pinned {
		var m TraceMetadata
		var doc string
		err = tx.QueryRowContext(ctx, "SELECT document FROM trace_metadata WHERE trace_id=?", target).Scan(&doc)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return result, err
		}
		if doc != "" {
			if err = json.Unmarshal([]byte(doc), &m); err != nil {
				return result, err
			}
		} else {
			m = TraceMetadata{TraceID: target, Status: "none", Labels: []string{}}
		}
		m.Pinned = true
		m.Revision++
		data, _ := json.Marshal(m)
		if _, err = tx.ExecContext(ctx, "INSERT INTO trace_metadata VALUES(?,?,?) ON CONFLICT(trace_id) DO UPDATE SET document=excluded.document,revision=excluded.revision", target, string(data), m.Revision); err != nil {
			return result, err
		}
	}
	if v.Action == "merge" {
		for _, id := range v.TraceIDs {
			if id == target {
				continue
			}
			if _, err = tx.ExecContext(ctx, "UPDATE trace_redirects SET target=? WHERE target=?", target, id); err != nil {
				return result, err
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO trace_redirects VALUES(?,?)", id, target); err != nil {
				return result, err
			}
		}
	}
	sourceJSON, _ := json.Marshal(v.TraceIDs)
	if _, err = tx.ExecContext(ctx, "INSERT INTO trace_group_operations VALUES(?,?,?,?,?,?)", uuid.NewString(), v.Action, string(sourceJSON), target, len(moved), time.Now().UnixMilli()); err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return TraceGroupingResult{Target: target, LogCount: len(moved)}, nil
}

func normalizeGroupedRequests(ctx context.Context, tx *sql.Tx, ids []string, target string) error {
	raw, _ := json.Marshal(ids)
	rows, err := tx.QueryContext(ctx, `SELECT a.log_id,a.info,l.trace_id FROM trace_assignments a JOIN request_logs l ON l.id=a.log_id WHERE l.trace_id IN (SELECT value FROM json_each(?))`, string(raw))
	if err != nil {
		return err
	}
	type item struct{ id, info, group string }
	items := []item{}
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.id, &v.info, &v.group); err != nil {
			rows.Close()
			return err
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, v := range items {
		var a trace.Assignment
		if err = json.Unmarshal([]byte(v.info), &a); err != nil {
			return err
		}
		parents := []string{}
		for _, id := range a.Parents {
			var same bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM request_logs WHERE id=? AND trace_id=?)", id, v.group).Scan(&same); err != nil {
				return err
			}
			if same {
				parents = append(parents, id)
			}
		}
		a.Parents = parents
		info, _ := json.Marshal(a)
		parent := ""
		if len(parents) == 1 {
			parent = parents[0]
		}
		if _, err = tx.ExecContext(ctx, "UPDATE trace_assignments SET parent_log_id=?,info=? WHERE log_id=?", parent, string(info), v.id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE request_logs SET parent_log_id=?,trace_info=? WHERE id=?", parent, string(info), v.id); err != nil {
			return err
		}
	}
	// Display order follows real timestamps; merging never invents causal edges.
	if _, err = tx.ExecContext(ctx, `WITH ordered AS (SELECT id,ROW_NUMBER() OVER(ORDER BY created_at_unix_ms,id) AS n FROM request_logs WHERE trace_id=?) UPDATE trace_assignments SET trace_seq=(SELECT n FROM ordered WHERE id=log_id) WHERE log_id IN (SELECT id FROM ordered)`, target); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `WITH ordered AS (SELECT id,ROW_NUMBER() OVER(ORDER BY created_at_unix_ms,id) AS n FROM request_logs WHERE trace_id=?) UPDATE request_logs SET trace_seq=(SELECT n FROM ordered WHERE ordered.id=request_logs.id) WHERE trace_id=?`, target, target); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO trace_revisions(trace_id,revision,next_seq) VALUES(?,1,(SELECT COALESCE(MAX(trace_seq),0) FROM request_logs WHERE trace_id=?)) ON CONFLICT(trace_id) DO UPDATE SET next_seq=excluded.next_seq`, target, target)
	return err
}

func (a *AsyncRepository) PreviewTraceGrouping(ctx context.Context, v TraceGroupingRequest) (TraceGroupingPreview, error) {
	if repo, ok := a.inner.(TraceGroupingRepository); ok {
		return repo.PreviewTraceGrouping(ctx, v)
	}
	return TraceGroupingPreview{}, errors.New("trace grouping unavailable")
}
func (a *AsyncRepository) ResolveTrace(id string) (string, error) {
	if repo, ok := a.inner.(TraceGroupingRepository); ok {
		return repo.ResolveTrace(id)
	}
	return id, nil
}
func (a *AsyncRepository) ApplyTraceGrouping(ctx context.Context, v TraceGroupingRequest) (TraceGroupingResult, error) {
	a.correlationMu.Lock()
	defer a.correlationMu.Unlock()
	repo, ok := a.inner.(*SQLiteRepository)
	if !ok {
		return TraceGroupingResult{}, errors.New("trace grouping unavailable")
	}
	result, err := repo.ApplyTraceGrouping(ctx, v)
	if err == nil && a.correlator != nil {
		a.reloadCorrelator(repo)
	}
	return result, err
}
