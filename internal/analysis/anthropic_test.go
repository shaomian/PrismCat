package analysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shaomian/PrismCat/internal/config"
)

func nativeEvent(w http.ResponseWriter, event any) {
	raw, _ := json.Marshal(event)
	sse(w, string(raw))
}

func TestAnthropicToolLoopPreservesSignedBlocksAndUsage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/gateway/v1/messages" || r.Header.Get("x-api-key") != "native-key" || r.Header.Get("anthropic-version") != anthropicVersion {
			t.Error("incorrect native endpoint or authentication")
		}
		var payload struct {
			System    string           `json:"system"`
			MaxTokens int              `json:"max_tokens"`
			Tools     []map[string]any `json:"tools"`
			Messages  []struct {
				Role    string           `json:"role"`
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		if payload.System != "native-system" || payload.MaxTokens != 8192 || payload.Tools[0]["input_schema"] == nil || payload.Tools[0]["function"] != nil {
			t.Error("incorrect native payload schema")
		}
		if calls == 2 {
			last := payload.Messages[len(payload.Messages)-1]
			if last.Role != "user" || len(last.Content) != 2 || last.Content[0]["type"] != "tool_result" || last.Content[1]["tool_use_id"] != "call-b" {
				t.Error("parallel tool results were not grouped correctly")
			}
			assistant := payload.Messages[len(payload.Messages)-2]
			if assistant.Content[0]["thinking"] != "private reasoning" || assistant.Content[0]["signature"] != "signed-opaque" {
				t.Error("thinking/signature not replayed unchanged")
			}
			for _, m := range payload.Messages {
				if m.Role == "system" || m.Role == "tool" {
					t.Error("OpenAI roles leaked into native messages")
				}
			}
			// Gateways may return a complete JSON response even when stream=true.
			fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"final answer"}],"stop_reason":"end_turn","usage":{"input_tokens":20,"output_tokens":15}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"type":"message_start","message":{"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":100}}}`)
		sse(w, `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`)
		sse(w, `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"private reasoning"}}`)
		sse(w, `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"signed-opaque"}}`)
		sse(w, `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"Looking at logs"}}`)
		for i, id := range []string{"call-a", "call-b"} {
			nativeEvent(w, map[string]any{"type": "content_block_start", "index": i + 2, "content_block": map[string]any{"type": "tool_use", "id": id, "name": ToolGetRequestDetail, "input": map[string]any{}}})
			for _, fragment := range []string{`{"log_id":`, `"abc"}`} {
				nativeEvent(w, map[string]any{"type": "content_block_delta", "index": i + 2, "delta": map[string]any{"type": "input_json_delta", "partial_json": fragment}})
			}
		}
		sse(w, `{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":8}}`)
		sse(w, `{"type":"message_stop"}`)
	}))
	defer server.Close()
	var output strings.Builder
	resets := 0
	result, err := RunChat(context.Background(), config.AnalysisConfig{Protocol: "anthropic", BaseURL: server.URL + "/gateway", APIKey: "native-key", Model: "native-model"},
		[]Message{{Role: "system", Content: "native-system"}, {Role: "user", Content: "Question"}}, StreamOptions{}, funcExecutor{result: "allowed evidence"},
		Callbacks{OnDelta: func(s string) error { output.WriteString(s); return nil }, OnReset: func() error { resets++; output.Reset(); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || result.ToolCalls != 2 || resets != 1 || result.Content != "final answer" || output.String() != result.Content {
		t.Fatalf("native loop failed: %+v output=%q resets=%d", result, output.String(), resets)
	}
	if result.Usage["prompt_tokens"] != float64(130) || result.Usage["completion_tokens"] != float64(23) || result.Usage["total_tokens"] != float64(153) || result.Usage["cache_read_input_tokens"] != float64(100) {
		t.Fatalf("usage was lost or double counted: %v", result.Usage)
	}
}

func TestAnthropicStreamingErrorsAndTruncation(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		truncated  bool
	}{
		{"premature", "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n", false},
		{"error", "data: {\"type\":\"error\",\"error\":{\"message\":\"overloaded\"}}\n\n", false},
		{"limit", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10}}}\n\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":20}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			result, err := RunChat(context.Background(), config.AnalysisConfig{Protocol: "anthropic", BaseURL: server.URL, Model: "test"}, []Message{{Role: "user", Content: "Question"}}, StreamOptions{}, nil, Callbacks{})
			if err == nil || errors.Is(err, ErrOutputTruncated) != tc.truncated {
				t.Fatalf("invalid output accepted: %v", err)
			}
			if tc.truncated && result.Usage["total_tokens"] != float64(30) {
				t.Fatal("truncated response usage lost")
			}
		})
	}
	want := errors.New("client closed")
	_, err := consumeAnthropicStream(strings.NewReader("data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"text\"}}\n\n"), Callbacks{OnDelta: func(string) error { return want }})
	if !errors.Is(err, want) {
		t.Fatal("callback cancellation ignored")
	}
}
