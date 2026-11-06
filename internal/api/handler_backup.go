package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/shaomian/PrismCat/internal/backup"
)

func (h *Handler) CloseBackups() {
	if h.backups != nil {
		h.backups.Close()
	}
}

func (h *Handler) handleBackups(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.backups == nil {
		h.jsonError(w, "backup_unavailable", http.StatusNotImplemented)
		return
	}
	switch r.Method {
	case http.MethodGet:
		jobs, err := h.backups.List()
		if err != nil {
			h.jsonError(w, "backup_list_failed", http.StatusInternalServerError)
			return
		}
		imported, importErr := h.backups.ImportStatus()
		importError := ""
		if importErr != nil {
			importError = "backup_import_state"
		}
		h.jsonResponse(w, map[string]any{"backups": jobs, "cleanup_paused": h.cfg.StorageSnapshot().CleanupPaused, "import_job": imported, "import_supported": h.backups.ImportSupported(), "import_error": importError})
	case http.MethodPost:
		job, err := h.backups.Create()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, backup.ErrBusy) {
				status = http.StatusConflict
			}
			h.jsonError(w, "backup_create_failed", status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		h.jsonResponse(w, job)
	default:
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleBackupDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.backups == nil {
		h.jsonError(w, "backup_unavailable", http.StatusNotImplemented)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/system/backups/"), "/")
	if len(parts) == 2 && parts[1] == "download" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		file, err := h.backups.Open(parts[0])
		if err != nil {
			h.jsonError(w, "backup_download_unavailable", http.StatusNotFound)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			h.jsonError(w, "backup_download_unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="prismcat-backup-`+parts[0]+`.zip"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, info.Name(), info.ModTime(), file)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		err := h.backups.Delete(parts[0])
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, backup.ErrBusy) {
				status = http.StatusConflict
			}
			if errors.Is(err, backup.ErrNotFound) {
				status = http.StatusNotFound
			}
			h.jsonError(w, "backup_delete_failed", status)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	job, err := h.backups.Get(parts[0])
	if err != nil {
		h.jsonError(w, "backup_not_found", http.StatusNotFound)
		return
	}
	h.jsonResponse(w, job)
}

func (h *Handler) handleCleanupResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.cfg.ResumeCleanup(); err != nil {
		h.jsonError(w, "cleanup_resume_failed", http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, map[string]bool{"cleanup_paused": false})
}
