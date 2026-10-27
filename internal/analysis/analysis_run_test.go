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

// 模拟两轮流式响应：第 1 轮发起 tool_call（参数分片），第 2 轮给出最终回答
func newToolLoopServer(t *testing.T) (*httptest.Server, *[]int) {
	var callCounts []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("bad payload: %v", err)
			w.WriteHeader(400)
			return
		}
		callCounts = append(callCounts, 1)

		w.Header().Set("Content-Type", "text/event-stream")
		rawMsgs, _ := json.Marshal(payload["messages"])
		var msgs []any
		_ = json.Unmarshal(rawMsgs, &msgs)

		_, hasTools := payload["tools"]
		if !strings.Contains(string(rawMsgs), `"role":"tool"`) && hasTools {
			// 第 1 轮：流式发起工具调用（参数分片发送，模拟真实行为）
			sse(w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_request_detail","arguments":"{\"log"}}]}}]}`)
			sse(w, `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"_id\":\"abc\"}"}}]}}]}`)
			sse(w, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"total_tokens":100}}`)
			w.Write([]byte("data: [DONE]\n\n"))
			return
		}
		// 第 2 轮：最终回答（校验 tool 消息已正确回传）
		raw, _ := json.Marshal(msgs)
		if !strings.Contains(string(raw), `"role":"tool"`) || !strings.Contains(string(raw), "call_1") {
			t.Errorf("tool result message not sent back: %s", raw)
		}
		if !strings.Contains(string(raw), "FILE_CONTENT_FROM_TOOL") {
			t.Errorf("tool result content not in messages: %s", raw)
		}
		sse(w, `{"choices":[{"index":0,"delta":{"content":"根据原文，"}}]}`)
		sse(w, `{"choices":[{"index":0,"delta":{"content":"修复内容是 println。"}}]}`)
		sse(w, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":300,"completion_tokens":50,"total_tokens":350}}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	return server, &callCounts
}

func sse(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func TestRunChatToolLoop(t *testing.T) {
	server, calls := newToolLoopServer(t)
	defer server.Close()

	var deltas []string
	var toolCalls []string
	var resets int
	exec := funcExecutor{result: "FILE_CONTENT_FROM_TOOL"}

	result, err := RunChat(context.Background(),
		config.AnalysisConfig{BaseURL: server.URL, Model: "test", APIKey: "k"},
		[]Message{{Role: "user", Content: "分析一下"}},
		StreamOptions{},
		exec,
		Callbacks{
			OnDelta:    func(d string) error { deltas = append(deltas, d); return nil },
			OnToolCall: func(name, args string) error { toolCalls = append(toolCalls, name+":"+args); return nil },
			OnReset:    func() error { resets++; return nil },
		},
	)
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}

	if result.Rounds != 2 {
		t.Errorf("expected 2 rounds, got %d", result.Rounds)
	}
	if result.ToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", result.ToolCalls)
	}
	if len(*calls) != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", len(*calls))
	}
	if content := strings.Join(deltas, ""); content != "根据原文，修复内容是 println。" {
		t.Errorf("final content mismatch: %q", content)
	}
	if result.Content != content(t, deltas) {
		t.Errorf("result.Content mismatch: %q", result.Content)
	}
	if len(toolCalls) != 1 || toolCalls[0] != `get_request_detail:{"log_id":"abc"}` {
		t.Errorf("tool call callback mismatch: %v", toolCalls)
	}
	// 多轮用量按轮累加：100+350=450 / 100+300=400 / 50
	if usage, _ := result.Usage["total_tokens"].(float64); usage != 450 {
		t.Errorf("total_tokens mismatch: %v", result.Usage)
	}
	if usage, _ := result.Usage["prompt_tokens"].(float64); usage != 400 {
		t.Errorf("prompt_tokens mismatch: %v", result.Usage)
	}
	if usage, _ := result.Usage["completion_tokens"].(float64); usage != 50 {
		t.Errorf("completion_tokens mismatch: %v", result.Usage)
	}
}

func content(t *testing.T, deltas []string) string {
	t.Helper()
	return strings.Join(deltas, "")
}

// 无工具回退：上游对 tools 报 400 时，自动降级重试
func TestRunChatToolFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if _, hasTools := payload["tools"]; hasTools {
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"tools not supported"}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		sse(w, `{"choices":[{"index":0,"delta":{"content":"no tools needed"}}]}`)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	result, err := RunChat(context.Background(),
		config.AnalysisConfig{BaseURL: server.URL, Model: "test"},
		[]Message{{Role: "user", Content: "hi"}},
		StreamOptions{}, funcExecutor{},
		Callbacks{},
	)
	if err != nil {
		t.Fatalf("RunChat failed: %v", err)
	}
	if result.Content != "no tools needed" {
		t.Errorf("content mismatch: %q", result.Content)
	}
	if result.Rounds != 1 || result.ToolCalls != 0 {
		t.Errorf("expected 1 round 0 tools after fallback, got rounds=%d tools=%d", result.Rounds, result.ToolCalls)
	}
}

