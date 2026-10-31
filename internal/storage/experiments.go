package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/benchmark"
)

type TraceMetadata struct {
	Origin  *TraceOrigin `json:"origin,omitempty"` // Read-only, derived from grouping history.
	TraceID string       `json:"trace_id"`
	Name    string       `json:"name"`
	Notes   string       `json:"notes"`
	Labels  []string     `json:"labels"`
	Pinned  bool         `json:"pinned"`
	// TopAt is server-assigned Unix milliseconds. A positive value requests
	// pinning to the top; zero removes it. Pinned retains its favorite meaning.
	TopAt    int64  `json:"top_at,omitempty"`
	Archived bool   `json:"archived"`
	Status   string `json:"status"`
	Revision int64  `json:"revision"`
}

// ExperimentRepository is optional to preserve existing repository integrations.
type ExperimentRepository interface {
	GetTraceMetadataDocument(string) (TraceMetadata, error)
	SaveTraceMetadata(TraceMetadata) (TraceMetadata, error)
	ListExperiments(string, bool, int, int) ([]benchmark.Experiment, int64, error)
	GetExperiment(string) (benchmark.Experiment, error)
	SaveExperiment(benchmark.Experiment) (benchmark.Experiment, error)
	ListExperimentRuns(string, int, int) ([]benchmark.Run, int64, error)
	GetExperimentRun(string) (benchmark.Run, error)
	SaveExperimentRun(benchmark.Run) (benchmark.Run, error)
	StartExperimentCapture(string, int64, string) (benchmark.Run, error)
	StopExperimentCapture(string, int64) (benchmark.Run, error)
	ExperimentLogs(context.Context, string, int) ([]*RequestLog, error)
}

func (r *SQLiteRepository) migrateExperiments() error {
	_, err := r.db.Exec(`
 CREATE TABLE IF NOT EXISTS trace_metadata(trace_id TEXT PRIMARY KEY, document TEXT NOT NULL, revision INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS experiments(id TEXT PRIMARY KEY, document TEXT NOT NULL, revision INTEGER NOT NULL, updated_at INTEGER NOT NULL);
 CREATE TABLE IF NOT EXISTS experiment_runs(id TEXT PRIMARY KEY, experiment_id TEXT NOT NULL, capture_run_id TEXT NOT NULL DEFAULT '', document TEXT NOT NULL, revision INTEGER NOT NULL, updated_at INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_experiment_runs_experiment ON experiment_runs(experiment_id,updated_at);
 DROP INDEX IF EXISTS idx_experiment_capture;
 CREATE UNIQUE INDEX IF NOT EXISTS idx_experiment_card_capture ON experiment_runs(experiment_id,capture_run_id) WHERE capture_run_id!='';
 CREATE TABLE IF NOT EXISTS experiment_run_traces(run_id TEXT NOT NULL, trace_id TEXT NOT NULL, PRIMARY KEY(run_id,trace_id));
 CREATE INDEX IF NOT EXISTS idx_experiment_trace ON experiment_run_traces(trace_id);
 CREATE TABLE IF NOT EXISTS trace_summary_dirty(trace_id TEXT PRIMARY KEY);
 CREATE TABLE IF NOT EXISTS trace_summaries(trace_id TEXT PRIMARY KEY, request_count INTEGER NOT NULL, first_time INTEGER NOT NULL, last_time INTEGER NOT NULL, total_latency INTEGER NOT NULL, errors INTEGER NOT NULL, input_tokens INTEGER, output_tokens INTEGER, total_tokens INTEGER, upstreams TEXT NOT NULL, tags TEXT NOT NULL, sources TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_trace_summary_recent ON trace_summaries(last_time DESC,trace_id);
 CREATE TRIGGER IF NOT EXISTS trace_summary_insert AFTER INSERT ON request_logs WHEN COALESCE(NEW.trace_id,'')!='' BEGIN INSERT INTO trace_summary_dirty VALUES(NEW.trace_id) ON CONFLICT(trace_id) DO NOTHING; END;
 CREATE TRIGGER IF NOT EXISTS trace_summary_update AFTER UPDATE ON request_logs BEGIN
 INSERT INTO trace_summary_dirty SELECT NEW.trace_id WHERE COALESCE(NEW.trace_id,'')!='' ON CONFLICT(trace_id) DO NOTHING;
 INSERT INTO trace_summary_dirty SELECT OLD.trace_id WHERE COALESCE(OLD.trace_id,'')!='' ON CONFLICT(trace_id) DO NOTHING; END;
 CREATE TRIGGER IF NOT EXISTS trace_summary_delete AFTER DELETE ON request_logs WHEN COALESCE(OLD.trace_id,'')!='' BEGIN INSERT INTO trace_summary_dirty VALUES(OLD.trace_id) ON CONFLICT(trace_id) DO NOTHING; END;
 INSERT OR IGNORE INTO trace_summary_dirty SELECT DISTINCT trace_id FROM request_logs WHERE trace_id!='' AND trace_id NOT IN (SELECT trace_id FROM trace_summaries);
 `)
	if err != nil {
		return err
	}
	if err = r.migrateReviews(); err != nil {
		return err
	}
	if err = r.migrateTaskManagement(); err != nil {
		return err
	}
	if err = r.migrateTraceOrdering(); err != nil {
		return err
	}
	if err = r.migrateTraceGrouping(); err != nil {
		return err
	}
	return r.migrateTraceRetention()
}

