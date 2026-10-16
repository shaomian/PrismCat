package api

import (
	"strings"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

func TestIsSafeInlineBlobType(t *testing.T) {
	cases := []struct {
		contentType string
		safe        bool
	}{
		{"text/plain; charset=utf-8", true},
		{"application/json", true},
		{"image/png", true},
		{"audio/mpeg", true},
		{"video/mp4", true},
		// 能执行脚本的类型必须强制 attachment，否则存储型 XSS
		{"text/html; charset=utf-8", false},
		{"application/xhtml+xml", false},
		{"application/xml", false},
		{"application/pdf", false},
		{"image/svg+xml", false}, // SVG 可内嵌 <script>
		{"application/octet-stream", false},
	}
	for _, tc := range cases {
		if got := isSafeInlineBlobType(tc.contentType); got != tc.safe {
			t.Errorf("isSafeInlineBlobType(%q) = %v, want %v", tc.contentType, got, tc.safe)
		}
	}
}

func TestResolveReplayTargetFromUpstream(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			Upstreams: map[string]config.UpstreamConfig{
				"openai": {
					Target:        "https://api.openai.com/base",
					Timeout:       42,
					OutboundProxy: "direct",
				},
			},
		},
	}

	fullURL, host, outboundProxy, timeout, err := h.resolveReplayTarget("openai", "", "/v1/chat/completions?debug=1")
	if err != nil {
		t.Fatalf("resolveReplayTarget returned error: %v", err)
	}
	if fullURL != "https://api.openai.com/base/v1/chat/completions?debug=1" {
		t.Fatalf("fullURL = %q", fullURL)
	}
	if host != "api.openai.com" {
		t.Fatalf("host = %q", host)
	}
	if outboundProxy != "direct" {
		t.Fatalf("outboundProxy = %q", outboundProxy)
	}
	if timeout != 42 {
		t.Fatalf("timeout = %d", timeout)
	}
}

func TestResolveReplayTargetFromCustomURL(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}

	fullURL, host, outboundProxy, timeout, err := h.resolveReplayTarget("", "https://example.com/v1/messages?q=1", "")
	if err != nil {
		t.Fatalf("resolveReplayTarget returned error: %v", err)
	}
	if fullURL != "https://example.com/v1/messages?q=1" {
		t.Fatalf("fullURL = %q", fullURL)
	}
	if host != "example.com" {
		t.Fatalf("host = %q", host)
	}
	if outboundProxy != "env" {
		t.Fatalf("outboundProxy = %q", outboundProxy)
	}
	if timeout != config.DefaultUpstreamTimeoutSeconds {
		t.Fatalf("timeout = %d", timeout)
	}
}

func TestResolveReplayTargetRejectsUnsupportedCustomURL(t *testing.T) {
	h := &Handler{cfg: &config.Config{}}

	_, _, _, _, err := h.resolveReplayTarget("", "ftp://example.com/file", "")
	if err == nil {
		t.Fatal("resolveReplayTarget succeeded, want error")
	}
	if !strings.Contains(err.Error(), "http 或 https") {
		t.Fatalf("error = %q", err)
	}
}
