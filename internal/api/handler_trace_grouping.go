package api

import (
	"encoding/json"
	"errors"
	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/storage"
	"net/http"
)

func (h *Handler) handleTraceGrouping(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", 405)
		return
	}
	repo, ok := h.repo.(storage.TraceGroupingRepository)
	if !ok {
		h.jsonError(w, "trace grouping unavailable", 501)
		return
	}
	var v storage.TraceGroupingRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20)).Decode(&v); err != nil {
		h.jsonError(w, "invalid trace grouping request", 400)
		return
	}
	var result any
	var err error
	switch r.URL.Path {
	case "/api/trace-grouping/preview":
		result, err = repo.PreviewTraceGrouping(r.Context(), v)
	case "/api/trace-grouping/apply":
		result, err = repo.ApplyTraceGrouping(r.Context(), v)
	default:
		h.jsonError(w, "not found", 404)
		return
	}
	if err != nil {
		status := 500
		if errors.Is(err, benchmark.ErrInvalid) {
			status = 400
		}
		if errors.Is(err, storage.ErrCorrelationConflict) {
			status = 409
		}
		h.jsonError(w, err.Error(), status)
		return
	}
	h.jsonResponse(w, result)
}
