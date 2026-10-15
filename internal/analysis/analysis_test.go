package analysis

import (
	"strings"
	"testing"
)

func TestChatCompletionsURL(t *testing.T) {
	cases := map[string]string{
		"https://api.openai.com/v1":              "https://api.openai.com/v1/chat/completions",
		"https://api.openai.com/v1/":             "https://api.openai.com/v1/chat/completions",
		"https://api.openai.com":                 "https://api.openai.com/v1/chat/completions",
		"http://localhost:8711/_proxy/openai":    "http://localhost:8711/_proxy/openai/v1/chat/completions",
		"http://localhost:8711/_proxy/openai/v1": "http://localhost:8711/_proxy/openai/v1/chat/completions",
		"https://x.com/v1/chat/completions":      "https://x.com/v1/chat/completions",
		"":                                       "",
	}
	for in, want := range cases {
		if got := ChatCompletionsURL(in); got != want {
			t.Errorf("ChatCompletionsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildMessages(t *testing.T) {
	userMsgs := []Message{
		{Role: "user", Content: "这个 agent 做了什么？"},
		{Role: "assistant", Content: "它读取并修复了 main.go"},
		{Role: "user", Content: "用了哪些工具？"},
	}
	msgs := BuildMessages("TIMELINE_CONTEXT", userMsgs)
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || !strings.Contains(msgs[0].Content, "PrismCat") {
		t.Fatalf("first message should be system prompt")
	}
	if msgs[1].Role != "user" || !strings.Contains(msgs[1].Content, "TIMELINE_CONTEXT") {
		t.Fatalf("second message should embed the timeline context")
	}
	if msgs[4].Content != "用了哪些工具？" {
		t.Fatalf("last message mismatch: %+v", msgs[4])
	}
}
