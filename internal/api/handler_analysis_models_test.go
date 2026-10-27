package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/config"
)

func TestAnalysisModelSyncDraftCredentialScopeAndNoSave(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "saved-secret" {
			t.Error("saved key not used for the configured destination")
		}
		fmt.Fprint(w, `{"data":[{"id":"native-model","display_name":"Native Model"}]}`)
	}))
	defer server.Close()
	cfg := &config.Config{Analysis: config.AnalysisConfig{Protocol: "anthropic", BaseURL: server.URL + "/v1/messages", APIKey: "saved-secret", Model: "original"}}
	before := cfg.AnalysisSnapshot()
	mux := http.NewServeMux()
	New(cfg, nil, nil, nil).RegisterRoutes(mux)
	rr := reviewRequest(mux, "POST", "/api/analysis/models", map[string]string{"protocol": "anthropic", "base_url": server.URL})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "native-model") || strings.Contains(rr.Body.String(), "saved-secret") {
		t.Fatalf("sync failed or leaked key: %s", rr.Body)
	}
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("saved key sent to an unsaved destination")
		}
		fmt.Fprint(w, `{"data":[]}`)
	}))
	defer foreign.Close()
	rr = reviewRequest(mux, "POST", "/api/analysis/models", map[string]string{"protocol": "anthropic", "base_url": foreign.URL})
	if rr.Code != 200 || !reflect.DeepEqual(before, cfg.AnalysisSnapshot()) {
		t.Fatal("sync modified saved configuration")
	}
	rr = reviewRequest(mux, "PUT", "/api/config", map[string]any{"analysis": map[string]any{"protocol": "invalid", "model": "must-not-save"}})
	if rr.Code != 400 || !reflect.DeepEqual(before, cfg.AnalysisSnapshot()) {
		t.Fatal("invalid protocol mutated configuration")
	}
}
