package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/shaomian/PrismCat/internal/storage"
)

const cleanupPreviewLifetime = 10 * time.Minute

type cleanupPreviewState struct {
	Token         string                        `json:"token"`
	Enabled       bool                          `json:"enabled"`
	RetentionDays int                           `json:"retention_days"`
	Before        time.Time                     `json:"before"`
	CreatedAt     time.Time                     `json:"-"`
	Counts        storage.ExpiredCleanupPreview `json:"counts"`
}

type cleanupJobState struct {
	ID            string     `json:"id"`
	State         string     `json:"state"`
	RetentionDays int        `json:"retention_days"`
	Before        time.Time  `json:"before"`
	DeletedLogs   int64      `json:"deleted_logs"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	Error         string     `json:"error,omitempty"`
}

type cleanupPreviewRepository interface {
	PreviewExpiredCleanup(context.Context, time.Time) (storage.ExpiredCleanupPreview, error)
}

func (h *Handler) handleCleanupPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.cleanupMu.Lock()
	busy := h.cleanupJob != nil && h.cleanupJob.State == "running"
	h.cleanupMu.Unlock()
	if busy {
		h.jsonError(w, "cleanup_busy", http.StatusConflict)
		return
	}
	preview := cleanupPreviewState{RetentionDays: h.cfg.StorageSnapshot().RetentionDays}
	if preview.RetentionDays <= 0 {
		h.jsonResponse(w, preview)
		return
	}
	if preview.RetentionDays > int((time.Duration(1<<63-1))/(24*time.Hour)) {
		h.jsonError(w, "cleanup_invalid_retention", http.StatusBadRequest)
		return
	}
	repo, ok := h.repo.(cleanupPreviewRepository)
	if !ok {
		h.jsonError(w, "cleanup_unavailable", http.StatusNotImplemented)
		return
	}
	preview.CreatedAt = time.Now().UTC()
	preview.Before = preview.CreatedAt.Add(-time.Duration(preview.RetentionDays) * 24 * time.Hour)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	counts, err := repo.PreviewExpiredCleanup(ctx, preview.Before)
	if err != nil {
		h.jsonError(w, "cleanup_preview_failed", http.StatusInternalServerError)
		return
	}
	preview.Enabled = true
	preview.Token = uuid.NewString()
	preview.Counts = counts
	h.cleanupMu.Lock()
	if h.cleanupJob != nil && h.cleanupJob.State == "running" {
		h.cleanupMu.Unlock()
		h.jsonError(w, "cleanup_busy", http.StatusConflict)
		return
	}
	h.cleanupPreview = &preview
	h.cleanupMu.Unlock()
	h.jsonResponse(w, preview)
}

func (h *Handler) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.cleanupMu.Lock()
		state := cleanupJobState{State: "idle"}
		if h.cleanupJob != nil {
			state = *h.cleanupJob
		}
		h.cleanupMu.Unlock()
		h.jsonResponse(w, state)
		return
	}
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		Token string `json:"token"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.Decode(new(any)) != io.EOF || request.Token == "" {
		h.jsonError(w, "cleanup_preview_required", http.StatusBadRequest)
		return
	}
	h.cleanupMu.Lock()
	if h.cleanupJob != nil && h.cleanupJob.State == "running" {
		h.cleanupMu.Unlock()
		h.jsonError(w, "cleanup_busy", http.StatusConflict)
		return
	}
	preview := h.cleanupPreview
	if preview == nil || preview.Token != request.Token || time.Since(preview.CreatedAt) > cleanupPreviewLifetime {
		h.cleanupMu.Unlock()
		h.jsonError(w, "cleanup_preview_expired", http.StatusConflict)
		return
	}
	retentionDays := h.cfg.StorageSnapshot().RetentionDays
	if retentionDays <= 0 {
		h.cleanupMu.Unlock()
		h.jsonError(w, "cleanup_disabled", http.StatusConflict)
		return
	}
	if retentionDays != preview.RetentionDays {
		h.cleanupMu.Unlock()
		h.jsonError(w, "cleanup_config_changed", http.StatusConflict)
		return
	}
	job := cleanupJobState{ID: uuid.NewString(), State: "running", RetentionDays: retentionDays, Before: preview.Before, StartedAt: time.Now().UTC()}
	h.cleanupJob = &job
	h.cleanupPreview = nil
	h.cleanupMu.Unlock()

	// Deleting only uses existing group retention protection. This worker neither
	// compacts SQLite nor walks blob storage on the request path.
	go h.runExpiredCleanup(job)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	h.jsonResponse(w, job)
}

func (h *Handler) runExpiredCleanup(job cleanupJobState) {
	deleted, err := h.repo.DeleteLogsBefore(job.Before)
	now := time.Now().UTC()
	job.DeletedLogs = deleted
	job.FinishedAt = &now
	job.State = "completed"
	if err != nil {
		job.State = "failed"
		job.Error = "cleanup_failed"
	}
	h.cleanupMu.Lock()
	if h.cleanupJob != nil && h.cleanupJob.ID == job.ID {
		h.cleanupJob = &job
	}
	h.cleanupMu.Unlock()
}
