package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/benchmark"
)

type ReviewRepository interface {
	ListReviewTemplates(string, int, int) ([]benchmark.ReviewTemplateSummary, int64, error)
	GetReviewTemplate(string) (benchmark.ReviewTemplate, error)
	SaveReviewTemplate(benchmark.ReviewTemplate) (benchmark.ReviewTemplate, error)
	ListReviewReports(string, int, int) ([]benchmark.ReviewReportSummary, int64, error)
	GetReviewReport(string) (benchmark.ReviewReport, error)
	CreateReviewReport(benchmark.ReviewReport) error
	FinishReviewReport(benchmark.ReviewReport) error
}

func (r *SQLiteRepository) migrateReviews() error {
	_, err := r.db.Exec(`CREATE TABLE IF NOT EXISTS review_templates(id TEXT PRIMARY KEY,document TEXT NOT NULL,revision INTEGER NOT NULL,archived INTEGER NOT NULL,updated_at INTEGER NOT NULL);
 CREATE INDEX IF NOT EXISTS idx_review_templates_recent ON review_templates(archived,updated_at DESC);
 CREATE TABLE IF NOT EXISTS review_reports(id TEXT PRIMARY KEY,experiment_id TEXT NOT NULL,status TEXT NOT NULL,model TEXT NOT NULL,question TEXT NOT NULL,parent_id TEXT NOT NULL,created_at INTEGER NOT NULL,document TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS review_card_logs(experiment_id TEXT NOT NULL,log_id TEXT NOT NULL,PRIMARY KEY(experiment_id,log_id));
 CREATE INDEX IF NOT EXISTS idx_review_card_log ON review_card_logs(log_id);
 CREATE INDEX IF NOT EXISTS idx_review_reports_card ON review_reports(experiment_id,created_at DESC,id DESC);
 CREATE UNIQUE INDEX IF NOT EXISTS idx_review_reports_active ON review_reports(experiment_id) WHERE status='running';
 UPDATE review_reports SET status='interrupted',document=json_set(document,'$.status','interrupted') WHERE status='running';`)
	return err
}
func (r *SQLiteRepository) GetReviewTemplate(id string) (benchmark.ReviewTemplate, error) {
	var v benchmark.ReviewTemplate
	var raw string
	err := r.db.QueryRow("SELECT document FROM review_templates WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (r *SQLiteRepository) ListReviewTemplates(q string, offset, limit int) ([]benchmark.ReviewTemplateSummary, int64, error) {
	return r.ListReviewTemplatesArchived(q, false, offset, limit)
}
func (r *SQLiteRepository) ListReviewTemplatesArchived(q string, archived bool, offset, limit int) ([]benchmark.ReviewTemplateSummary, int64, error) {
	args := []any{archived, "%" + q + "%"}
	where := "WHERE archived=? AND json_extract(document,'$.name') LIKE ?"
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM review_templates "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query("SELECT id,json_extract(document,'$.name'),json_extract(document,'$.description'),json_array_length(json_extract(document,'$.config.selected')),revision,archived FROM review_templates "+where+" ORDER BY updated_at DESC,id LIMIT ? OFFSET ?", append(args, pageLimit(limit), max(offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	values := []benchmark.ReviewTemplateSummary{}
	for rows.Next() {
		var v benchmark.ReviewTemplateSummary
		if err = rows.Scan(&v.ID, &v.Name, &v.Description, &v.PointCount, &v.Revision, &v.Archived); err != nil {
			return nil, 0, err
		}
		values = append(values, v)
	}
	return values, total, rows.Err()
}
func (r *SQLiteRepository) SaveReviewTemplate(v benchmark.ReviewTemplate) (benchmark.ReviewTemplate, error) {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" || len(v.Name) > 300 || len(v.Description) > 1200 {
		return v, fmt.Errorf("%w: template name missing or too long", benchmark.ErrInvalid)
	}
	if err := v.Config.Validate(); err != nil {
		return v, err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	tx, err := r.db.Begin()
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE review_templates SET revision=revision WHERE id='' "); err != nil {
		return v, err
	}
	now := time.Now().UTC()
	if v.ID == "" {
		v.ID = uuid.NewString()
		v.CreatedAt = now
		v.Revision = 0
	} else {
		var raw string
		var old benchmark.ReviewTemplate
		if err = tx.QueryRow("SELECT document FROM review_templates WHERE id=?", v.ID).Scan(&raw); err != nil {
			return v, err
		}
		if err = json.Unmarshal([]byte(raw), &old); err != nil {
			return v, err
		}
		if old.Revision != v.Revision {
			return v, ErrCorrelationConflict
		}
		v.CreatedAt = old.CreatedAt
	}
	v.Revision++
	v.UpdatedAt = now
	v.Config.TemplateID = v.ID
	v.Config.TemplateName = v.Name
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	_, err = tx.Exec("INSERT INTO review_templates VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document,revision=excluded.revision,archived=excluded.archived,updated_at=excluded.updated_at", v.ID, string(raw), v.Revision, v.Archived, now.UnixMilli())
	if err == nil {
		err = tx.Commit()
	}
	return v, err
}
func (r *SQLiteRepository) GetReviewReport(id string) (benchmark.ReviewReport, error) {
	var v benchmark.ReviewReport
	var raw string
	err := r.db.QueryRow("SELECT document FROM review_reports WHERE id=?", id).Scan(&raw)
	if err == nil {
		err = json.Unmarshal([]byte(raw), &v)
	}
	return v, err
}
func (r *SQLiteRepository) ListReviewReports(id string, offset, limit int) ([]benchmark.ReviewReportSummary, int64, error) {
	var total int64
	if err := r.db.QueryRow("SELECT COUNT(*) FROM review_reports WHERE experiment_id=?", id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.Query("SELECT id,parent_id,question,status,model,created_at FROM review_reports WHERE experiment_id=? ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", id, pageLimit(limit), max(offset, 0))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	values := []benchmark.ReviewReportSummary{}
	for rows.Next() {
		var v benchmark.ReviewReportSummary
		var at int64
		if err = rows.Scan(&v.ID, &v.ParentID, &v.Question, &v.Status, &v.Model, &at); err != nil {
			return nil, 0, err
		}
		v.CreatedAt = time.UnixMilli(at).UTC()
		values = append(values, v)
	}
	return values, total, rows.Err()
}
func (r *SQLiteRepository) CreateReviewReport(v benchmark.ReviewReport) error {
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
	if _, err = tx.Exec("UPDATE experiments SET revision=revision WHERE id=''"); err != nil {
		return err
	}
	// Revalidate membership after preparing a report: a card/run or its source
	// may have been removed while AI context was being assembled.
	var exists int
	if err = tx.QueryRow("SELECT COUNT(*) FROM experiments WHERE id=?", v.ExperimentID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	logIDs := []string{}
	seen := map[string]bool{}
	for _, task := range v.Tasks {
		if v.ParentID == "" && task.RunID != "" {
			if err = tx.QueryRow("SELECT COUNT(*) FROM experiment_runs WHERE id=? AND experiment_id=?", task.RunID, v.ExperimentID).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				return ErrCorrelationConflict
			}
		}
		for _, id := range task.Metrics.LogIDs {
			if !seen[id] {
				seen[id] = true
				logIDs = append(logIDs, id)
			}
		}
	}
	if len(logIDs) > 0 {
		ids, _ := json.Marshal(logIDs)
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM json_each(?) v JOIN deleted_log_ids d ON d.id=v.value)", string(ids)).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return ErrCorrelationConflict
		}
	}
	_, err = tx.Exec("INSERT INTO review_reports VALUES(?,?,?,?,?,?,?,?)", v.ID, v.ExperimentID, v.Status, v.Model, v.Question, v.ParentID, v.CreatedAt.UnixMilli(), string(raw))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrCorrelationConflict
		}
		return err
	}
	statement, err := tx.Prepare("INSERT INTO review_card_logs VALUES(?,?) ON CONFLICT(experiment_id,log_id) DO NOTHING")
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, task := range v.Tasks {
		for _, id := range task.Metrics.LogIDs {
			if _, err = statement.Exec(v.ExperimentID, id); err != nil {
				return err
			}
		}
	}
	err = tx.Commit()
	return err
}
func (r *SQLiteRepository) FinishReviewReport(v benchmark.ReviewReport) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	result, err := r.db.Exec("UPDATE review_reports SET status=?,document=? WHERE id=? AND status='running'", v.Status, string(raw), v.ID)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