func (r *SQLiteRepository) GetTraceMetadataDocument(id string) (TraceMetadata, error) {
	m := TraceMetadata{TraceID: id, Status: "none", Labels: []string{}}
	var raw string
	err := r.db.QueryRow("SELECT document FROM trace_metadata WHERE trace_id=?", id).Scan(&raw)
	if err == sql.ErrNoRows {
		var count int
		err = r.db.QueryRow("SELECT COUNT(*) FROM request_logs WHERE trace_id=?", id).Scan(&count)
		if err == nil && count == 0 {
			err = sql.ErrNoRows
		}
		if err == nil {
			m.Origin, err = r.traceOrigin(id)
		}
		return m, err
	}
	if err == nil {
		err = json.Unmarshal([]byte(raw), &m)
	}
	if err == nil {
		m.Origin, err = r.traceOrigin(id)
	}
	return m, err
}

func (r *SQLiteRepository) SaveTraceMetadata(m TraceMetadata) (TraceMetadata, error) {
	// The caller cannot forge, edit or remove a system provenance marker.
	m.Origin = nil
	m.Name = strings.TrimSpace(m.Name)
	m.Labels = normalizeLabels(m.Labels)
	if len(m.Name) > 512 || len(m.Notes) > 16000 || len(m.Labels) > 20 {
		return m, fmt.Errorf("%w: trace metadata too large", benchmark.ErrInvalid)
	}
	if m.TopAt < 0 {
		return m, fmt.Errorf("%w: invalid trace top time", benchmark.ErrInvalid)
	}
	if m.Status == "" {
		m.Status = "none"
	}
	if m.Status != "none" && m.Status != "todo" && m.Status != "done" {
		return m, fmt.Errorf("%w: invalid trace status", benchmark.ErrInvalid)
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return m, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE trace_metadata SET revision=revision WHERE trace_id='' "); err != nil {
		return m, err
	}
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM request_logs WHERE trace_id=?", m.TraceID).Scan(&count); err != nil {
		return m, err
	}
	if count == 0 {
		return m, sql.ErrNoRows
	}
	var revision int64
	var previousDocument string
	err = tx.QueryRow("SELECT revision,document FROM trace_metadata WHERE trace_id=?", m.TraceID).Scan(&revision, &previousDocument)
	if err != nil && err != sql.ErrNoRows {
		return m, err
	}
	if revision != m.Revision {
		return m, ErrCorrelationConflict
	}
	var previous TraceMetadata
	if previousDocument != "" {
		if err = json.Unmarshal([]byte(previousDocument), &previous); err != nil {
			return m, err
		}
	}
	if m.Archived {
		m.TopAt = 0
	} else if m.TopAt > 0 {
		m.TopAt = previous.TopAt
		if m.TopAt == 0 {
			m.TopAt = time.Now().UnixMilli()
		}
	}
	m.Revision++
	raw, _ := json.Marshal(m)
	_, err = tx.Exec("INSERT INTO trace_metadata VALUES(?,?,?) ON CONFLICT(trace_id) DO UPDATE SET document=excluded.document,revision=excluded.revision", m.TraceID, string(raw), m.Revision)
	if err == nil {
		err = tx.Commit()
	}
	if err == nil {
		m.Origin, err = r.traceOrigin(m.TraceID)
	}
	return m, err
}

