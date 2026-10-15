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
		"A->>L: #1 请求",
		"L-->>A: 调用工具 read_file",
		"A->>T: read_file(",
		"T-->>A: read_file → package main...",
		"A->>L: #2 请求",
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
