package timeline

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

// 构造一个 OpenAI 格式的 agent 循环调用链：
// 请求1: system + user → assistant(tool_call: read_file)
// 请求2: system + user + assistant(tool_call) + tool_result → assistant("done")
func openAIChain() []Input {
	sys := `{"role":"system","content":"You are a coding agent."}`
	user := `{"role":"user","content":"fix the bug"}`
	assistantCall := `{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]}`
	toolResult := `{"role":"tool","tool_call_id":"call_1","content":"package main..."}`

	req1 := `{"model":"gpt-4o","messages":[` + sys + `,` + user + `]}`
	resp1 := `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]}}]}`

	req2 := `{"model":"gpt-4o","messages":[` + sys + `,` + user + `,` + assistantCall + `,` + toolResult + `]}`
	resp2 := `{"choices":[{"message":{"role":"assistant","content":"Bug fixed."}}]}`

	return []Input{
		{LogID: "1", Seq: 1, Path: "/v1/chat/completions", RequestBody: req1, ResponseBody: resp1},
		{LogID: "2", Seq: 2, Path: "/v1/chat/completions", RequestBody: req2, ResponseBody: resp2},
	}
}

func TestBuildOpenAIChain(t *testing.T) {
	tl := Build("trace-1", openAIChain(), Options{})
	if tl.Summary.RequestCount != 2 {
		t.Fatalf("expected 2 requests, got %d", tl.Summary.RequestCount)
	}
	if tl.Summary.ParsedCount != 2 {
		t.Fatalf("expected 2 parsed, got %d", tl.Summary.ParsedCount)
	}

	r1 := tl.Requests[0]
	if r1.Format != formatOpenAIChat {
		t.Fatalf("expected openai-chat, got %s", r1.Format)
	}
	if r1.Model != "gpt-4o" {
		t.Fatalf("expected model gpt-4o, got %s", r1.Model)
	}
	if r1.Carried != 0 {
		t.Fatalf("request1 carried should be 0, got %d", r1.Carried)
	}
	// 新事件: system + user = 2; 响应: tool_call = 1
	if len(r1.NewEvents) != 2 {
		t.Fatalf("request1 new events should be 2, got %d", len(r1.NewEvents))
	}
	if len(r1.ResponseEvents) != 1 || r1.ResponseEvents[0].Kind != "tool_call" {
		t.Fatalf("request1 response should be 1 tool_call, got %+v", r1.ResponseEvents)
	}
	if r1.ResponseEvents[0].ToolName != "read_file" {
		t.Fatalf("tool name should be read_file, got %s", r1.ResponseEvents[0].ToolName)
	}

	r2 := tl.Requests[1]
	// 请求2 的前 3 条消息(system, user, assistant_call)与上一链一致 → carried=3
	if r2.Carried != 3 {
		t.Fatalf("request2 carried should be 3, got %d", r2.Carried)
	}
	// 新增: tool_result = 1
	if len(r2.NewEvents) != 1 || r2.NewEvents[0].Kind != "tool_result" {
		t.Fatalf("request2 new events should be 1 tool_result, got %+v", r2.NewEvents)
	}
	// 响应: assistant message
	if len(r2.ResponseEvents) != 1 || r2.ResponseEvents[0].Content != "Bug fixed." {
		t.Fatalf("request2 response mismatch: %+v", r2.ResponseEvents)
	}
	if tl.Summary.TotalCarried != 3 {
		t.Fatalf("total carried should be 3, got %d", tl.Summary.TotalCarried)
	}
}

func TestBuildOpenAISSE(t *testing.T) {
	req := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	var sb strings.Builder
	chunks := []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"run","arguments":"{\"a\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"1}"}}]}}]}`,
	}
	for _, c := range chunks {
		sb.WriteString("data: " + c + "\n\n")
	}
	sb.WriteString("data: [DONE]\n\n")

	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1/chat/completions", RequestBody: req, ResponseBody: sb.String(), Streaming: true},
	}
	tl := Build("t", items, Options{})
	r := tl.Requests[0]
	if r.ParseError != "" {
		t.Fatalf("unexpected parse error: %s", r.ParseError)
	}
	// 响应: message("Hello") + tool_call(run {"a":1})
	if len(r.ResponseEvents) != 2 {
		t.Fatalf("expected 2 response events, got %+v", r.ResponseEvents)
	}
	if r.ResponseEvents[0].Content != "Hello" {
		t.Fatalf("merged content should be Hello, got %q", r.ResponseEvents[0].Content)
	}
	tc := r.ResponseEvents[1]
	if tc.Kind != "tool_call" || tc.ToolName != "run" || tc.ToolArgs != `{"a":1}` {
		t.Fatalf("tool call mismatch: %+v", tc)
	}
}

