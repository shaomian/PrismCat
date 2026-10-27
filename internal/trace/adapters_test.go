package trace

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestGeminiToolRoundTripAndTaskBoundary(t *testing.T) {
	for _, id := range []string{"", "g-call-1"} {
		t.Run(id, func(t *testing.T) {
			call := map[string]any{"functionCall": map[string]any{"id": id, "name": "read", "args": map[string]any{"n": json.Number("9007199254740993")}}, "thoughtSignature": "signature"}
			user := map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Fix login"}}}
			model := map[string]any{"role": "model", "parts": []any{call}}
			result := map[string]any{"role": "user", "parts": []any{map[string]any{"functionResponse": map[string]any{"id": id, "name": "read", "response": map[string]any{"result": "file"}}}}}
			encode := func(v any) []byte { b, _ := json.Marshal(v); return b }
			c, now := NewCorrelator(), time.Now()
			first := c.Observe(Observation{LogID: "one", Scope: "gemini", CreatedAt: now, Request: ParseRequest("/v1beta/models/gemini:generateContent", encode(map[string]any{"contents": []any{user}})), Response: ParseResponse("gemini", encode(map[string]any{"responseId": "resp-g", "candidates": []any{map[string]any{"content": model, "finishReason": "STOP"}}}), false)})[0]
			secondReq := ParseRequest("/v1beta/models/gemini:streamGenerateContent", encode(map[string]any{"contents": []any{user, model, result}}))
			second := c.Observe(Observation{LogID: "two", Scope: "gemini", CreatedAt: now.Add(time.Second), Request: secondReq})[0]
			if second.Assignment.TraceID != first.Assignment.TraceID || len(second.Assignment.Parents) != 1 {
				t.Fatalf("tool continuation lost: %+v", second.Assignment)
			}
			thirdReq := ParseRequest("/v1beta/models/gemini:generateContent", encode(map[string]any{"contents": []any{user, model, result, map[string]any{"role": "user", "parts": []any{map[string]any{"text": "Add export"}}}}}))
			third := c.Observe(Observation{LogID: "three", Scope: "gemini", CreatedAt: now.Add(2 * time.Second), Request: thirdReq})[0]
			if third.Assignment.TraceID == first.Assignment.TraceID || third.Assignment.SessionID != first.Assignment.SessionID {
				t.Fatalf("bad task boundary: %+v", third.Assignment)
			}
		})
	}
}

