package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/shaomian/PrismCat/internal/config"
	"github.com/shaomian/PrismCat/internal/storage"
)

func (h *Handler) handleCorrelationStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "method not allowed", 405)
		return
	}
	status := map[string]any{"queued_bytes": 0, "dropped_observations": 0}
	if repo, ok := h.repo.(interface{ CorrelationStatus() map[string]any }); ok {
		for key, value := range repo.CorrelationStatus() {
			status[key] = value
		}
	}
	// 归一化后展示:未经 Load 的原始配置(0 值)也能显示生效值
	snapshot := config.NormalizeCorrelation(h.cfg.CorrelationSnapshot())
	status["enabled"] = snapshot.Enabled
	// 内存边界的当前生效值,供设置页展示与编辑
	status["max_entries"] = snapshot.MaxEntries
	status["idle_minutes"] = snapshot.IdleMinutes
	type activeCapture struct {
		ID       string `json:"id"`
		Upstream string `json:"upstream"`
	}
	active := []activeCapture{}
	if repo, ok := h.repo.(interface{ ActiveCaptureRuns() []storage.CaptureRun }); ok {
		for _, run := range repo.ActiveCaptureRuns() {
			active = append(active, activeCapture{ID: run.ID, Upstream: run.Upstream})
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	status["active_captures"] = active
	h.jsonResponse(w, status)
}

func (h *Handler) handleCaptureRuns(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.repo.(storage.CorrelationRepository)
	if !ok {
		h.jsonError(w, "capture recording is unavailable", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if raw := r.URL.Query().Get("archived"); raw != "" {
			archived, err := strconv.ParseBool(raw)
			if err != nil {
				h.jsonError(w, "invalid archived filter", 400)
				return
			}
			if pager, ok := h.repo.(interface {
				ListCaptureRunsPageArchived(int, int, bool) ([]storage.CaptureRun, int, error)
			}); ok {
				limit, offset := 100, 0
				if raw := r.URL.Query().Get("limit"); raw != "" {
					limit, err = strconv.Atoi(raw)
					if err != nil || limit < 1 || limit > 100 {
						h.jsonError(w, "invalid capture pagination", 400)
						return
					}
				}
				if raw := r.URL.Query().Get("offset"); raw != "" {
					offset, err = strconv.Atoi(raw)
					if err != nil || offset < 0 {
						h.jsonError(w, "invalid capture pagination", 400)
						return
					}
				}
				runs, total, err := pager.ListCaptureRunsPageArchived(limit, offset, archived)
				if err != nil {
					h.jsonError(w, err.Error(), 500)
					return
				}
				h.jsonResponse(w, map[string]any{"runs": runs, "total": total, "limit": limit, "offset": offset})
				return
			}
		}
		if pager, ok := h.repo.(interface {
			ListCaptureRunsPage(int, int) ([]storage.CaptureRun, int, error)
		}); ok {
			limit, offset := 100, 0
			for key, target := range map[string]*int{"limit": &limit, "offset": &offset} {
				if raw := r.URL.Query().Get(key); raw != "" {
					value, err := strconv.Atoi(raw)
					if err != nil || value < 0 || (key == "limit" && (value < 1 || value > 100)) {
						h.jsonError(w, "invalid capture pagination", http.StatusBadRequest)
						return
					}
					*target = value
				}
			}
			runs, total, err := pager.ListCaptureRunsPage(limit, offset)
			if err != nil {
				h.jsonError(w, err.Error(), 500)
				return
			}
			h.jsonResponse(w, map[string]any{"runs": runs, "total": total, "limit": limit, "offset": offset})
			return
		}
		runs, err := repo.ListCaptureRuns()
		if err != nil {
			h.jsonError(w, err.Error(), 500)
			return
		}
		h.jsonResponse(w, map[string]any{"runs": runs})
	case http.MethodPost:
		var req struct {
			Name       string `json:"name"`
			Upstream   string `json:"upstream"`
			SingleTask bool   `json:"single_task"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
			h.jsonError(w, "invalid capture request", 400)
			return
		}
		if upstream, ok := h.cfg.GetUpstream(req.Upstream); !ok || upstream.LoggingDisabled {
			h.jsonError(w, "select an existing upstream with logging enabled", 400)
			return
		}
		if req.SingleTask && !h.cfg.CorrelationSnapshot().Enabled {
			h.jsonError(w, "enable trace correlation before recording as a single task", 400)
			return
		}
		run, err := repo.StartCaptureRun(req.Name, req.Upstream, req.SingleTask)
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusConflict)
			return
		}
		h.jsonResponse(w, run)
	default:
		h.jsonError(w, "method not allowed", 405)
	}
}

func (h *Handler) handleStopCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", 405)
		return
	}
	id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/api/capture-runs/"), "/stop")
	if !ok || id == "" {
		h.jsonError(w, "capture not found", 404)
		return
	}
	repo, ok := h.repo.(storage.CorrelationRepository)
	if !ok {
		h.jsonError(w, "capture recording is unavailable", 501)
		return
	}
	if err := repo.StopCaptureRun(id); err != nil {
		status := 500
		if errors.Is(err, sql.ErrNoRows) {
			status = 404
		}
		h.jsonError(w, err.Error(), status)
		return
	}
	h.jsonResponse(w, map[string]string{"status": "ok"})
}

func (h *Handler) handleTraceAssignment(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method == http.MethodGet {
		var entry *storage.RequestLog
		var err error
		if repo, ok := h.repo.(interface {
			GetTraceMetadata(string) (*storage.RequestLog, error)
		}); ok {
			entry, err = repo.GetTraceMetadata(id)
		} else {
			entry, err = h.repo.GetLog(id)
		}
		if err != nil {
			status := 500
			if errors.Is(err, sql.ErrNoRows) {
				status = 404
			}
			h.jsonError(w, err.Error(), status)
			return
		}
		h.jsonResponse(w, map[string]any{"trace_id": entry.TraceID, "trace_seq": entry.TraceSeq, "trace_association": entry.TraceAssociation, "capture_run_id": entry.CaptureRunID})
		return
	}
	if r.Method != http.MethodPatch {
		h.jsonError(w, "method not allowed", 405)
		return
	}
	var req struct {
		TraceID  string `json:"trace_id"`
		Revision int64  `json:"revision"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req) != nil {
		h.jsonError(w, "invalid assignment", 400)
		return
	}
	if len(req.TraceID) > 200 {
		h.jsonError(w, "trace ID too long", 400)
		return
	}
	repo, ok := h.repo.(storage.CorrelationRepository)
	if !ok {
		h.jsonError(w, "trace correction is unavailable", 501)
		return
	}
	assignment, err := repo.AssignTrace(id, strings.TrimSpace(req.TraceID), req.Revision)
	if err != nil {
		status := 500
		if errors.Is(err, sql.ErrNoRows) {
			status = 404
		}
		if errors.Is(err, storage.ErrCorrelationConflict) {
			status = 409
		}
		h.jsonError(w, err.Error(), status)
		return
	}
	h.jsonResponse(w, assignment)
}