func (r *SQLiteRepository) GetExperiment(id string) (benchmark.Experiment, error) {
	var e benchmark.Experiment
	var raw string
	err := r.db.QueryRow("SELECT document,(SELECT COUNT(*) FROM experiment_runs WHERE experiment_id=experiments.id) FROM experiments WHERE id=?", id).Scan(&raw, &e.RunCount)
	count := e.RunCount
	if err == nil {
		err = json.Unmarshal([]byte(raw), &e)
		e.RunCount = count
	}
	return e, err
}

func (r *SQLiteRepository) ListExperiments(q string, archived bool, offset, limit int) ([]benchmark.Experiment, int64, error) {
	limit = pageLimit(limit)
	where := "WHERE (json_extract(document,'$.name') LIKE ? OR json_extract(document,'$.notes') LIKE ?) AND json_extract(document,'$.archived')=?"
	args := []any{"%" + q + "%", "%" + q + "%", archived}
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM experiments "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query("SELECT json_remove(document,'$.task','$.review','$.notes'),(SELECT COUNT(*) FROM experiment_runs WHERE experiment_id=experiments.id),COALESCE((SELECT status FROM review_reports WHERE experiment_id=experiments.id ORDER BY created_at DESC,id DESC LIMIT 1),''),COALESCE(json_extract(document,'$.review.template_name'),''),COALESCE(json_array_length(document,'$.review.selected'),0),COALESCE(json_extract(document,'$.review.template_id'),'') FROM experiments "+where+" ORDER BY updated_at DESC,id LIMIT ? OFFSET ?", append(args, limit, max(0, offset))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []benchmark.Experiment{}
	for rows.Next() {
		var raw string
		var count int
		var status, templateName, templateID string
		var pointCount int
		var e benchmark.Experiment
		if err = rows.Scan(&raw, &count, &status, &templateName, &pointCount, &templateID); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(raw), &e); err != nil {
			return nil, 0, err
		}
		e.RunCount = count
		e.Task = benchmark.Task{}
		e.ReviewSummary = &benchmark.ReviewSummary{Status: status, TemplateID: templateID, TemplateName: templateName, PointCount: pointCount}
		e.Review = nil
		items = append(items, e)
	}
	return items, total, rows.Err()
}

func pageLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	return min(limit, 100)
}

func (r *SQLiteRepository) SaveExperiment(e benchmark.Experiment) (benchmark.Experiment, error) {
	if err := e.Validate(); err != nil {
		return e, err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return e, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE experiments SET revision=revision WHERE id='' "); err != nil {
		return e, err
	}
	now := time.Now().UTC()
	if e.ID == "" {
		e.ID = uuid.NewString()
		e.CreatedAt = now
		e.Revision = 0
	} else {
		var raw string
		var old benchmark.Experiment
		if err = tx.QueryRow("SELECT document FROM experiments WHERE id=?", e.ID).Scan(&raw); err != nil {
			return e, err
		}
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return e, err
		}
		if old.Revision != e.Revision {
			return e, ErrCorrelationConflict
		}
		e.CreatedAt = old.CreatedAt
	}
	e.Revision++
	e.UpdatedAt = now
	e.RunCount = 0
	raw, _ := json.Marshal(e)
	_, err = tx.Exec("INSERT INTO experiments VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document,revision=excluded.revision,updated_at=excluded.updated_at", e.ID, string(raw), e.Revision, now.UnixMilli())
	if err == nil {
		err = tx.Commit()
	}
	return e, err
}

func (r *SQLiteRepository) GetExperimentRun(id string) (benchmark.Run, error) {
	var run benchmark.Run
	var raw string
	err := r.db.QueryRow("SELECT document FROM experiment_runs WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &run)
	}
	run.Normalize()
	return run, err
}

func (r *SQLiteRepository) ListExperimentRuns(id string, offset, limit int) ([]benchmark.Run, int64, error) {
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM experiment_runs WHERE experiment_id=?", id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query("SELECT document FROM experiment_runs WHERE experiment_id=? ORDER BY updated_at DESC,id LIMIT ? OFFSET ?", id, pageLimit(limit), max(0, offset))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []benchmark.Run{}
	for rows.Next() {
		var raw string
		var run benchmark.Run
		if err = rows.Scan(&raw); err != nil {
			return nil, 0, err
		}
		if err = json.Unmarshal([]byte(raw), &run); err != nil {
			return nil, 0, err
		}
		run.Normalize()
		items = append(items, run)
	}
	return items, total, rows.Err()
}