func TestBuildAnthropicChain(t *testing.T) {
	req := `{"model":"claude-sonnet-4-5","max_tokens":1024,"system":"You are helpful.","messages":[{"role":"user","content":[{"type":"text","text":"read a file"}]}]}`
	resp := `{"role":"assistant","content":[{"type":"text","text":"Let me read it."},{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/tmp/a.go"}}]}`

	req2 := `{"model":"claude-sonnet-4-5","max_tokens":1024,"system":"You are helpful.","messages":[{"role":"user","content":[{"type":"text","text":"read a file"}]},{"role":"assistant","content":[{"type":"text","text":"Let me read it."},{"type":"tool_use","id":"tu_1","name":"Read","input":{"path":"/tmp/a.go"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"file content here"}]}]}`
	resp2 := `{"role":"assistant","content":[{"type":"text","text":"Done."}]}`

	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1/messages", RequestBody: req, ResponseBody: resp},
		{LogID: "2", Seq: 2, Path: "/v1/messages", RequestBody: req2, ResponseBody: resp2},
	}
	tl := Build("t", items, Options{})

	r1 := tl.Requests[0]
	if r1.Format != formatAnthropic {
		t.Fatalf("expected anthropic, got %s", r1.Format)
	}
	// 新事件: system + user = 2; 响应: text + tool_use = 2
	if len(r1.NewEvents) != 2 {
		t.Fatalf("r1 new events: %+v", r1.NewEvents)
	}
	if len(r1.ResponseEvents) != 2 || r1.ResponseEvents[1].Kind != "tool_call" || r1.ResponseEvents[1].ToolName != "Read" {
		t.Fatalf("r1 response: %+v", r1.ResponseEvents)
	}
	// tool_use 参数应为 JSON
	var args map[string]any
	if err := json.Unmarshal([]byte(r1.ResponseEvents[1].ToolArgs), &args); err != nil || args["path"] != "/tmp/a.go" {
		t.Fatalf("tool args should be JSON with path, got %q", r1.ResponseEvents[1].ToolArgs)
	}

	r2 := tl.Requests[1]
	// system + user + assistant_text + assistant_tool_use = 4 条前缀一致
	if r2.Carried != 4 {
		t.Fatalf("r2 carried should be 4, got %d", r2.Carried)
	}
	if len(r2.NewEvents) != 1 || r2.NewEvents[0].Kind != "tool_result" {
		t.Fatalf("r2 new events: %+v", r2.NewEvents)
	}
}

func TestBuildGemini(t *testing.T) {
	req := `{"contents":[{"role":"user","parts":[{"text":"list files"}]}],"systemInstruction":{"parts":[{"text":"Be brief."}]}}`
	resp := `{"candidates":[{"content":{"role":"model","parts":[{"text":"Listing..."},{"functionCall":{"name":"ls","args":{"dir":"/tmp"}}}]}}]}`
	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1beta/models/gemini-2.5-pro:generateContent", RequestBody: req, ResponseBody: resp},
	}
	tl := Build("t", items, Options{})
	r := tl.Requests[0]
	if r.Format != formatGemini {
		t.Fatalf("expected gemini, got %s", r.Format)
	}
	if len(r.NewEvents) != 2 || r.NewEvents[0].Role != "system" {
		t.Fatalf("new events: %+v", r.NewEvents)
	}
	if len(r.ResponseEvents) != 2 || r.ResponseEvents[1].Kind != "tool_call" || r.ResponseEvents[1].ToolName != "ls" {
		t.Fatalf("response: %+v", r.ResponseEvents)
	}
}

func TestTruncation(t *testing.T) {
	long := strings.Repeat("好", 5000) // 中文字符验证 rune 截断
	req := `{"messages":[{"role":"user","content":"` + long + `"}]}`
	items := []Input{{LogID: "1", Path: "/v1/chat/completions", RequestBody: req}}
	tl := Build("t", items, Options{MaxEventChars: 100})
	ev := tl.Requests[0].NewEvents[0]
	if !ev.Truncated {
		t.Fatal("expected truncated event")
	}
	if n := len([]rune(ev.Content)); n > 101 { // 100 + "…"
		t.Fatalf("content should be ~100 runes, got %d", n)
	}
}

