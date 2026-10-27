package trace

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func observation(id, scope, request, response string, at time.Time) Observation {
	q := ParseRequest("/v1/chat/completions", []byte(request))
	return Observation{LogID: id, Scope: scope, CreatedAt: at, Request: q, Response: ParseResponse(q.Format, []byte(response), false)}
}

const rootRequest = `{"messages":[{"role":"user","content":"Fix login"}]}`
const toolResponse = `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"login.go\"}"}}]}}]}`
const resultRequest = `{"messages":[{"role":"user","content":"Fix login"},{"role":"assistant","tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{\"path\":\"login.go\"}"}}]},{"role":"tool","tool_call_id":"c1","content":"file contents"}]}`

func TestToolContinuationAndIsolation(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, toolResponse, now))[0]
	second := c.Observe(observation("2", "a", resultRequest, "", now.Add(time.Second)))[0]
	if second.Assignment.TraceID != first.Assignment.TraceID || second.Assignment.Reason != "tool_result" || len(second.Assignment.Parents) != 1 {
		t.Fatalf("not continued: %+v", second.Assignment)
	}
	other := c.Observe(observation("3", "b", resultRequest, "", now.Add(time.Second)))[0]
	if other.Assignment.TraceID == first.Assignment.TraceID {
		t.Fatal("crossed upstream scope")
	}
	repeated := c.Observe(observation("4", "a", rootRequest, "", now.Add(time.Second)))[0]
	if repeated.Assignment.TraceID == first.Assignment.TraceID {
		t.Fatal("identical root prompt merged")
	}
}

func TestNewInstructionIgnoresHistoricalToolIDs(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, toolResponse, now))[0]
	var doc map[string]any
	_ = json.Unmarshal([]byte(resultRequest), &doc)
	doc["messages"] = append(doc["messages"].([]any), map[string]any{"role": "user", "content": "Now add export"})
	raw, _ := json.Marshal(doc)
	next := c.Observe(observation("2", "a", string(raw), "", now.Add(time.Second)))[0]
	if next.Assignment.TraceID == first.Assignment.TraceID {
		t.Fatal("historical tool IDs merged new instruction")
	}
}

func TestAmbiguousToolIDsDoNotChooseOne(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	c.Observe(observation("1", "a", rootRequest, toolResponse, now))
	c.Observe(observation("2", "a", rootRequest, toolResponse, now))
	next := c.Observe(observation("3", "a", resultRequest, "", now.Add(time.Second)))[0]
	if next.Assignment.State != "unresolved" || next.Assignment.Reason != "ambiguous_candidates" {
		t.Fatalf("arbitrarily matched: %+v", next.Assignment)
	}
}

func TestLateParentResponseResolvesWaitingToolResult(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, "", now))[0]
	second := c.Observe(observation("2", "a", resultRequest, "", now.Add(time.Second)))[0]
	if second.Assignment.TraceID == first.Assignment.TraceID {
		t.Fatal("matched without evidence")
	}
	updates := c.Observe(observation("1", "a", rootRequest, toolResponse, now))
	for _, o := range updates {
		if o.LogID == "2" && o.Assignment.TraceID == first.Assignment.TraceID {
			return
		}
	}
	t.Fatalf("waiting child not reconciled: %+v", updates)
}

func TestResponsesNewInstructionSameSessionDifferentTask(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := Observation{LogID: "1", Scope: "a", CreatedAt: now, Request: ParseRequest("/v1/responses", []byte(`{"input":"fix bug"}`)), Response: ParseResponse("openai-responses", []byte(`{"id":"resp_1","output":[{"type":"function_call","call_id":"c1","name":"read","arguments":"{}"}]}`), false)}
	first = c.Observe(first)[0]
	continuation := Observation{LogID: "2", Scope: "a", CreatedAt: now.Add(time.Second), Request: ParseRequest("/v1/responses", []byte(`{"previous_response_id":"resp_1","input":[{"type":"function_call_output","call_id":"c1","output":"ok"}]}`))}
	second := c.Observe(continuation)[0]
	if second.Assignment.TraceID != first.Assignment.TraceID {
		t.Fatal("lost response continuation")
	}
	continuation.LogID = "3"
	continuation.Request = ParseRequest("/v1/responses", []byte(`{"previous_response_id":"resp_1","input":"add export"}`))
	third := c.Observe(continuation)[0]
	if third.Assignment.TraceID == first.Assignment.TraceID || third.Assignment.SessionID != first.Assignment.SessionID {
		t.Fatalf("bad task boundary: %+v", third.Assignment)
	}
}

