package timeline

import (
	"strings"
	"testing"
)

func TestDiagram(t *testing.T) {
	tl := Build("trace-1", openAIChain(), Options{})
	src := Diagram(tl, DiagramOptions{})

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
}

func TestDiagramLabelTruncation(t *testing.T) {
	long := strings.Repeat("x", 500)
	items := []Input{{
		LogID: "1", Path: "/v1/chat/completions",
		RequestBody:  `{"messages":[{"role":"user","content":"` + long + `"}]}`,
		ResponseBody: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`,
	}}
	tl := Build("t", items, Options{})
	src := Diagram(tl, DiagramOptions{LabelChars: 40})
	for _, line := range strings.Split(src, "\n") {
		if len([]rune(line)) > 150 { // 前缀 + 40 + 省略号
			t.Errorf("line too long (%d runes): %q", len([]rune(line)), line)
		}
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

	src := Diagram(Build("t", items, Options{}), DiagramOptions{})

	for _, want := range []string{
		"＃define",   // 用户消息中的 # 被转义
		"；",          // 分号被转义
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
