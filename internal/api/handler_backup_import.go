package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shaomian/PrismCat/internal/backup"
)

func importSameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}

func (h *Handler) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !importSameOrigin(r) {
		h.jsonError(w, "backup_import_origin", http.StatusForbidden)
		return
	}
	if h.backups == nil {
		h.jsonError(w, "backup_unavailable", http.StatusNotImplemented)
		return
	}
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/zip" {
		h.jsonError(w, "backup_import_type", http.StatusUnsupportedMediaType)
		return
	}
	// The ordinary API keeps its 30s timeout; only this bounded upload gets more
	// time. Reverse proxies must also permit the documented upload size/time.
	if err := http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Minute)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.jsonError(w, "backup_import_failed", http.StatusInternalServerError)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, backup.MaxImportBytes)
	defer r.Body.Close()
	job, err := h.backups.UploadImport(r.Context(), r.Body, r.ContentLength)
	if err != nil {
		h.importError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	h.jsonResponse(w, job)
}

func (h *Handler) handleBackupImportAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !importSameOrigin(r) {
		h.jsonError(w, "backup_import_origin", http.StatusForbidden)
		return
	}
	if h.backups == nil {
		h.jsonError(w, "backup_unavailable", http.StatusNotImplemented)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/system/backup-import/"), "/")
	if len(parts) == 1 && r.Method == http.MethodDelete {
		if err := h.backups.CancelImport(parts[0]); err != nil {
			h.importError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(parts) != 2 || parts[1] != "confirm" || r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Confirmed bool `json:"confirmed"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&body) != nil || !body.Confirmed {
		h.jsonError(w, "backup_import_confirmation", http.StatusBadRequest)
		return
	}
	job, err := h.backups.ConfirmImport(parts[0])
	if err != nil {
		h.importError(w, err)
		return
	}
	h.jsonResponse(w, job)
}

func (h *Handler) importError(w http.ResponseWriter, err error) {
	code, status := "backup_import_failed", http.StatusInternalServerError
	var sizeErr *http.MaxBytesError
	switch {
	case errors.Is(err, backup.ErrBusy):
		code, status = "backup_busy", http.StatusConflict
	case errors.Is(err, backup.ErrImportLayout):
		code, status = "backup_import_layout", http.StatusConflict
	case errors.Is(err, backup.ErrImportState):
		code, status = "backup_import_state", http.StatusConflict
	case errors.As(err, &sizeErr), err.Error() == "backup_import_size":
		code, status = "backup_import_size", http.StatusRequestEntityTooLarge
	case err.Error() == "backup_import_expired":
		code, status = "backup_import_expired", http.StatusConflict
	}
	h.jsonError(w, code, status)
}
