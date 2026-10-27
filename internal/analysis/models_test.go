package analysis

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/config"
)

func TestListModelsAuthenticationPaginationAndDeduplication(t *testing.T) {
	for _, protocol := range []string{"openai", "anthropic"} {
		t.Run(protocol, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/proxy/vendor/v1/models" || r.Header.Get("X-PrismCat-Tag") != "prismcat-analysis" {
					t.Error("incorrect model-list request")
				}
				if protocol == "openai" {
					if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("x-api-key") != "" {
						t.Error("incorrect OpenAI authentication")
					}
					fmt.Fprint(w, `{"data":[{"id":"b"},{"id":"a"},{"id":"a"},{"id":""}]}`)
					return
				}
				if r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") != anthropicVersion || r.Header.Get("Authorization") != "" {
					t.Error("incorrect native authentication")
				}
				if calls == 1 {
					fmt.Fprint(w, `{"data":[{"id":"b","display_name":"Model B"}],"has_more":true,"last_id":"b"}`)
				} else {
					if r.URL.Query().Get("after_id") != "b" {
						t.Error("pagination cursor missing")
					}
					fmt.Fprint(w, `{"data":[{"id":"a"},{"id":"b"}],"has_more":false}`)
				}
			}))
			defer server.Close()
			list, err := ListModels(context.Background(), config.AnalysisConfig{Protocol: protocol, BaseURL: server.URL + "/proxy/vendor/v1/messages", APIKey: "test-key"})
			if err != nil || len(list.Models) != 2 || list.Models[0].ID != "a" || list.Models[1].ID != "b" || list.Truncated {
				t.Fatalf("invalid model list: %+v %v", list, err)
			}
			if protocol == "anthropic" && (calls != 2 || list.Models[1].Name != "Model B") {
				t.Fatal("native pagination or display name lost")
			}
		})
	}
}

func TestListModelsErrorsAndRedirectsDoNotLeakCredentials(t *testing.T) {
	for _, tc := range []struct {
		status     int
		body, code string
	}{
		{401, "echoed-secret", "analysis_models_unauthorized"},
		{404, "echoed-secret", "analysis_models_unsupported"},
		{500, "echoed-secret", "analysis_models_failed"},
		{200, `{"unexpected":[]}`, "analysis_models_invalid_response"},
		{200, strings.Repeat("x", (2<<20)+1), "analysis_models_invalid_response"},
	} {
		t.Run(fmt.Sprint(tc.status, tc.code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			_, err := ListModels(context.Background(), config.AnalysisConfig{BaseURL: server.URL, APIKey: "echoed-secret"})
			if err == nil || err.Error() != tc.code {
				t.Fatalf("unsafe or wrong error: %v", err)
			}
		})
	}
	redirected := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected = true }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, 302) }))
	defer server.Close()
	_, err := ListModels(context.Background(), config.AnalysisConfig{Protocol: "anthropic", BaseURL: server.URL, APIKey: "secret"})
	if err == nil || redirected {
		t.Fatal("redirect forwarded a native credential")
	}
}

func TestModelDestinationAndEndpointCompatibility(t *testing.T) {
	for _, base := range []string{"https://example.com", "https://example.com/v1", "https://example.com/v1/chat/completions", "https://example.com/v1/messages/"} {
		if ModelsURL(base) != "https://example.com/v1/models" {
			t.Fatalf("incorrect endpoint for %s", base)
		}
		if !SameModelDestination(base, "https://example.com/v1") {
			t.Fatal("equivalent API roots did not match")
		}
	}
	for _, base := range []string{"file:///tmp/models", "https://user:secret@example.com/v1", "https://example.com/v1#fragment"} {
		if ModelsURL(base) != "" {
			t.Fatal("invalid API address accepted")
		}
	}
	if SameModelDestination("https://example.com/_proxy/a", "https://example.com/_proxy/b") || SameModelDestination("http://example.com", "https://example.com") {
		t.Fatal("different credential destinations matched")
	}
}

func TestListModelsCancellationDuringResponseBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	_, err := ListModels(ctx, config.AnalysisConfig{BaseURL: server.URL})
	if err == nil || err.Error() != "analysis_models_timeout" {
		t.Fatalf("canceled model sync was not stopped: %v", err)
	}
}