func (r *SQLiteRepository) SaveExperimentRun(run benchmark.Run) (benchmark.Run, error) {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return run, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE experiment_runs SET revision=revision WHERE id='' "); err != nil {
		return run, err
	}
	var raw string
	var experiment benchmark.Experiment
	if err = tx.QueryRow("SELECT document FROM experiments WHERE id=?", run.ExperimentID).Scan(&raw); err != nil {
		return run, err
	}
	if err = json.Unmarshal([]byte(raw), &experiment); err != nil {
		return run, err
	}
	now := time.Now().UTC()
	previousTraces := map[string]bool{}
	if run.ID == "" {
		run.ID = uuid.NewString()
		run.Revision = 0
		run.CreatedAt = now
		run.StartedAt = nil
		run.FinishedAt = nil
		run.Task = experiment.Task
		run.TaskHash = benchmark.Hash(run.Task)
	} else {
		var old benchmark.Run
		if err = tx.QueryRow("SELECT document FROM experiment_runs WHERE id=? AND experiment_id=?", run.ID, run.ExperimentID).Scan(&raw); err != nil {
			return run, err
		}
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return run, err
		}
		if run.Revision != old.Revision {
			return run, ErrCorrelationConflict
		}
		if old.CaptureRunID != "" && run.CaptureRunID != old.CaptureRunID {
			return run, fmt.Errorf("%w: capture source is immutable", benchmark.ErrInvalid)
		}
		for _, id := range old.TraceIDs {
			previousTraces[id] = true
		}
		run.CreatedAt, run.Task, run.TaskHash = old.CreatedAt, old.Task, old.TaskHash
		run.StartedAt, run.FinishedAt = old.StartedAt, old.FinishedAt
		if old.State != run.State && old.CaptureRunID != "" {
			return run, fmt.Errorf("%w: stop the capture to complete this run", benchmark.ErrInvalid)
		}
	}
	if run.CaptureRunID != "" {
		var stopped sql.NullInt64
		var started int64
		if err = tx.QueryRow("SELECT started_at,stopped_at FROM capture_runs WHERE id=?", run.CaptureRunID).Scan(&started, &stopped); err != nil {
			return run, err
		}
		if !stopped.Valid && run.State != "running" {
			return run, fmt.Errorf("%w: capture is still running", benchmark.ErrInvalid)
		}
		start := time.UnixMilli(started).UTC()
		run.StartedAt = &start
		if stopped.Valid {
			end := time.UnixMilli(stopped.Int64).UTC()
			run.FinishedAt = &end
			run.State = "completed"
		}
	}
	if err = run.Validate(); err != nil {
		return run, err
	}
	if run.State == "running" && run.StartedAt == nil {
		run.StartedAt = &now
	}
	if run.State == "completed" && run.StartedAt != nil && run.FinishedAt == nil {
		run.FinishedAt = &now
	}
	for _, id := range run.TraceIDs {
		// Preserve source references when unprotected logs have expired, so the
		// assessment can still be edited. New references must exist.
		if previousTraces[id] {
			continue
		}
		var count int
		if err = tx.QueryRow("SELECT COUNT(*) FROM request_logs WHERE trace_id=?", id).Scan(&count); err != nil {
			return run, err
		}
		if count == 0 {
			return run, fmt.Errorf("%w: trace %s does not exist", benchmark.ErrInvalid, id)
		}
	}
	run.Revision++
	run.UpdatedAt = now
	if err = writeExperimentRun(tx, run); err != nil {
		return run, err
	}
	if _, err = tx.Exec("DELETE FROM experiment_run_traces WHERE run_id=?", run.ID); err != nil {
		return run, err
	}
	for _, id := range run.TraceIDs {
		if _, err = tx.Exec("INSERT INTO experiment_run_traces VALUES(?,?)", run.ID, id); err != nil {
			return run, err
		}
	}
	for _, table := range []string{"experiment_trace_samples", "experiment_trace_snapshots"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE run_id=? AND trace_id NOT IN (SELECT trace_id FROM experiment_run_traces WHERE run_id=?)", run.ID, run.ID); err != nil {
			return run, err
		}
	}
	if err = tx.Commit(); err != nil {
		return run, err
	}
	return run, nil
}

