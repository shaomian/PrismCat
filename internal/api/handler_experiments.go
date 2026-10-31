package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/storage"
)

func decodeExperiment(w http.ResponseWriter, r *http.Request, target any) error {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid JSON: %v", benchmark.ErrInvalid, err)
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("%w: expected a single JSON document", benchmark.ErrInvalid)
	}
	return nil
}

func (h *Handler) experimentError(w http.ResponseWriter, err error) {
	status := 500
	if errors.Is(err, sql.ErrNoRows) {
		status = 404
	}
	if errors.Is(err, benchmark.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, storage.ErrCorrelationConflict) {
		status = 409
	}
	h.jsonError(w, err.Error(), status)
}

func (h *Handler) experimentRepo(w http.ResponseWriter) storage.ExperimentRepository {
	r, ok := h.repo.(storage.ExperimentRepository)
	if !ok {
		h.jsonError(w, "experiments are unavailable", 501)
		return nil
	}
	return r
}

func (h *Handler) handleTraceMetadata(w http.ResponseWriter, r *http.Request, id string) {
	repo := h.experimentRepo(w)
	if repo == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		v, err := repo.GetTraceMetadataDocument(id)
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, v)
	case http.MethodPut:
		var v storage.TraceMetadata
		if err := decodeExperiment(w, r, &v); err != nil {
			h.experimentError(w, err)
			return
		}
		v.TraceID = id
		updated, err := repo.SaveTraceMetadata(v)
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, updated)
	default:
		h.jsonError(w, "method not allowed", 405)
	}
}

func (h *Handler) handleExperiments(w http.ResponseWriter, r *http.Request) {
	repo := h.experimentRepo(w)
	if repo == nil {
		return
	}
	switch r.Method {
	case http.MethodGet:
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		items, total, err := repo.ListExperiments(r.URL.Query().Get("q"), r.URL.Query().Get("archived") == "true", offset, limit)
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, map[string]any{"experiments": items, "total": total})
	case http.MethodPost:
		var e benchmark.Experiment
		if err := decodeExperiment(w, r, &e); err != nil {
			h.experimentError(w, err)
			return
		}
		if e.ID != "" {
			h.experimentError(w, fmt.Errorf("%w: new experiment must not supply an ID", benchmark.ErrInvalid))
			return
		}
		e, err := repo.SaveExperiment(e)
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, e)
	default:
		h.jsonError(w, "method not allowed", 405)
	}
}

