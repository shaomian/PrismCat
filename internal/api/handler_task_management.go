package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/shaomian/PrismCat/internal/storage"
)

func (h *Handler) handleTaskManagement(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/task-management/")
	if action != "preview" && action != "apply" {
		h.jsonError(w, "not found", 404)
		return
	}
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", 405)
		return
	}
	var request storage.TaskManagementRequest
	if err := decodeExperiment(w, r, &request); err != nil {
		h.experimentError(w, err)
		return
	}
	if err := request.Validate(); err != nil {
		h.experimentError(w, err)
		return
	}
	repo, ok := h.repo.(storage.TaskManagementRepository)
	if !ok {
		h.jsonError(w, "task management unavailable", 501)
		return
	}
	// Serialize card management against review acquisition, including the short
	// preparation window before a running report is persisted.
	h.reviewMu.Lock()
	defer h.reviewMu.Unlock()
	preview, err := repo.PreviewTaskManagement(r.Context(), request)
	if err != nil {
		h.experimentError(w, err)
		return
	}
	if request.Action == "delete" || request.Action == "archive" {
		for i := range preview.Items {
			id := preview.Items[i].ID
			if request.Kind == "run" {
				id = request.ExperimentID
			}
			if (request.Kind == "experiment" || request.Kind == "run") && h.reviewActive[id] {
				preview.Items[i].BlockedReason = "task_active"
				preview.CanApply = false
			}
		}
	}
	if action == "preview" {
		h.jsonResponse(w, preview)
		return
	}
	if !preview.CanApply {
		h.taskManagementBlocked(w, preview)
		return
	}
	result, err := repo.ApplyTaskManagement(r.Context(), request)
	if err != nil {
		var blocked *storage.TaskManagementBlockedError
		if errors.As(err, &blocked) {
			h.taskManagementBlocked(w, blocked.Preview)
			return
		}
		h.experimentError(w, err)
		return
	}
	if request.Kind == "trace" && request.Action == "delete" && h.tlCache != nil {
		ids := make([]string, 0, len(request.Items))
		for _, item := range request.Items {
			ids = append(ids, item.ID)
		}
		h.tlCache.invalidate(ids)
	}
	h.jsonResponse(w, result)
}
func (h *Handler) taskManagementBlocked(w http.ResponseWriter, preview storage.TaskManagementPreview) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	h.jsonResponse(w, map[string]any{"error": "task_management_blocked", "preview": preview})
}
