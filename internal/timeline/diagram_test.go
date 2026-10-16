package timeline

import (
	"strings"
	"testing"
)

func TestDiagram(t *testing.T) {
	tl := Build("trace-1", openAIChain(), Options{})
	src, labels := Diagram(tl, DiagramOptions{})

	mustContain := []string{
		"sequenceDiagram",
		"actor U as User",
		"participant L as LLM (gpt-4o)",
		"U->>A: fix the bug",
		"A->>L: ＃1 请求",
		"L-->>A: 调用工具 read_file",
		"A->>T: read_file(",
		"T-->>A: read_file → package main...",
		"A->>L: ＃2 请求",
		"L-->>A: Bug fixed.",
	}
	for _, s := range mustContain {
		if !strings.Contains(src, s) {
			t.Errorf("diagram missing %q:\n%s", s, src)
		}
	}

	// 每一行必须是合法的 Mermaid 单行（无裸换行残留）
	for _, line := range strings.Split(src, "\n") {
		if strings.ContainsRune(line, '\r') {
			t.Errorf("line contains CR: %q", line)
		}
		checkNoMermaidBreakers(t, line)
	}

	// labels 与源码中的消息顺序一致（Text 为 esc 前的原文）：
	// U->>A(user), A->>L(#1), L-->>A(tool), A->>T(args),
	// T-->>A(result), A->>L(#2), L-->>A(message)
	if len(labels) != 7 {
		t.Fatalf("expected 7 labels, got %d: %+v", len(labels), labels)
	}
	if labels[0].Text != "fix the bug" || labels[0].LogID != "1" || labels[0].Truncated {
		t.Errorf("label[0] mismatch: %+v", labels[0])
	}
	if labels[1].Text != "#1 请求" {
		t.Errorf("label[1] mismatch: %+v", labels[1])
	}
	if labels[6].Text != "Bug fixed." || labels[6].LogID != "2" {
		t.Errorf("label[6] mismatch: %+v", labels[6])
	}
}

func TestDiagramLabelTruncation(t *testing.T) {
	long := strings.Repeat("x", 500)
	items := []Input{{
		LogID: "1", Path: "/v1/chat/completions",
		RequestBody:  `{"messages":[{"role":"user","content":"` + long + `"}]}`,
		ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`,
	}}
	tl := Build("t", items, Options{})
	src, labels := Diagram(tl, DiagramOptions{LabelChars: 40})
	for _, line := range strings.Split(src, "\n") {
		if len([]rune(line)) > 150 { // 前缀 + 40 + 省略号
			t.Errorf("line too long (%d runes): %q", len([]rune(line)), line)
		}
	}

	// 被截断的标签必须携带完整原文，否则前端无从展开
	if len(labels) != 3 {
		t.Fatalf("expected 3 labels, got %d: %+v", len(labels), labels)
	}
	truncatedLabel := labels[0]
	if !truncatedLabel.Truncated {
		t.Fatalf("user message label should be truncated: %+v", truncatedLabel)
	}
	if truncatedLabel.Text != long || truncatedLabel.LogID != "1" {
		t.Errorf("truncated label should carry full text: %+v", truncatedLabel)
	}
	respLabel := labels[2]
	if respLabel.Truncated || respLabel.Text != "ok" {
		t.Errorf("short label should not be truncated: %+v", respLabel)
	}
}

// 工具参数/输出中包含会破坏 Mermaid sequenceDiagram 语法的字符：
// ";" 是语句结束符、"#" 起注释作用、换行会截断语句，此外还有引号、箭头、括号等。
func TestDiagramEscapesMermaidSyntax(t *testing.T) {
	sys := `{"role":"system","content":"s"}`
	user := `{"role":"user","content":"grep -rn '#define' main.c; echo done"}`
	toolResult := `{"role":"tool","tool_call_id":"c1","content":"file.go:3: #define FOO\n-->> next line"}`
	assistantCall := `{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo '=== v ==='; grep #tag\"}"}}]}`
	resp1 := `{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo '=== v ==='; grep #tag\"}"}}]}}]}`
	resp2 := `{"choices":[{"message":{"role":"assistant","content":"done"}}]}`

	items := []Input{
		{LogID: "1", Seq: 1, Path: "/v1/chat/completions",
			RequestBody: `{"model":"m","messages":[` + sys + `,` + user + `]}`, ResponseBody: resp1},
		{LogID: "2", Seq: 2, Path: "/v1/chat/completions",
			RequestBody: `{"model":"m","messages":[` + sys + `,` + user + `,` + assistantCall + `,` + toolResult + `]}`, ResponseBody: resp2},
	}

	src, _ := Diagram(Build("t", items, Options{}), DiagramOptions{})

	for _, want := range []string{
		"＃define",         // 用户消息中的 # 被转义
		"；",               // 分号被转义
		"echo '=== v ===", // 工具参数中的引号、=、> 均安全，保留原样
	} {
		if !strings.Contains(src, want) {
			t.Errorf("diagram missing %q:\n%s", want, src)
		}
	}
	if strings.Contains(src, "-->> next line") {
		t.Errorf("tool result 换行未压平:\n%s", src)
	}

	for _, line := range strings.Split(src, "\n") {
		checkNoMermaidBreakers(t, line)
	}
}

// checkNoMermaidBreakers 校验一行中消息文本区（首个 ":" 之后）不含裸 ";"、"#"。
func checkNoMermaidBreakers(t *testing.T, line string) {
	t.Helper()
	idx := strings.Index(line, ":")
	if idx < 0 {
		return
	}
	text := line[idx:]
	if strings.ContainsAny(text, ";#") {
		t.Errorf("label contains Mermaid syntax breaker: %q", line)
	}
}
