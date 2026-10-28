package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/trace"
)

type TaskManagementItem struct {
	ID       string `json:"id"`
	Revision *int64 `json:"revision,omitempty"`
}
type TaskManagementRequest struct {
	Kind         string               `json:"kind"`
	Action       string               `json:"action"`
	Items        []TaskManagementItem `json:"items"`
	ExperimentID string               `json:"experiment_id,omitempty"`
}
type TaskReference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type TaskManagementPreviewItem struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Revision      int64           `json:"revision"`
	LogCount      int64           `json:"log_count"`
	RunCount      int64           `json:"run_count"`
	ReportCount   int64           `json:"report_count"`
	BlockedReason string          `json:"blocked_reason,omitempty"`
	ReferencedBy  []TaskReference `json:"referenced_by,omitempty"`
}
type TaskManagementPreview struct {
	Items       []TaskManagementPreviewItem `json:"items"`
	TotalItems  int                         `json:"total_items"`
	LogCount    int64                       `json:"log_count"`
	RunCount    int64                       `json:"run_count"`
	ReportCount int64                       `json:"report_count"`
	CanApply    bool                        `json:"can_apply"`
}
type TaskManagementResult struct {
	Affected    int   `json:"affected"`
	DeletedLogs int64 `json:"deleted_logs"`
}
type TaskManagementBlockedError struct{ Preview TaskManagementPreview }

func (e *TaskManagementBlockedError) Error() string { return "task_management_blocked" }

type TaskManagementRepository interface {
	PreviewTaskManagement(context.Context, TaskManagementRequest) (TaskManagementPreview, error)
	ApplyTaskManagement(context.Context, TaskManagementRequest) (TaskManagementResult, error)
}

