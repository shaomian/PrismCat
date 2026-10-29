package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/shaomian/PrismCat/internal/storage"
)

func (h *Handler) handleTaskPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/task-previews/"), "/")
	if len(parts) != 2 || parts[1] == "" || (parts[0] != "capture" && parts[0] != "experiment") {
		h.jsonError(w, "preview not found", http.StatusNotFound)
		return
	}
	repo, ok := h.repo.(storage.TaskPreviewRepository)
	if !ok {
		h.jsonError(w, "task previews are unavailable", http.StatusNotImplemented)
		return
	}
	var value any
	var err error
	if parts[0] == "capture" {
		value, err = repo.GetCaptureRunPreview(r.Context(), parts[1])
	} else {
		value, err = repo.GetExperimentPreview(r.Context(), parts[1])
	}
	if r.Context().Err() != nil {
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, sql.ErrNoRows) {
			status = http.StatusNotFound
		}
		h.jsonError(w, err.Error(), status)
		return
	}
	h.jsonResponse(w, value)
}
