package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type PreviewTrace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type PreviewRequestError struct {
	StatusCode int    `json:"status_code"`
	Path       string `json:"path"`
}

type CaptureRunPreview struct {
	Traces      []PreviewTrace       `json:"traces"`
	TraceCount  int64                `json:"trace_count"`
	ErrorCount  int64                `json:"error_count"`
	LatestError *PreviewRequestError `json:"latest_error,omitempty"`
}

type PreviewRun struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Agent string `json:"agent"`
	Model string `json:"model"`
}

type ExperimentPreview struct {
	Background   string       `json:"background"`
	Tasks        []PreviewRun `json:"tasks"`
	TaskCount    int          `json:"task_count"`
	Points       []string     `json:"points"`
	PointCount   int          `json:"point_count"`
	LastReportAt *time.Time   `json:"last_report_at,omitempty"`
}

// TaskPreviewRepository 提供悬停时按需读取的短摘要，不读取日志正文或报告内容。
type TaskPreviewRepository interface {
	GetCaptureRunPreview(context.Context, string) (CaptureRunPreview, error)
	GetExperimentPreview(context.Context, string) (ExperimentPreview, error)
}

func previewExcerpt(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}

func (r *SQLiteRepository) GetCaptureRunPreview(ctx context.Context, id string) (CaptureRunPreview, error) {
	v := CaptureRunPreview{Traces: []PreviewTrace{}}
	var exists int
	if err := r.db.QueryRowContext(ctx, "SELECT 1 FROM capture_runs WHERE id=?", id).Scan(&exists); err != nil {
		return v, err
	}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT NULLIF(trace_id,'')),
		COALESCE(SUM(CASE WHEN status_code>=400 OR COALESCE(error,'')!='' THEN 1 ELSE 0 END),0)
		FROM request_logs WHERE capture_run_id=?`, id).Scan(&v.TraceCount, &v.ErrorCount); err != nil {
		return v, err
	}
	// 先限制到三条调用链，再读取其名称；查询范围始终限定于单个批次。
	rows, err := r.db.QueryContext(ctx, `WITH traces AS (
		SELECT trace_id, MIN(created_at_unix_ms) AS first_at FROM request_logs
		WHERE capture_run_id=? AND COALESCE(trace_id,'')!='' GROUP BY trace_id
		ORDER BY first_at,trace_id LIMIT 3
	) SELECT t.trace_id,COALESCE(substr(json_extract(m.document,'$.name'),1,81),'')
	FROM traces t LEFT JOIN trace_metadata m ON m.trace_id=t.trace_id ORDER BY t.first_at,t.trace_id`, id)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var trace PreviewTrace
		if err := rows.Scan(&trace.ID, &trace.Name); err != nil {
			rows.Close()
			return v, err
		}
		trace.Name = previewExcerpt(trace.Name, 80)
		v.Traces = append(v.Traces, trace)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	if v.ErrorCount > 0 {
		latest := &PreviewRequestError{}
		err := r.db.QueryRowContext(ctx, `SELECT status_code,substr(path,1,81) FROM request_logs
			WHERE capture_run_id=? AND (status_code>=400 OR COALESCE(error,'')!='')
			ORDER BY created_at_unix_ms DESC,id DESC LIMIT 1`, id).Scan(&latest.StatusCode, &latest.Path)
		if err != nil && err != sql.ErrNoRows {
			return v, err
		}
		if err == nil {
			latest.Path = previewExcerpt(latest.Path, 80)
			v.LatestError = latest
		}
	}
	return v, nil
}

func (r *SQLiteRepository) GetExperimentPreview(ctx context.Context, id string) (ExperimentPreview, error) {
	v := ExperimentPreview{Tasks: []PreviewRun{}, Points: []string{}}
	var reportAt sql.NullInt64
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(substr(json_extract(document,'$.task.prompt'),1,241),''),
		(SELECT COUNT(*) FROM experiment_runs WHERE experiment_id=experiments.id),
		COALESCE(json_array_length(document,'$.review.selected'),0),
		(SELECT created_at FROM review_reports WHERE experiment_id=experiments.id AND status='completed' ORDER BY created_at DESC,id DESC LIMIT 1)
		FROM experiments WHERE id=?`, id).Scan(&v.Background, &v.TaskCount, &v.PointCount, &reportAt)
	if err != nil {
		return v, err
	}
	v.Background = previewExcerpt(v.Background, 240)
	if reportAt.Valid && reportAt.Int64 > 0 {
		at := time.UnixMilli(reportAt.Int64).UTC()
		v.LastReportAt = &at
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,COALESCE(substr(json_extract(document,'$.name'),1,81),''),
		COALESCE(substr(json_extract(document,'$.conditions.agent'),1,41),''),COALESCE(substr(json_extract(document,'$.conditions.model'),1,61),'')
		FROM experiment_runs WHERE experiment_id=? ORDER BY updated_at DESC,id LIMIT 3`, id)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var task PreviewRun
		if err := rows.Scan(&task.ID, &task.Name, &task.Agent, &task.Model); err != nil {
			rows.Close()
			return v, err
		}
		task.Name, task.Agent, task.Model = previewExcerpt(task.Name, 80), previewExcerpt(task.Agent, 40), previewExcerpt(task.Model, 60)
		v.Tasks = append(v.Tasks, task)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return v, err
	}
	rows, err = r.db.QueryContext(ctx, `SELECT COALESCE(substr(json_extract(point.value,'$.name'),1,61),'')
		FROM experiments e,json_each(e.document,'$.review.selected') chosen
		JOIN json_each(e.document,'$.review.points') point ON json_extract(point.value,'$.id')=chosen.value
		WHERE e.id=? ORDER BY CAST(chosen.key AS INTEGER) LIMIT 4`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var point string
		if err := rows.Scan(&point); err != nil {
			return v, err
		}
		if point = previewExcerpt(point, 60); point != "" {
			v.Points = append(v.Points, point)
		}
	}
	return v, rows.Err()
}

func (a *AsyncRepository) GetCaptureRunPreview(ctx context.Context, id string) (CaptureRunPreview, error) {
	if repo, ok := a.inner.(TaskPreviewRepository); ok {
		return repo.GetCaptureRunPreview(ctx, id)
	}
	return CaptureRunPreview{}, fmt.Errorf("task previews are unavailable")
}

func (a *AsyncRepository) GetExperimentPreview(ctx context.Context, id string) (ExperimentPreview, error) {
	if repo, ok := a.inner.(TaskPreviewRepository); ok {
		return repo.GetExperimentPreview(ctx, id)
	}
	return ExperimentPreview{}, fmt.Errorf("task previews are unavailable")
}