// full_log 按需调取用的构建方式：与默认构建共享逻辑，
// 事件数量、顺序、carried 完全一致，仅内容不截断。
func TestBuildUnlimitedAlignsWithDefault(t *testing.T) {
	long := strings.Repeat("好", 5000)
	req1 := `{"model":"m","messages":[{"role":"user","content":"` + long + `"}]}`
	resp1 := `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`
	req2 := `{"model":"m","messages":[{"role":"user","content":"` + long + `"},{"role":"assistant","content":"ok"}]}`
	resp2 := `{"choices":[{"message":{"role":"assistant","content":"` + long + `"}}]}`

	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1/chat/completions", RequestBody: req1, ResponseBody: resp1},
		{LogID: "2", Seq: 2, Path: "/v1/chat/completions", RequestBody: req2, ResponseBody: resp2},
	}
	def := Build("t", items, Options{})
	full := Build("t", items, Options{MaxEventChars: math.MaxInt})

	if len(def.Requests) != len(full.Requests) {
		t.Fatalf("request count mismatch: %d vs %d", len(def.Requests), len(full.Requests))
	}
	for i := range full.Requests {
		d, f := def.Requests[i], full.Requests[i]
		if d.Carried != f.Carried ||
			len(d.NewEvents) != len(f.NewEvents) ||
			len(d.ResponseEvents) != len(f.ResponseEvents) {
			t.Fatalf("entry %d structure mismatch: %+v vs %+v", i, d, f)
		}
	}

	dEv, fEv := def.Requests[0].NewEvents[0], full.Requests[0].NewEvents[0]
	if !dEv.Truncated {
		t.Fatal("default build should truncate long content")
	}
	if len([]rune(dEv.Content)) != 2001 { // 2000 + "…"
		t.Fatalf("default build should truncate at 2000 runes, got %d", len([]rune(dEv.Content)))
	}
	if fEv.Truncated || fEv.Content != long {
		t.Fatalf("full build should carry original content: %+v", fEv)
	}

	// 第二个请求：长消息被前缀去重折叠，仅响应事件为长文本
	if full.Requests[1].Carried != 2 || len(full.Requests[1].NewEvents) != 0 {
		t.Fatalf("full build carried mismatch: %+v", full.Requests[1])
	}
	fResp := full.Requests[1].ResponseEvents[0]
	if fResp.Truncated || fResp.Content != long {
		t.Fatalf("full build should carry full response: %+v", fResp)
	}
	if !def.Requests[1].ResponseEvents[0].Truncated {
		t.Fatal("default build should truncate long response")
	}
}

// TruncateEvents(完整构建, N) 必须与 Build(Options{MaxEventChars: N}) 完全一致，
// 缓存派生路径才能安全替换直接构建。
func TestTruncateEventsParity(t *testing.T) {
	long := strings.Repeat("好", 5000)
	req1 := `{"model":"m","messages":[{"role":"user","content":"` + long + `"},{"role":"system","content":"sys"}]}`
	resp1 := `{"choices":[{"message":{"role":"assistant","content":"ok","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{\"cmd\":\"` + long + `\"}"}}]}}]}`
	req2 := `{"model":"m","messages":[{"role":"system","content":"sys"},{"role":"user","content":"` + long + `"}]}`
	resp2 := `{"choices":[{"message":{"role":"assistant","content":"` + long + `"}}]}`

	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1/chat/completions", RequestBody: req1, ResponseBody: resp1},
		{LogID: "2", Seq: 2, Path: "/v1/chat/completions", RequestBody: req2, ResponseBody: resp2},
	}
	full := Build("t", items, Options{MaxEventChars: math.MaxInt})

	for _, maxChars := range []int{40, 200, 2000} {
		direct := Build("t", items, Options{MaxEventChars: maxChars})
		derived := TruncateEvents(full, maxChars)
		if !reflect.DeepEqual(direct, derived) {
			t.Fatalf("maxChars=%d: derived timeline differs from direct build\n--direct--\n%+v\n--derived--\n%+v", maxChars, direct, derived)
		}
	}

	// 无需截断的级别：完整内容原样保留
	same := TruncateEvents(full, math.MaxInt)
	if !reflect.DeepEqual(full, same) {
		t.Fatal("TruncateEvents with unlimited maxChars should be an equal copy")
	}
}

func TestGracefulDegradation(t *testing.T) {
	// 非 JSON 请求体不应阻断整条链
	items := []Input{
		{LogID: "1", Path: "/v1/chat/completions", RequestBody: `{invalid`, ResponseBody: ""},
		{LogID: "2", Path: "/v1/chat/completions", RequestBody: `{"messages":[{"role":"user","content":"hi"}]}`, ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"yo"}}]}`},
	}
	tl := Build("t", items, Options{})
	if tl.Requests[0].ParseError == "" {
		t.Fatal("expected parse error on first request")
	}
	if tl.Requests[1].ParseError != "" || len(tl.Requests[1].NewEvents) != 1 {
		t.Fatalf("second request should parse fine: %+v", tl.Requests[1])
	}
}

func TestContextRewrite(t *testing.T) {
	// 客户端压缩/改写历史后：LCP 在分叉点停止，之后全部算新增
	req1 := `{"messages":[{"role":"system","content":"s"},{"role":"user","content":"u1"}]}`
	resp1 := `{"choices":[{"message":{"role":"assistant","content":"a1"}}]}`
	// 历史被压缩：system 保留，user 被替换为摘要
	req2 := `{"messages":[{"role":"system","content":"s"},{"role":"user","content":"[summary of u1+a1]"},{"role":"user","content":"u2"}]}`
	resp2 := `{"choices":[{"message":{"role":"assistant","content":"a2"}}]}`
	items := []Input{
		{LogID: "1", Path: "/v1/chat/completions", RequestBody: req1, ResponseBody: resp1},
		{LogID: "2", Path: "/v1/chat/completions", RequestBody: req2, ResponseBody: resp2},
	}
	tl := Build("t", items, Options{})
	r2 := tl.Requests[1]
	if r2.Carried != 1 {
		t.Fatalf("carried should be 1 (only system), got %d", r2.Carried)
	}
	if len(r2.NewEvents) != 2 {
		t.Fatalf("new events should be 2, got %+v", r2.NewEvents)
	}
}