func writeExperimentRun(tx *sql.Tx, run benchmark.Run) error {
	raw, _ := json.Marshal(run)
	_, err := tx.Exec("INSERT INTO experiment_runs VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET capture_run_id=excluded.capture_run_id,document=excluded.document,revision=excluded.revision,updated_at=excluded.updated_at", run.ID, run.ExperimentID, run.CaptureRunID, string(raw), run.Revision, run.UpdatedAt.UnixMilli())
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fmt.Errorf("%w: capture already belongs to a run", benchmark.ErrInvalid)
	}
	return err
}

func (r *SQLiteRepository) StartExperimentCapture(id string, revision int64, upstream string) (benchmark.Run, error) {
	r.captures.mu.Lock()
	defer r.captures.mu.Unlock()
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	var run benchmark.Run
	if _, exists := r.captures.active[upstream]; exists {
		return run, ErrCorrelationConflict
	}
	tx, err := r.db.Begin()
	if err != nil {
		return run, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE experiment_runs SET revision=revision WHERE id='' "); err != nil {
		return run, err
	}
	var raw string
	if err = tx.QueryRow("SELECT document FROM experiment_runs WHERE id=?", id).Scan(&raw); err != nil {
		return run, err
	}
	if err = json.Unmarshal([]byte(raw), &run); err != nil {
		return run, err
	}
	run.Normalize()
	if run.Revision != revision || run.CaptureRunID != "" || run.State != "planned" {
		return run, ErrCorrelationConflict
	}
	now := time.Now().UTC()
	capture := CaptureRun{ID: uuid.NewString(), Name: run.Name, Upstream: upstream, StartedAt: now}
	if _, err = tx.Exec("INSERT INTO capture_runs(id,name,upstream,started_at,single_task) VALUES(?,?,?,?,0)", capture.ID, capture.Name, upstream, now.UnixMilli()); err != nil {
		return run, err
	}
	run.CaptureRunID = capture.ID
	run.State = "running"
	run.StartedAt = &now
	run.UpdatedAt = now
	run.Revision++
	if err = writeExperimentRun(tx, run); err != nil {
		return run, err
	}
	if err = tx.Commit(); err != nil {
		return run, err
	}
	r.captures.active[upstream] = capture
	return run, nil
}

func (r *SQLiteRepository) StopExperimentCapture(id string, revision int64) (benchmark.Run, error) {
	r.captures.mu.Lock()
	defer r.captures.mu.Unlock()
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	var run benchmark.Run
	tx, err := r.db.Begin()
	if err != nil {
		return run, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE experiment_runs SET revision=revision WHERE id='' "); err != nil {
		return run, err
	}
	var raw string
	if err = tx.QueryRow("SELECT document FROM experiment_runs WHERE id=?", id).Scan(&raw); err != nil {
		return run, err
	}
	if err = json.Unmarshal([]byte(raw), &run); err != nil {
		return run, err
	}
	run.Normalize()
	if run.Revision != revision || run.State != "running" || run.CaptureRunID == "" {
		return run, ErrCorrelationConflict
	}
	now := time.Now().UTC()
	if _, err = tx.Exec("UPDATE capture_runs SET stopped_at=COALESCE(stopped_at,?),revision=revision+1 WHERE id=?", now.UnixMilli(), run.CaptureRunID); err != nil {
		return run, err
	}
	run.State = "completed"
	run.FinishedAt = &now
	run.UpdatedAt = now
	run.Revision++
	if err = writeExperimentRun(tx, run); err != nil {
		return run, err
	}
	if err = tx.Commit(); err != nil {
		return run, err
	}
	for upstream, capture := range r.captures.active {
		if capture.ID == run.CaptureRunID {
			delete(r.captures.active, upstream)
		}
	}
	return run, nil
}

