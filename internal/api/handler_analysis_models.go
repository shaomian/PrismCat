package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shaomian/PrismCat/internal/analysis"
	"github.com/shaomian/PrismCat/internal/config"
)

func (h *Handler) handleAnalysisModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var draft struct {
		Protocol string `json:"protocol"`
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&draft); err != nil || len(draft.BaseURL) > 2048 || len(draft.APIKey) > 8192 {
		h.jsonError(w, "analysis_models_invalid_url", http.StatusBadRequest)
		return
	}
	protocol, err := config.NormalizeAnalysisProtocol(draft.Protocol)
	if err != nil {
		h.jsonError(w, "analysis_invalid_protocol", http.StatusBadRequest)
		return
	}
	cfg := config.AnalysisConfig{Protocol: protocol, BaseURL: strings.TrimSpace(draft.BaseURL), APIKey: strings.TrimSpace(draft.APIKey)}
	saved := h.cfg.AnalysisSnapshot()
	if cfg.APIKey == "" && analysis.SameModelDestination(cfg.BaseURL, saved.BaseURL) {
		cfg.APIKey = saved.APIKey
	}
	models, err := analysis.ListModels(r.Context(), cfg)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	h.jsonResponse(w, models)
}