func (h *Handler) handleExperimentDetail(w http.ResponseWriter, r *http.Request) {
	repo := h.experimentRepo(w)
	if repo == nil {
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/experiments/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		h.jsonError(w, "experiment not found", 404)
		return
	}
	experimentID := parts[0]
	experiment, err := repo.GetExperiment(experimentID)
	if err != nil {
		h.experimentError(w, err)
		return
	}
	if len(parts) >= 2 && parts[1] == "review" {
		h.handleExperimentReview(w, r, experiment, parts[2:])
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			runs, total, e := repo.ListExperimentRuns(experimentID, offset, limit)
			if e != nil {
				h.experimentError(w, e)
				return
			}
			h.jsonResponse(w, map[string]any{"experiment": experiment, "runs": runs, "total": total})
		case http.MethodPut:
			var e benchmark.Experiment
			if err = decodeExperiment(w, r, &e); err != nil {
				h.experimentError(w, err)
				return
			}
			e.ID = experimentID
			e, err = repo.SaveExperiment(e)
			if err != nil {
				h.experimentError(w, err)
				return
			}
			h.jsonResponse(w, e)
		default:
			h.jsonError(w, "method not allowed", 405)
		}
		return
	}
	if len(parts) == 2 && (parts[1] == "compare" || parts[1] == "export") {
		if r.Method != http.MethodGet {
			h.jsonError(w, "method not allowed", 405)
			return
		}
		ids := strings.Split(r.URL.Query().Get("runs"), ",")
		if len(ids) < 2 || len(ids) > 8 {
			h.experimentError(w, fmt.Errorf("%w: select 2–8 runs", benchmark.ErrInvalid))
			return
		}
		results := []benchmark.Result{}
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				h.experimentError(w, fmt.Errorf("%w: duplicate run", benchmark.ErrInvalid))
				return
			}
			seen[id] = true
			run, e := repo.GetExperimentRun(id)
			if e != nil {
				h.experimentError(w, e)
				return
			}
			if run.ExperimentID != experimentID {
				h.jsonError(w, "run not found in this experiment", 404)
				return
			}
			result, e := h.measureExperimentRun(r, repo, run)
			if e != nil {
				h.experimentError(w, e)
				return
			}
			results = append(results, result)
		}
		comparison := benchmark.Compare(experiment, results)
		if checker, ok := repo.(interface {
			ExperimentRunsOverlap(context.Context, []string) (bool, error)
		}); ok {
			overlap, err := checker.ExperimentRunsOverlap(r.Context(), ids)
			if err != nil {
				h.experimentError(w, err)
				return
			}
			if overlap {
				comparison.Warnings = append(comparison.Warnings, "overlapping_logs")
			}
		}
		// 与 publicReview 同策略:证据清单留在服务端(复盘 AI 用内存中的全量
		// LogIDs 分页检索),对比/导出响应只携带紧凑指标,避免超大任务的响应
		// 被数万个日志 ID 撑到数 MB
		for i := range comparison.Results {
			comparison.Results[i].Metrics.LogIDs = []string{}
		}
		if parts[1] == "export" {
			w.Header().Set("Content-Disposition", `attachment; filename="prismcat-experiment.json"`)
		}
		h.jsonResponse(w, comparison)
		return
	}
	if parts[1] != "runs" {
		h.jsonError(w, "not found", 404)
		return
	}
	if len(parts) == 2 {
		if r.Method != http.MethodPost {
			h.jsonError(w, "method not allowed", 405)
			return
		}
		var run benchmark.Run
		if err = decodeExperiment(w, r, &run); err != nil {
			h.experimentError(w, err)
			return
		}
		if run.ID != "" {
			h.experimentError(w, fmt.Errorf("%w: new run must not supply an ID", benchmark.ErrInvalid))
			return
		}
		run.ExperimentID = experimentID
		run, err = repo.SaveExperimentRun(run)
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, run)
		return
	}
	run, err := repo.GetExperimentRun(parts[2])
	if err != nil {
		h.experimentError(w, err)
		return
	}
	if run.ExperimentID != experimentID {
		h.jsonError(w, "run not found", 404)
		return
	}
	if len(parts) == 3 {
		switch r.Method {
		case http.MethodGet:
			h.jsonResponse(w, run)
		case http.MethodPut:
			var updated benchmark.Run
			if err = decodeExperiment(w, r, &updated); err != nil {
				h.experimentError(w, err)
				return
			}
			updated.ID = run.ID
			updated.ExperimentID = experimentID
			updated, err = repo.SaveExperimentRun(updated)
			if err != nil {
				h.experimentError(w, err)
				return
			}
			h.jsonResponse(w, updated)
		default:
			h.jsonError(w, "method not allowed", 405)
		}
		return
	}
	if len(parts) == 4 && (parts[3] == "start" || parts[3] == "stop") {
		if r.Method != http.MethodPost {
			h.jsonError(w, "method not allowed", 405)
			return
		}
		var req struct {
			Revision int64  `json:"revision"`
			Upstream string `json:"upstream"`
		}
		if err = decodeExperiment(w, r, &req); err != nil {
			h.experimentError(w, err)
			return
		}
		if parts[3] == "start" {
			upstream, ok := h.cfg.GetUpstream(req.Upstream)
			if !ok || upstream.LoggingDisabled {
				h.experimentError(w, fmt.Errorf("%w: select an upstream with logging enabled", benchmark.ErrInvalid))
				return
			}
			run, err = repo.StartExperimentCapture(run.ID, req.Revision, req.Upstream)
		} else {
			run, err = repo.StopExperimentCapture(run.ID, req.Revision)
		}
		if err != nil {
			h.experimentError(w, err)
			return
		}
		h.jsonResponse(w, run)
		return
	}
	h.jsonError(w, "not found", 404)
}

// Statistics consume all captured model metadata; evidence membership is frozen
// separately when an AI report is created.
func (h *Handler) measureExperimentRun(r *http.Request, repo storage.ExperimentRepository, run benchmark.Run) (benchmark.Result, error) {
	metrics, err := storage.FullExperimentMetrics(r.Context(), repo, run.ID)
	if err != nil {
		return benchmark.Result{}, err
	}
	return benchmark.Result{Run: run, Metrics: metrics}, nil
}
