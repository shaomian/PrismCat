package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/benchmark"
	"github.com/shaomian/PrismCat/internal/config"
)

func nativeReviewText(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	reviewSSE(w, map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 20, "output_tokens": 1}}})
	reviewSSE(w, map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}})
	reviewSSE(w, map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}})
	reviewSSE(w, map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 8}})
	reviewSSE(w, map[string]any{"type": "message_stop"})
}

func TestAnthropicTraceReviewAndSuggestions(t *testing.T) {
	repo, experiment, runs := reviewAPISeed(t)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "native-key" {
			t.Error("wrong native request")
		}
		var payload struct {
			System   string          `json:"system"`
			Messages json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if strings.Contains(payload.System, "Suggest up to five") {
			nativeReviewText(w, `{"suggestions":[{"name":"Observed verification","instructions":"Read the actual evidence"}]}`)
			return
		}
		if !strings.Contains(string(payload.Messages), `"type":"tool_result"`) {
			fmt.Fprint(w, `{"type":"message","content":[{"type":"tool_use","id":"read-a","name":"get_request_detail","input":{"log_id":"a","part":"response"}}],"stop_reason":"tool_use","usage":{"input_tokens":10,"output_tokens":5}}`)
			return
		}
		if !strings.Contains(string(payload.Messages), "This is recorded evidence") {
			t.Error("native tool result lost")
		}
		if strings.Contains(payload.System, "task review assistant") {
			output := benchmark.ReviewOutput{Summary: "Native review completed", Observations: []benchmark.ReviewObservation{{PointID: "finding", Findings: []benchmark.ReviewFinding{{RunID: runs[0].ID, Value: "recorded claim", Source: "logs", Evidence: []string{"a"}}}}}}
			raw, _ := json.Marshal(output)
			nativeReviewText(w, string(raw))
		} else {
			nativeReviewText(w, "Native trace analysis completed")
		}
	}))
	defer model.Close()
	cfg := &config.Config{Analysis: config.AnalysisConfig{Enabled: true, Protocol: "anthropic", BaseURL: model.URL, APIKey: "native-key", Model: "native-model"}}
	mux := http.NewServeMux()
	New(cfg, repo, nil, nil).RegisterRoutes(mux)
	trace := reviewRequest(mux, "POST", "/api/analysis/chat", map[string]any{"trace_id": "a", "messages": []map[string]string{{"role": "user", "content": "Explain the trace"}}})
	if trace.Code != 200 || !strings.Contains(trace.Body.String(), "Native trace analysis completed") || !strings.Contains(trace.Body.String(), `"done":true`) {
		t.Fatalf("native trace failed: %s", trace.Body)
	}
	base := "/api/experiments/" + experiment.ID + "/review/"
	rr := reviewRequest(mux, "POST", base+"analyze", map[string]any{"revision": experiment.Revision, "run_ids": []string{runs[0].ID, runs[1].ID}})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"type":"done"`) {
		t.Fatalf("native review failed: %s", rr.Body)
	}
	summaries, _, _ := repo.ListReviewReports(experiment.ID, 0, 10)
	report, err := repo.GetReviewReport(summaries[0].ID)
	if err != nil || report.Output == nil || report.Output.Summary != "Native review completed" || report.Usage["total_tokens"] != float64(43) {
		t.Fatalf("native report or usage not saved: %+v %v", report, err)
	}
	if report.Output.Observations[0].Findings[0].Value != "120" {
		t.Fatal("native model overrode authoritative statistics")
	}
	rr = reviewRequest(mux, "POST", base+"suggest", map[string]any{"revision": experiment.Revision, "run_ids": []string{runs[0].ID}})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Observed verification") {
		t.Fatalf("native suggestions failed: %s", rr.Body)
	}
}