func (v TaskManagementRequest) Validate() error {
	if len(v.Items) < 1 || len(v.Items) > 100 {
		return fmt.Errorf("%w: select 1–100 tasks", benchmark.ErrInvalid)
	}
	if v.Kind != "trace" && v.Kind != "capture" && v.Kind != "experiment" && v.Kind != "template" && v.Kind != "run" {
		return fmt.Errorf("%w: invalid task kind", benchmark.ErrInvalid)
	}
	if v.Action != "archive" && v.Action != "restore" && v.Action != "delete" || v.Kind == "run" && (v.Action != "delete" || v.ExperimentID == "") {
		return fmt.Errorf("%w: invalid task action", benchmark.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, item := range v.Items {
		if item.ID == "" || len(item.ID) > 1024 || seen[item.ID] || item.Revision != nil && *item.Revision < 0 {
			return fmt.Errorf("%w: invalid or duplicate task", benchmark.ErrInvalid)
		}
		seen[item.ID] = true
	}
	return nil
}

func (r *SQLiteRepository) migrateTaskManagement() error {
	for _, column := range []struct{ name, def string }{{"archived", "INTEGER NOT NULL DEFAULT 0"}, {"revision", "INTEGER NOT NULL DEFAULT 0"}} {
		exists, err := r.hasColumn("capture_runs", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = r.db.Exec("ALTER TABLE capture_runs ADD COLUMN " + column.name + " " + column.def); err != nil {
				return err
			}
		}
	}
	_, err := r.db.Exec(`CREATE INDEX IF NOT EXISTS idx_capture_archive_recent ON capture_runs(archived,started_at DESC,id DESC);
 CREATE TABLE IF NOT EXISTS deleted_log_ids(id TEXT PRIMARY KEY,deleted_at INTEGER NOT NULL);
 CREATE TRIGGER IF NOT EXISTS prevent_deleted_log_restore BEFORE INSERT ON request_logs
 WHEN EXISTS(SELECT 1 FROM deleted_log_ids d WHERE d.id=NEW.id) BEGIN SELECT RAISE(IGNORE); END;
 CREATE TRIGGER IF NOT EXISTS prevent_deleted_annotation_restore BEFORE INSERT ON log_annotations
 WHEN EXISTS(SELECT 1 FROM deleted_log_ids d WHERE d.id=NEW.log_id) BEGIN SELECT RAISE(IGNORE); END;`)
	return err
}

func (r *SQLiteRepository) PreviewTaskManagement(ctx context.Context, request TaskManagementRequest) (TaskManagementPreview, error) {
	if err := request.Validate(); err != nil {
		return TaskManagementPreview{}, err
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TaskManagementPreview{}, err
	}
	defer tx.Rollback()
	return previewTasks(ctx, tx, request)
}

func previewTasks(ctx context.Context, tx *sql.Tx, request TaskManagementRequest) (TaskManagementPreview, error) {
	preview := TaskManagementPreview{Items: []TaskManagementPreviewItem{}, TotalItems: len(request.Items), CanApply: true}
	for _, selected := range request.Items {
		item, err := previewTask(ctx, tx, request, selected.ID)
		if errors.Is(err, sql.ErrNoRows) {
			item = TaskManagementPreviewItem{ID: selected.ID, Name: selected.ID, BlockedReason: "task_not_found"}
		} else if err != nil {
			return preview, err
		}
		if item.BlockedReason == "" && selected.Revision != nil && *selected.Revision != item.Revision {
			item.BlockedReason = "task_changed"
		}
		if item.BlockedReason != "" {
			preview.CanApply = false
		}
		preview.Items = append(preview.Items, item)
		preview.LogCount += item.LogCount
		preview.RunCount += item.RunCount
		preview.ReportCount += item.ReportCount
	}
	return preview, nil
}

func previewTask(ctx context.Context, tx *sql.Tx, request TaskManagementRequest, id string) (TaskManagementPreviewItem, error) {
	item := TaskManagementPreviewItem{ID: id, Name: id}
	active := false
	switch request.Kind {
	case "trace":
		var count, metadataRevision, logRevision int64
		err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE((SELECT json_extract(document,'$.name') FROM trace_metadata WHERE trace_id=?),''),COALESCE((SELECT revision FROM trace_metadata WHERE trace_id=?),0),COALESCE((SELECT revision FROM trace_revisions WHERE trace_id=?),0),COALESCE(MAX(CASE WHEN (l.status_code=0 AND COALESCE(l.error,'')='' AND l.created_at_unix_ms>?) OR EXISTS(SELECT 1 FROM capture_runs c WHERE c.id=l.capture_run_id AND c.stopped_at IS NULL) THEN 1 ELSE 0 END),0) FROM request_logs l WHERE trace_id=?`, id, id, id, time.Now().Add(-24*time.Hour).UnixMilli(), id).Scan(&count, &item.Name, &metadataRevision, &logRevision, &active)
		if err != nil {
			return item, err
		}
		if count == 0 {
			return item, sql.ErrNoRows
		}
		item.LogCount = count
		token := sha256.Sum256([]byte(fmt.Sprintf("trace:%d:%d:%d", metadataRevision, logRevision, count)))
		item.Revision = int64(binary.BigEndian.Uint64(token[:8]) & ((1 << 52) - 1))
		if item.Name == "" {
			item.Name = id
		}
	case "capture":
		var pending bool
		err := tx.QueryRowContext(ctx, `SELECT name,revision,stopped_at IS NULL,(SELECT COUNT(*) FROM request_logs WHERE capture_run_id=c.id),EXISTS(SELECT 1 FROM request_logs l WHERE l.capture_run_id=c.id AND l.status_code=0 AND COALESCE(l.error,'')='' AND l.created_at_unix_ms>?) FROM capture_runs c WHERE id=?`, time.Now().Add(-24*time.Hour).UnixMilli(), id).Scan(&item.Name, &item.Revision, &active, &item.LogCount, &pending)
		if err != nil {
			return item, err
		}
		active = active || pending
	case "experiment":
		err := tx.QueryRowContext(ctx, `SELECT json_extract(document,'$.name'),revision,(SELECT COUNT(*) FROM experiment_runs WHERE experiment_id=e.id),(SELECT COUNT(*) FROM review_reports WHERE experiment_id=e.id),EXISTS(SELECT 1 FROM review_reports WHERE experiment_id=e.id AND status='running') OR EXISTS(SELECT 1 FROM experiment_runs er WHERE er.experiment_id=e.id AND (json_extract(er.document,'$.state')='running' OR EXISTS(SELECT 1 FROM capture_runs c WHERE c.id=er.capture_run_id AND c.stopped_at IS NULL))) FROM experiments e WHERE id=?`, id).Scan(&item.Name, &item.Revision, &item.RunCount, &item.ReportCount, &active)
		if err != nil {
			return item, err
		}
		version, err := cardManagementVersion(ctx, tx, id, item.Revision)
		if err != nil {
			return item, err
		}
		item.Revision = version
	case "template":
		if strings.HasPrefix(id, "builtin:") {
			item.BlockedReason = "task_builtin"
			return item, nil
		}
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(document,'$.name'),revision FROM review_templates WHERE id=?`, id).Scan(&item.Name, &item.Revision); err != nil {
			return item, err
		}
	case "run":
		if err := tx.QueryRowContext(ctx, `SELECT json_extract(document,'$.name'),revision,json_extract(document,'$.state')='running' OR EXISTS(SELECT 1 FROM capture_runs c WHERE c.id=er.capture_run_id AND c.stopped_at IS NULL) OR EXISTS(SELECT 1 FROM review_reports WHERE experiment_id=er.experiment_id AND status='running') FROM experiment_runs er WHERE id=? AND experiment_id=?`, id, request.ExperimentID).Scan(&item.Name, &item.Revision, &active); err != nil {
			return item, err
		}
	}
	if active && (request.Action == "delete" || request.Action == "archive" && (request.Kind == "capture" || request.Kind == "experiment")) {
		item.BlockedReason = "task_active"
	}
	if request.Action == "delete" && (request.Kind == "trace" || request.Kind == "capture") {
		refs, err := taskReferences(ctx, tx, request.Kind, id)
		if err != nil {
			return item, err
		}
		item.ReferencedBy = refs
		if len(refs) > 0 && item.BlockedReason == "" {
			item.BlockedReason = "task_referenced"
		}
	}
	return item, nil
}

// The card's own edit revision does not change when runs/reports are added.
// Hash only lightweight identity/version columns, and keep the token within
// JavaScript's exact integer range so the browser can round-trip it unchanged.
func cardManagementVersion(ctx context.Context, tx *sql.Tx, id string, parent int64) (int64, error) {
	hash := sha256.New()
	fmt.Fprintf(hash, "parent:%d\n", parent)
	rows, err := tx.QueryContext(ctx, `SELECT 'run:'||id||':'||revision AS version FROM experiment_runs WHERE experiment_id=?
 UNION ALL SELECT 'report:'||id||':'||status AS version FROM review_reports WHERE experiment_id=? ORDER BY version`, id, id)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var version string
		if err = rows.Scan(&version); err != nil {
			return 0, err
		}
		fmt.Fprintln(hash, version)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(hash.Sum(nil)[:8]) & ((1 << 52) - 1)), nil
}