// 非工具不支持的错误（5xx）不应触发降级重试：盲重试会翻倍失败延迟
// 并掩盖真实错误。
func TestRunChatNoFallbackOnServerError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"upstream exploded"}`))
	}))
	defer server.Close()

	_, err := RunChat(context.Background(),
		config.AnalysisConfig{BaseURL: server.URL, Model: "test"},
		[]Message{{Role: "user", Content: "hi"}},
		StreamOptions{}, funcExecutor{},
		Callbacks{},
	)
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != 500 {
		t.Fatalf("expected HTTPError 500, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 HTTP call (no blind retry), got %d", calls)
	}
}

// 复现 DeepSeek 在第七轮工具被撤掉后仍输出 DSML 的情况。
// 收尾必须保留查证结果、清掉临时文本，并且最多额外整理一次。
func TestRunChatFinalizesToolEvidenceAndRecoversDSML(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload struct {
			Tools    json.RawMessage  `json:"tools"`
			Messages []map[string]any `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if calls <= maxToolRounds {
			if len(payload.Tools) == 0 {
				t.Error("tools withdrawn before the collection limit")
			}
			raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
				"content":    "Reading evidence…",
				"tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", calls), "function": map[string]any{"name": ToolGetRequestDetail, "arguments": fmt.Sprintf(`{"log_id":"log-%d"}`, calls)}}},
			}}}})
			sse(w, string(raw))
		} else {
			if len(payload.Tools) != 0 {
				t.Error("finalization can still request tools")
			}
			for _, m := range payload.Messages {
				if m["role"] == "tool" || m["tool_calls"] != nil {
					t.Error("finalization still uses the tool conversation format")
				}
			}
			raw, _ := json.Marshal(payload.Messages)
			for _, want := range []string{"Original question", "Return JSON", "READ_EVIDENCE", "log-1", "log-6", "untrusted data"} {
				if !strings.Contains(string(raw), want) {
					t.Errorf("finalization lost %q", want)
				}
			}
			last := payload.Messages[len(payload.Messages)-1]
			if last["role"] != "user" || last["content"] != finalAnswerInstruction {
				t.Error("finalization missing explicit final-answer instruction")
			}
			text := `<｜｜DSML｜｜ calls><｜｜DSML｜｜ invoke name="get_request_detail"></｜｜DSML｜｜ invoke></｜｜DSML｜｜ calls>`
			if calls > maxToolRounds+1 {
				text = `{"summary":"Based on the evidence already read"}`
			}
			raw, _ = json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}, "finish_reason": "stop"}}})
			sse(w, string(raw))
		}
		sse(w, `{"choices":[],"usage":{"total_tokens":10}}`)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	var body strings.Builder
	result, err := RunChat(context.Background(), config.AnalysisConfig{BaseURL: server.URL, Model: "mock"},
		[]Message{{Role: "system", Content: "Return JSON"}, {Role: "user", Content: "Original question"}}, StreamOptions{}, funcExecutor{result: "READ_EVIDENCE"},
		Callbacks{OnDelta: func(s string) error { body.WriteString(s); return nil }, OnReset: func() error { body.Reset(); return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if result.Rounds != maxToolRounds+2 || result.ToolCalls != maxToolRounds || calls != maxToolRounds+2 {
		t.Fatalf("unexpected collection or retry count: %+v, requests=%d", result, calls)
	}
	if result.Content != body.String() || !json.Valid([]byte(result.Content)) {
		t.Fatalf("tool markup or provisional text leaked into final output: %q / %q", result.Content, body.String())
	}
	if result.Usage["total_tokens"] != float64(calls*10) {
		t.Fatalf("finalization usage not counted: %v", result.Usage)
	}
}

func TestRunChatToolTextRetryIsBounded(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint("native=", native), func(t *testing.T) {
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/event-stream")
				if native {
					sse(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"unexpected","function":{"name":"get_request_detail","arguments":"{}"}}]}}]}`)
				} else {
					sse(w, `{"choices":[{"delta":{"content":"<｜｜DSML｜｜ calls>unexecuted</｜｜DSML｜｜ calls>"},"finish_reason":"stop"}]}`)
				}
				sse(w, `{"choices":[],"usage":{"total_tokens":10}}`)
				fmt.Fprint(w, "data: [DONE]\n\n")
			}))
			defer server.Close()
			result, err := RunChat(context.Background(), config.AnalysisConfig{BaseURL: server.URL, Model: "mock"},
				[]Message{{Role: "user", Content: "Answer now"}}, StreamOptions{}, nil, Callbacks{})
			if !errors.Is(err, ErrNoFinalAnswer) || calls != 2 || result.ToolCalls != 0 || result.Content != "" {
				t.Fatalf("unexpected tool text was accepted or retried indefinitely: result=%+v calls=%d err=%v", result, calls, err)
			}
			if result.Usage["total_tokens"] != float64(20) {
				t.Fatalf("failed finalization usage lost: %v", result.Usage)
			}
		})
	}
}

func TestRunChatNonStreamingFinalContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"complete answer"}}]}`)
	}))
	defer server.Close()
	result, err := RunChat(context.Background(), config.AnalysisConfig{BaseURL: server.URL, Model: "mock"},
		[]Message{{Role: "user", Content: "Question"}}, StreamOptions{}, nil, Callbacks{})
	if err != nil || result.Content != "complete answer" {
		t.Fatalf("non-streaming output lost: %+v %v", result, err)
	}
}

type funcExecutor struct {
	result string
}

func (f funcExecutor) ExecuteToolCall(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if name != ToolGetRequestDetail {
		return "", fmt.Errorf("unknown tool %s", name)
	}
	return f.result, nil
}