func TestGeminiSSEAndOpaqueParts(t *testing.T) {
	sse := "data: {\"responseId\":\"g1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"Hello \"}]}}]}\n\ndata: {\"responseId\":\"g1\",\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"world\"}]},\"finishReason\":\"STOP\"}]}\n\n"
	stream := ParseResponse("gemini", []byte(sse), true)
	plain := ParseResponse("gemini", []byte(`{"responseId":"g1","candidates":[{"content":{"parts":[{"text":"Hello world"}]},"finishReason":"STOP"}]}`), false)
	if !stream.Valid || !reflect.DeepEqual(stream, plain) {
		t.Fatalf("stream mismatch: %+v / %+v", stream, plain)
	}
	for _, raw := range []string{strings.Split(sse, "\n\n")[0], strings.ReplaceAll(sse, "STOP", "MAX_TOKENS"), sse + "data: invalid\n\n"} {
		if ParseResponse("gemini", []byte(raw), true).Valid {
			t.Fatal("incomplete or malformed stream became evidence")
		}
	}
	a := ParseRequest("/v1/models/g:generateContent", []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AAA"}}]}]}`))
	b := ParseRequest("/v1/models/g:generateContent", []byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"BBB"}}]}]}`))
	if a.Events[0].Digest == b.Events[0].Digest {
		t.Fatal("media identity discarded")
	}
}

func TestNativeTurnMetadataAndSessionIsolation(t *testing.T) {
	request := func(session, turn string) Features {
		metadata, _ := json.Marshal(map[string]string{"thread_id": session, "turn_id": turn})
		body, _ := json.Marshal(map[string]any{"input": "client injected instruction", "client_metadata": map[string]string{"x-codex-turn-metadata": string(metadata)}})
		return ParseClientRequest("/v1/responses", body, nil)
	}
	c, now := NewCorrelator(), time.Now()
	observe := func(id, session, turn string) Observation {
		return c.Observe(Observation{LogID: id, Scope: "a", CreatedAt: now, Request: request(session, turn)})[0]
	}
	a, b, next, other := observe("a", "thread-one", "turn-one"), observe("b", "thread-one", "turn-one"), observe("c", "thread-one", "turn-two"), observe("d", "thread-two", "turn-one")
	if a.Assignment.TraceID != b.Assignment.TraceID || next.Assignment.TraceID == a.Assignment.TraceID || other.Assignment.TraceID == a.Assignment.TraceID || a.Assignment.SessionID != next.Assignment.SessionID {
		t.Fatalf("invalid native task boundaries: %+v %+v %+v %+v", a.Assignment, b.Assignment, next.Assignment, other.Assignment)
	}
	if len(b.Assignment.Parents) != 0 {
		t.Fatal("native identity invented dependency")
	}
	encoded, _ := json.Marshal(a.Request)
	if strings.Contains(string(encoded), "thread-one") || strings.Contains(string(encoded), "turn-one") {
		t.Fatal("raw native identity persisted")
	}
	// Native task identity remains stable after restart without loading parents.
	restarted := NewCorrelator().Observe(Observation{LogID: "again", Scope: "a", CreatedAt: now, Request: request("thread-one", "turn-one")})[0]
	if restarted.Assignment.TraceID != a.Assignment.TraceID {
		t.Fatal("native task identity changed after restart")
	}
}

func TestNativeSessionNeverDirectlyJoinsTasksOrAccounts(t *testing.T) {
	flat := ParseClientRequest("/v1/responses", []byte(`{"input":"task","client_metadata":{"thread_id":"thread","turn_id":"turn"}}`), nil)
	if flat.NativeTurn == "" {
		t.Fatal("flat Codex metadata was ignored")
	}
	c, now := NewCorrelator(), time.Now()
	for _, header := range []string{"X-Opencode-Session", "X-Session-ID", "X-Session-Affinity", "Thread-Id"} {
		path, body := "/v1/chat/completions", rootRequest
		if header == "Thread-Id" {
			path, body = "/v1/responses", `{"input":"fix"}`
		}
		f := ParseClientRequest(path, []byte(body), map[string][]string{header: {"session-1"}})
		a := c.Observe(Observation{LogID: header + "a", Scope: "a", CreatedAt: now, Request: f})[0]
		b := c.Observe(Observation{LogID: header + "b", Scope: "a", CreatedAt: now, Request: f})[0]
		if a.Assignment.TraceID == b.Assignment.TraceID || a.Assignment.SessionID != b.Assignment.SessionID {
			t.Fatalf("session became task: %s", header)
		}
	}
	f := ParseClientRequest("/v1/messages", []byte(`{"metadata":{"user_id":"account-1"},"messages":[{"role":"user","content":"fix"}]}`), nil)
	if f.NativeSession != "" || f.NativeTurn != "" {
		t.Fatal("account ID became session/task")
	}
	first := observation("first", "a", rootRequest, toolResponse, now)
	first.Request = ParseClientRequest("/v1/chat/completions", []byte(rootRequest), map[string][]string{"x-session-id": {"one"}})
	a := c.Observe(first)[0]
	second := observation("second", "a", resultRequest, "", now.Add(time.Second))
	second.Request = ParseClientRequest("/v1/chat/completions", []byte(resultRequest), map[string][]string{"x-session-id": {"two"}})
	b := c.Observe(second)[0]
	if a.Assignment.TraceID == b.Assignment.TraceID {
		t.Fatal("crossed conflicting native sessions")
	}
}