func taskReferences(ctx context.Context, tx *sql.Tx, kind, id string) ([]TaskReference, error) {
	predicate := "l.trace_id=?"
	if kind == "capture" {
		predicate = "l.capture_run_id=?"
	}
	direct := `EXISTS(SELECT 1 FROM experiment_runs er WHERE er.experiment_id=e.id AND er.capture_run_id=?)`
	if kind == "trace" {
		direct = `EXISTS(SELECT 1 FROM experiment_run_traces et JOIN experiment_runs er ON er.id=et.run_id WHERE er.experiment_id=e.id AND et.trace_id=?)`
	}
	query := `SELECT e.id,COALESCE(json_extract(e.document,'$.name'),e.id) FROM experiments e WHERE ` + direct + ` OR EXISTS(SELECT 1 FROM experiment_runs er JOIN request_logs l ON l.capture_run_id=er.capture_run_id AND er.capture_run_id!='' WHERE er.experiment_id=e.id AND ` + predicate + `) OR EXISTS(SELECT 1 FROM review_card_logs cl JOIN request_logs l ON l.id=cl.log_id WHERE cl.experiment_id=e.id AND ` + predicate + `)`
	args := []any{id, id, id}
	query += ` OR EXISTS(SELECT 1 FROM experiment_trace_samples s JOIN experiment_runs er ON er.id=s.run_id JOIN request_logs l ON l.id=s.log_id WHERE er.experiment_id=e.id AND ` + predicate + `)`
	args = append(args, id)
	if kind == "capture" { // A run may import a trace whose requests belong to this capture.
		query += ` OR EXISTS(SELECT 1 FROM experiment_run_traces et JOIN experiment_runs er ON er.id=et.run_id JOIN request_logs l ON l.trace_id=et.trace_id WHERE er.experiment_id=e.id AND l.capture_run_id=?)`
		args = append(args, id)
	} else {
		query += ` OR EXISTS(SELECT 1 FROM review_reports rr,json_each(rr.document,'$.tasks') rt,json_each(rt.value,'$.metrics.trace_ids') ti WHERE rr.experiment_id=e.id AND ti.value=?)`
		args = append(args, id)
	}
	query += " ORDER BY e.id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	refs := []TaskReference{}
	for rows.Next() {
		var ref TaskReference
		if err = rows.Scan(&ref.ID, &ref.Name); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (r *SQLiteRepository) ApplyTaskManagement(ctx context.Context, request TaskManagementRequest) (TaskManagementResult, error) {
	if err := request.Validate(); err != nil {
		return TaskManagementResult{}, err
	}
	// Capture operations use this order too. Keep the registry and database atomic.
	r.captures.mu.Lock()
	defer r.captures.mu.Unlock()
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return TaskManagementResult{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE trace_metadata SET revision=revision WHERE trace_id=''"); err != nil {
		return TaskManagementResult{}, err
	}
	preview, err := previewTasks(ctx, tx, request)
	if err != nil {
		return TaskManagementResult{}, err
	}
	if !preview.CanApply {
		return TaskManagementResult{}, &TaskManagementBlockedError{Preview: preview}
	}
	result := TaskManagementResult{}
	for _, item := range preview.Items {
		if request.Action == "delete" {
			n, err := deleteTask(ctx, tx, request, item.ID)
			if err != nil {
				return result, err
			}
			result.DeletedLogs += n
		} else if err = archiveTask(ctx, tx, request.Kind, item.ID, request.Action == "archive"); err != nil {
			return result, err
		}
		result.Affected++
	}
	if err = tx.Commit(); err != nil {
		return TaskManagementResult{}, err
	}
	return result, nil
}

func archiveTask(ctx context.Context, tx *sql.Tx, kind, id string, archived bool) error {
	if kind == "capture" {
		_, err := tx.ExecContext(ctx, "UPDATE capture_runs SET archived=?,revision=revision+1 WHERE id=?", archived, id)
		return err
	}
	table, key := "experiments", "id"
	if kind == "trace" {
		table, key = "trace_metadata", "trace_id"
	}
	if kind == "template" {
		table = "review_templates"
	}
	var raw string
	err := tx.QueryRowContext(ctx, "SELECT document FROM "+table+" WHERE "+key+"=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) && kind == "trace" {
		m := TraceMetadata{TraceID: id, Status: "none", Labels: []string{}, Archived: archived, Revision: 1}
		value, _ := json.Marshal(m)
		_, err = tx.ExecContext(ctx, "INSERT INTO trace_metadata VALUES(?,?,?)", id, string(value), 1)
		return err
	}
	if err != nil {
		return err
	}
	patch := `UPDATE ` + table + ` SET document=json_set(document,'$.archived',json(?),'$.revision',revision+1),revision=revision+1`
	if kind == "trace" {
		// Archived tasks return as ordinary tasks when restored. Favorites are
		// independent and continue to protect the complete trace.
		patch = `UPDATE trace_metadata SET document=json_set(document,'$.archived',json(?),'$.top_at',0,'$.revision',revision+1),revision=revision+1`
	}
	args := []any{fmt.Sprint(archived)}
	if kind != "trace" {
		now := time.Now().UTC()
		patch = `UPDATE ` + table + ` SET document=json_set(document,'$.archived',json(?),'$.revision',revision+1,'$.updated_at',?),revision=revision+1,updated_at=?`
		args = append(args, now.Format(time.RFC3339Nano), now.UnixMilli())
	}
	if kind == "template" {
		patch += ",archived=?"
		args = append(args, archived)
	}
	_, err = tx.ExecContext(ctx, patch+" WHERE "+key+"=?", append(args, id)...)
	return err
}

func deleteTask(ctx context.Context, tx *sql.Tx, request TaskManagementRequest, id string) (int64, error) {
	exec := func(query string, args ...any) error { _, err := tx.ExecContext(ctx, query, args...); return err }
	switch request.Kind {
	case "trace":
		if err := exec("INSERT OR IGNORE INTO deleted_log_ids SELECT id,? FROM request_logs WHERE trace_id=?", time.Now().UnixMilli(), id); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM request_logs WHERE trace_id=?", id)
		if err != nil {
			return 0, err
		}
		n, _ := result.RowsAffected()
		for _, query := range []string{"DELETE FROM log_annotations WHERE log_id IN (SELECT id FROM deleted_log_ids)", "DELETE FROM trace_metadata WHERE trace_id=?", "DELETE FROM trace_summaries WHERE trace_id=?", "DELETE FROM trace_summary_dirty WHERE trace_id=?"} {
			args := []any{}
			if strings.Contains(query, "?") {
				args = append(args, id)
			}
			if err = exec(query, args...); err != nil {
				return 0, err
			}
		}
		return n, nil
	case "capture":
		// Preserve already declared single-task membership after its recording label is removed.
		if err := exec(`UPDATE trace_assignments SET locked=1,info=json_set(info,'$.locked',json('true')) WHERE log_id IN (SELECT id FROM request_logs WHERE capture_run_id=?) AND json_extract(info,'$.source')='capture'`, id); err != nil {
			return 0, err
		}
		if err := exec(`UPDATE request_logs SET trace_info=(SELECT info FROM trace_assignments WHERE log_id=request_logs.id) WHERE capture_run_id=? AND EXISTS(SELECT 1 FROM trace_assignments WHERE log_id=request_logs.id AND json_extract(info,'$.source')='capture')`, id); err != nil {
			return 0, err
		}
		return 0, exec("DELETE FROM capture_runs WHERE id=?", id)
	case "experiment":
		for _, query := range []string{"DELETE FROM experiment_run_traces WHERE run_id IN (SELECT id FROM experiment_runs WHERE experiment_id=?)", "DELETE FROM experiment_runs WHERE experiment_id=?", "DELETE FROM review_reports WHERE experiment_id=?", "DELETE FROM review_card_logs WHERE experiment_id=?", "DELETE FROM experiments WHERE id=?"} {
			if err := exec(query, id); err != nil {
				return 0, err
			}
		}
	case "template":
		return 0, exec("DELETE FROM review_templates WHERE id=?", id)
	case "run":
		if err := exec("DELETE FROM experiment_run_traces WHERE run_id=?", id); err != nil {
			return 0, err
		}
		if err := exec("DELETE FROM experiment_runs WHERE id=? AND experiment_id=?", id, request.ExperimentID); err != nil {
			return 0, err
		}
		return 0, exec(`UPDATE experiments SET revision=revision+1,document=json_set(document,'$.revision',revision+1),updated_at=? WHERE id=?`, time.Now().UnixMilli(), request.ExperimentID)
	}
	return 0, nil
}

func (a *AsyncRepository) PreviewTaskManagement(ctx context.Context, v TaskManagementRequest) (TaskManagementPreview, error) {
	if r, ok := a.inner.(TaskManagementRepository); ok {
		return r.PreviewTaskManagement(ctx, v)
	}
	return TaskManagementPreview{}, errors.New("task management unavailable")
}
func (a *AsyncRepository) ApplyTaskManagement(ctx context.Context, v TaskManagementRequest) (TaskManagementResult, error) {
	a.correlationMu.Lock()
	defer a.correlationMu.Unlock()
	r, ok := a.inner.(TaskManagementRepository)
	if !ok {
		return TaskManagementResult{}, errors.New("task management unavailable")
	}
	result, err := r.ApplyTaskManagement(ctx, v)
	if err == nil && v.Action == "delete" && (v.Kind == "trace" || v.Kind == "capture") {
		if store, ok := a.inner.(*SQLiteRepository); ok && a.correlator != nil {
			a.correlator = trace.NewCorrelator()
			if observations, e := store.loadObservations(); e == nil {
				for _, o := range observations {
					a.correlator.Restore(o)
				}
			}
		}
	}
	return result, err
}

func (a *AsyncRepository) ListCaptureRunsPageArchived(limit, offset int, archived bool) ([]CaptureRun, int, error) {
	if store, ok := a.inner.(interface {
		ListCaptureRunsPageArchived(int, int, bool) ([]CaptureRun, int, error)
	}); ok {
		return store.ListCaptureRunsPageArchived(limit, offset, archived)
	}
	runs, err := a.ListCaptureRuns()
	if err != nil {
		return nil, 0, err
	}
	filtered := []CaptureRun{}
	for _, run := range runs {
		if run.Archived == archived {
			filtered = append(filtered, run)
		}
	}
	total := len(filtered)
	offset = min(max(offset, 0), total)
	limit = min(max(limit, 1), 100)
	return filtered[offset:min(offset+limit, total)], total, nil
}
func (a *AsyncRepository) ListReviewTemplatesArchived(q string, archived bool, offset, limit int) ([]benchmark.ReviewTemplateSummary, int64, error) {
	if store, ok := a.inner.(interface {
		ListReviewTemplatesArchived(string, bool, int, int) ([]benchmark.ReviewTemplateSummary, int64, error)
	}); ok {
		return store.ListReviewTemplatesArchived(q, archived, offset, limit)
	}
	if !archived {
		return a.ListReviewTemplates(q, offset, limit)
	}
	return []benchmark.ReviewTemplateSummary{}, 0, nil
}
