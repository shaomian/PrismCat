package storage

import (
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/shaomian/PrismCat/internal/config"
)

func TestPrepareLogExtractsCompressedUsageBeforeDetachingBody(t *testing.T) {
	blobs := &memBlobStore{}
	cfg := &config.Config{Usage: config.UsageExtractionConfig{
		Enabled: true, Upstreams: map[string]config.UsageExtractionUpstreamBinding{"deepseek": {Enabled: true, RuleNames: []string{"OpenAI compatible"}}},
		Rules: []config.UsageExtractionRule{{Name: "OpenAI compatible", Enabled: true, Paths: config.UsageExtractionPaths{
			InputTokens: []string{"/usage/prompt_tokens"}, OutputTokens: []string{"/usage/completion_tokens"}, TotalTokens: []string{"/usage/total_tokens"},
		}}},
	}}
	cfg.Logging.MaxResponseBody = 2 << 20
	cfg.Logging.DetachBodyOverBytes = 1
	cfg.Logging.BodyPreviewBytes = 128
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer encoder.Close()
	body := strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"repeated\"}}]}\n\n", 5000) +
		"data: {\"usage\":{\"prompt_tokens\":123,\"completion_tokens\":45,\"total_tokens\":168}}\n\ndata: [DONE]\n\n"
	entry := &RequestLog{ID: "compressed-usage", Upstream: "deepseek", ResponseHeaders: map[string][]string{"Content-Type": {"text/event-stream"}, "Content-Encoding": {"zstd"}}, ResponseBodyRaw: encoder.EncodeAll([]byte(body), nil)}
	PrepareLogForPersistence(entry, cfg, blobs)
	if entry.ResponseBodyRef == "" || entry.ResponseBodyRaw != nil || entry.UsageTotalTokens == nil || *entry.UsageTotalTokens != 168 {
		t.Fatalf("compressed usage lost during preparation: ref=%s total=%v", entry.ResponseBodyRef, entry.UsageTotalTokens)
	}
}
