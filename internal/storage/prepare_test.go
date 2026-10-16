package storage

import (
	"strings"
	"testing"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

// 回归测试：body_preview_bytes 为 0（禁用预览）且开启 detach 时，
// 文本 body 此前既无内联预览也不落 blob，会被静默丢失。
func TestPrepareLogStoresBodyWhenPreviewDisabled(t *testing.T) {
	blobs := &memBlobStore{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 1024
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.StoreBase64 = true
	cfg.Logging.DetachBodyOverBytes = 1024
	cfg.Logging.BodyPreviewBytes = 0 // 禁用内联预览

	entry := &RequestLog{
		ID:             "id",
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBodyRaw: []byte(`{"hello":"world"}`),
	}
	PrepareLogForPersistence(entry, cfg, blobs)

	if entry.RequestBodyRef == "" {
		t.Fatal("RequestBodyRef should be set when inline preview is disabled, otherwise the body is silently lost")
	}
	if entry.Truncated {
		t.Fatal("body is recoverable via blob ref, should not be marked truncated")
	}
	if blobs.puts != 1 {
		t.Fatalf("blob puts = %d, want 1", blobs.puts)
	}
}

// 常规配置行为保持不变：小 body 走内联预览，不重复落 blob。
func TestPrepareLogInlinePreviewStillSkipsBlob(t *testing.T) {
	blobs := &memBlobStore{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 1024
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.StoreBase64 = true
	cfg.Logging.DetachBodyOverBytes = 1024
	cfg.Logging.BodyPreviewBytes = 128

	entry := &RequestLog{
		ID:             "id",
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBodyRaw: []byte(`{"hello":"world"}`),
	}
	PrepareLogForPersistence(entry, cfg, blobs)

	if entry.RequestBodyRef != "" {
		t.Fatalf("small body should stay inline without blob, got ref %q", entry.RequestBodyRef)
	}
	if !strings.Contains(entry.RequestBody, `"hello":"world"`) {
		t.Fatalf("RequestBody = %q, want inline preview content", entry.RequestBody)
	}
	if entry.Truncated {
		t.Fatal("small inline body should not be truncated")
	}
}

// 预览截断（超过 body_preview_bytes 但低于 detach 阈值）仍落 blob，可恢复。
func TestPrepareLogTruncatedPreviewStillStoresBlob(t *testing.T) {
	blobs := &memBlobStore{}
	cfg := &config.Config{}
	cfg.Logging.MaxRequestBody = 1024
	cfg.Logging.MaxResponseBody = 1024
	cfg.Logging.StoreBase64 = true
	cfg.Logging.DetachBodyOverBytes = 4096
	cfg.Logging.BodyPreviewBytes = 16

	entry := &RequestLog{
		ID:             "id",
		RequestHeaders: map[string][]string{"Content-Type": {"application/json"}},
		RequestBodyRaw: []byte(`{"hello":"world","more":"data"}`),
	}
	PrepareLogForPersistence(entry, cfg, blobs)

	if entry.RequestBodyRef == "" {
		t.Fatal("truncated preview must be recoverable via blob ref")
	}
	if entry.Truncated {
		t.Fatal("body is recoverable via blob ref, should not be marked truncated")
	}
}