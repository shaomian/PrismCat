package timeline

import (
	"fmt"
	"strings"
	"testing"
)

func TestFormatText(t *testing.T) {
	tl := Build("trace-1", openAIChain(), Options{})
	text := FormatText(tl, FormatOptions{})

	mustContain := []string{
		"共 2 个请求",
		"[请求 #1](日志ID=1) 上游=",
		"模型=gpt-4o",
		"[system] You are a coding agent.",
		"[user] fix the bug",
		"RESP [assistant 调用工具: read_file] {\"path\":\"main.go\"}",
		"（复用 3 条历史消息，内容同上，不再重复展示）",
		"[工具结果: read_file] package main", // tool_result 被富化出工具名
		"RESP [assistant] Bug fixed.",
	}
	for _, s := range mustContain {
		if !strings.Contains(text, s) {
			t.Errorf("format text missing %q:\n%s", s, text)
		}
	}
}

func TestFormatTextToolResultEnrichment(t *testing.T) {
	// tool_result 在输出事件中应被富化出工具名
	tl := Build("t", openAIChain(), Options{})
	r2 := tl.Requests[1]
	found := false
	for _, ev := range r2.NewEvents {
		if ev.Kind == "tool_result" && ev.ToolName == "read_file" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tool_result should be enriched with tool name read_file: %+v", r2.NewEvents)
	}

	text := FormatText(tl, FormatOptions{})
	if !strings.Contains(text, "[工具结果: read_file]") {
		t.Errorf("expected enriched tool name in text:\n%s", text)
	}
}

func TestFormatTextGlobalTruncation(t *testing.T) {
	// 构造多个超长请求，验证"保留首尾、省略中段"
	// 每个请求引入不同前缀的新内容，确保不被前缀去重折叠
	var items []Input
	for i := 1; i <= 10; i++ {
		items = append(items, Input{
			LogID: fmt.Sprint(i), Seq: i, Path: "/v1/chat/completions",
			RequestBody:  fmt.Sprintf(`{"messages":[{"role":"user","content":"msg%d %s"}]}`, i, strings.Repeat("x", 3000)),
			ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`,
		})
	}
	tl := Build("t", items, Options{})
	text := FormatText(tl, FormatOptions{MaxTotalChars: 4000})

	if len(text) > 4500 {
		t.Errorf("text should be under budget, got %d chars", len(text))
	}
	if !strings.Contains(text, "已省略") {
		t.Errorf("expected omission marker:\n%s", text)
	}
	if !strings.Contains(text, "[请求 #1]") || !strings.Contains(text, "[请求 #10]") {
		t.Errorf("should keep head and tail requests:\n%s", text)
	}
	// 中段请求应有单行摘要（含日志 ID，可 drill-down）
	if !strings.Contains(text, "[请求 #5](日志ID=5)") {
		t.Errorf("omitted middle requests should have digest lines with log IDs:\n%s", text)
	}
	if !strings.Contains(text, "get_request_detail") {
		t.Errorf("omission should hint the drill-down tool:\n%s", text)
	}
}