func TestAnthropicToolResultAndMediaIdentity(t *testing.T) {
	f := ParseRequest("/v1/messages", []byte(`{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}]}`))
	if !f.Valid || f.Events[0].Kind != "tool_result" || f.Events[0].Role != "tool" {
		t.Fatalf("misclassified user tool result: %+v", f)
	}
	a := ParseRequest("/v1/chat/completions", []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:one"}}]}]}`))
	b := ParseRequest("/v1/chat/completions", []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:two"}}]}]}`))
	if a.Events[0].Digest == b.Events[0].Digest {
		t.Fatal("different images collapsed")
	}
}

func TestSSEAssemblesArgumentsAndRequiresCompletion(t *testing.T) {
	raw := "data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"read\",\"arguments\":\"{\\\"path\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"login.go\\\"}\"}}]}}]}\n\n"
	if ParseResponse("openai-chat", []byte(raw), true).Valid {
		t.Fatal("partial response became complete evidence")
	}
	f := ParseResponse("openai-chat", []byte(raw+"data: [DONE]\n\n"), true)
	if !f.Valid || len(f.Events) != 1 || f.Events[0].CallID != "c1" {
		t.Fatalf("lost SSE tool call: %+v", f)
	}
	want := ParseResponse("openai-chat", []byte(toolResponse), false)
	if f.Events[0].Digest != want.Events[0].Digest {
		t.Fatal("stream and nonstream arguments differ")
	}
}

func TestFeatureLimitsAndNoPromptPersistence(t *testing.T) {
	if ParseRequest("/v1/chat/completions", []byte(strings.Repeat("x", MaxParseBytes+1))).Valid {
		t.Fatal("accepted oversize")
	}
	f := ParseRequest("/v1/chat/completions", []byte(rootRequest))
	raw, _ := json.Marshal(f)
	if strings.Contains(string(raw), "Fix login") {
		t.Fatal("features persist prompt text")
	}
}

func TestNumericIdentityIsNotRounded(t *testing.T) {
	a := ParseRequest("/v1/chat/completions", []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"lookup","arguments":"{\"id\":9007199254740992}"}}]}]}`))
	b := ParseRequest("/v1/chat/completions", []byte(`{"messages":[{"role":"assistant","tool_calls":[{"id":"c","function":{"name":"lookup","arguments":"{\"id\":9007199254740993}"}}]}]}`))
	if !a.Valid || !b.Valid || a.Events[0].Digest == b.Events[0].Digest {
		t.Fatal("large numeric arguments collapsed")
	}
}

func TestProtocolNamespacesDoNotCollide(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	chat := observation("chat", "a", rootRequest, toolResponse, now)
	chat.Response.ResponseID = "same-id"
	first := c.Observe(chat)[0]
	responses := Observation{LogID: "responses", Scope: "a", CreatedAt: now.Add(time.Second), Request: ParseRequest("/v1/responses", []byte(`{"previous_response_id":"same-id","input":[{"type":"function_call_output","call_id":"c1","output":"ok"}]}`))}
	second := c.Observe(responses)[0]
	if second.Assignment.TraceID == first.Assignment.TraceID || second.Assignment.SessionID == first.Assignment.SessionID {
		t.Fatal("IDs crossed protocol families")
	}
}

func TestNewInstructionBeforeTrailingOldToolResultDoesNotMerge(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, toolResponse, now))[0]
	raw := `{"messages":[{"role":"user","content":"A new task"},{"role":"tool","tool_call_id":"c1","content":"old result"}]}`
	second := c.Observe(observation("2", "a", raw, "", now.Add(time.Second)))[0]
	if second.Assignment.TraceID == first.Assignment.TraceID || second.Assignment.SessionID != first.Assignment.SessionID {
		t.Fatalf("new instruction merged into old task: %+v", second.Assignment)
	}
}

func TestLateCollisionRetractsEarlierInference(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := c.Observe(observation("1", "a", rootRequest, toolResponse, now))[0]
	second := c.Observe(observation("2", "a", resultRequest, "", now.Add(time.Second)))[0]
	if second.Assignment.TraceID != first.Assignment.TraceID {
		t.Fatal("initial inference failed")
	}
	updates := c.Observe(observation("other-root", "a", rootRequest, toolResponse, now))
	for _, update := range updates {
		if update.LogID == "2" {
			if update.Assignment.TraceID == first.Assignment.TraceID || update.Assignment.Reason != "ambiguous_candidates" {
				t.Fatalf("late collision retained wrong trace: %+v", update.Assignment)
			}
			return
		}
	}
	t.Fatal("dependent inference was not re-evaluated")
}

func TestConflictingResponseAndToolEvidenceIsNotIgnored(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	first := observation("1", "a", rootRequest, toolResponse, now)
	first.Response.ResponseID = "resp-one"
	c.Observe(first)
	other := observation("other", "a", rootRequest, strings.ReplaceAll(toolResponse, "c1", "c2"), now)
	c.Observe(other)
	child := observation("child", "a", strings.ReplaceAll(resultRequest, "c1", "c2"), "", now.Add(time.Second))
	child.Request.PreviousID = "resp-one"
	got := c.Observe(child)[0]
	if got.Assignment.Reason != "ambiguous_candidates" {
		t.Fatalf("strong conflicting evidence ignored: %+v", got.Assignment)
	}
}

func TestLateContentEvidenceAndNextTaskBoundary(t *testing.T) {
	c := NewCorrelator()
	now := time.Now()
	answer := strings.Repeat("specific answer ", 12)
	response := `{"choices":[{"message":{"role":"assistant","content":"` + answer + `"}}]}`
	request := `{"messages":[{"role":"user","content":"Fix login"},{"role":"assistant","content":"` + answer + `"},{"role":"user","content":"Now export"}]}`
	first := c.Observe(observation("1", "a", rootRequest, "", now))[0]
	c.Observe(observation("2", "a", request, "", now.Add(time.Second)))
	updates := c.Observe(observation("1", "a", rootRequest, response, now))
	for _, update := range updates {
		if update.LogID == "2" {
			if update.Assignment.TraceID == first.Assignment.TraceID || update.Assignment.SessionID != first.Assignment.SessionID || update.Assignment.Reason != "new_user_instruction" {
				t.Fatalf("content boundary failed: %+v", update.Assignment)
			}
			return
		}
	}
	t.Fatal("late content evidence did not trigger reconciliation")
}
