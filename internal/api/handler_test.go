package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
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

// traceDetailRepo 只实现 handleTraceDetail 用到的 Repository 方法
type traceDetailRepo struct {
	storage.Repository
}

func (r *traceDetailRepo) GetTraceRequests(traceID string) ([]*storage.RequestLog, error) {
	return []*storage.RequestLog{{
		ID:        "l1",
		TraceID:   traceID,
		Method:    "POST",
		Path:      "/v1/messages",
		Latency:   42,
		Upstream:  "test",
		CreatedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),

		RequestBody:         "REQ-BODY",
		RequestBodyOriginal: "REQ-ORIGINAL",
		RequestBodyFinal:    "REQ-FINAL",
		RequestBodySize:     100,
		ResponseBody:        "RESP-BODY",
		ResponseBodySize:    200,
	}}, nil
}

// 详情接口必须瘦身（不含 body 全文）：请求视图卡片只用元数据，body 由
// 前端在点开 LogDetail 时按需拉取单条完整日志。重链的 body 可达数十 MB。
func TestHandleTraceDetailOmitsBodies(t *testing.T) {
	h := &Handler{repo: &traceDetailRepo{}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/traces/t1", nil)

	h.handleTraceDetail(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var payload struct {
		Requests []map[string]interface{} `json:"requests"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(payload.Requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(payload.Requests))
	}
	row := payload.Requests[0]
	for _, field := range []string{"request_body", "request_body_original", "request_body_final", "response_body"} {
		if _, ok := row[field]; ok {
			t.Errorf("detail response should omit %q, got %v", field, row[field])
		}
	}
	// 元数据保留：卡片大小展示与 blob 附件判断依赖
	if row["request_body_size"] != float64(100) || row["response_body_size"] != float64(200) {
		t.Fatalf("body sizes missing from slim response: %v", row)
	}
	if row["method"] != "POST" {
		t.Fatalf("method missing from slim response: %v", row)
	}
}