// ExperimentLogs returns only the metadata needed for basic comparison.
func (r *SQLiteRepository) ExperimentLogs(ctx context.Context, id string, limit int) ([]*RequestLog, error) {
	rows, err := r.db.QueryContext(ctx, `
	SELECT id,created_at,method,path,status_code,latency_ms,error,trace_id,COALESCE(websocket_info,''),usage_input_tokens,usage_output_tokens,usage_total_tokens
	FROM request_logs l WHERE `+experimentLogScope+` AND COALESCE(tag,'') NOT LIKE 'prismcat-analysis%' ORDER BY created_at_unix_ms,id LIMIT ?
	`, id, id, id, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []*RequestLog{}
	for rows.Next() {
		entry := &RequestLog{}
		var websocket string
		var input, output, total sql.NullInt64
		if err = rows.Scan(&entry.ID, &entry.CreatedAt, &entry.Method, &entry.Path, &entry.StatusCode, &entry.Latency, &entry.Error, &entry.TraceID, &websocket, &input, &output, &total); err != nil {
			return nil, err
		}
		entry.CreatedAt = entry.CreatedAt.UTC()
		entry.UsageInputTokens, entry.UsageOutputTokens, entry.UsageTotalTokens = nullIntPtr(input), nullIntPtr(output), nullIntPtr(total)
		if websocket != "" {
			_ = json.Unmarshal([]byte(websocket), &entry.WebSocket)
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

// searchBodyPreviewBytes 限制关键词检索扫描的正文前缀长度。数据库里的正文
// 可能达到数 MB;截前缀既约束 LIKE 扫描成本,又恰好覆盖保留的可读预览。
const searchBodyPreviewBytes = 4096

// SearchExperimentLogs 返回某个实验任务日志的原始行窗口,可按关键词或
// "仅失败"收窄。关键词用 LIKE(SQLite 默认对 ASCII 大小写不敏感)匹配
// 路径、错误文本与两侧正文预览;行内附同样的预览,供命中片段渲染。
// SupportedPath 过滤留在调用方,SQL 不重复协议知识;offset 是原始行
// 游标，调用方按实际消费位置推进。范围圈定与 ExperimentLogs 完全一致。
func (r *SQLiteRepository) SearchExperimentLogs(ctx context.Context, id, query string, errorsOnly bool, limit, offset int) ([]*RequestLog, error) {
	conditions := []string{
		experimentLogScope,
		"method='POST'",
		"COALESCE(tag,'') NOT LIKE 'prismcat-analysis%'",
	}
	return r.searchReviewLogs(ctx, conditions, []any{id, id, id}, query, errorsOnly, limit, offset)
}

// SearchReviewLogs 的成员来自报告快照，不依赖当前任务引用，历史追问也保持原范围。
func (r *SQLiteRepository) SearchReviewLogs(ctx context.Context, ids []string, query string, errorsOnly bool, limit, offset int) ([]*RequestLog, error) {
	if len(ids) == 0 {
		return []*RequestLog{}, ctx.Err()
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return r.searchReviewLogs(ctx, []string{"l.id IN (SELECT value FROM json_each(?))", "method='POST'", "COALESCE(tag,'') NOT LIKE 'prismcat-analysis%'"}, []any{string(raw)}, query, errorsOnly, limit, offset)
}

func (r *SQLiteRepository) searchReviewLogs(ctx context.Context, conditions []string, scopeArgs []any, query string, errorsOnly bool, limit, offset int) ([]*RequestLog, error) {
	if limit <= 0 {
		limit = 200
	}
	limit = min(limit, 1000)
	offset = max(0, offset)
	args := append([]any{searchBodyPreviewBytes, searchBodyPreviewBytes}, scopeArgs...)
	if errorsOnly {
		conditions = append(conditions, "(status_code>=400 OR COALESCE(error,'')!='')")
	}
	if query != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query) + "%"
		conditions = append(conditions, "(path LIKE ? ESCAPE '\\' OR COALESCE(error,'') LIKE ? ESCAPE '\\' OR SUBSTR(COALESCE(request_body,''),1,?) LIKE ? ESCAPE '\\' OR SUBSTR(COALESCE(response_body,''),1,?) LIKE ? ESCAPE '\\')")
		args = append(args, like, like, searchBodyPreviewBytes, like, searchBodyPreviewBytes, like)
	}
	rows, err := r.db.QueryContext(ctx, fmt.Sprintf(`
	SELECT id,created_at,method,path,status_code,latency_ms,COALESCE(error,''),trace_id,SUBSTR(COALESCE(request_body,''),1,?),SUBSTR(COALESCE(response_body,''),1,?)
	FROM request_logs l WHERE %s ORDER BY created_at_unix_ms,id LIMIT ? OFFSET ?`,
		strings.Join(conditions, " AND ")), append(args, limit, offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []*RequestLog{}
	for rows.Next() {
		entry := &RequestLog{}
		if err = rows.Scan(&entry.ID, &entry.CreatedAt, &entry.Method, &entry.Path, &entry.StatusCode, &entry.Latency, &entry.Error, &entry.TraceID, &entry.RequestBody, &entry.ResponseBody); err != nil {
			return nil, err
		}
		entry.CreatedAt = entry.CreatedAt.UTC()
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}
