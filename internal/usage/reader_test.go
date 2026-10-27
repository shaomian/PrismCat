package usage

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"io"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

func compressUsage(t *testing.T, encoding string, body []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	var writer io.WriteCloser
	var err error
	switch encoding {
	case "gzip":
		writer = gzip.NewWriter(&out)
	case "deflate":
		writer, err = flate.NewWriter(&out, flate.DefaultCompression)
	case "br":
		writer = brotli.NewWriter(&out)
	case "zstd":
		writer, err = zstd.NewWriter(&out, zstd.WithEncoderConcurrency(1))
	default:
		t.Fatal("unsupported test encoding")
	}
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// 复现压缩比超过 32 倍时，位于长 SSE/JSON 尾部的 usage 被截掉。
func TestExtractHighCompressionRatioUsage(t *testing.T) {
	for _, encoding := range []string{"gzip", "deflate", "br", "zstd"} {
		for _, streaming := range []bool{false, true} {
			name := encoding + "/json"
			contentType := "application/json"
			body := []byte(`{"content":"` + strings.Repeat("repeated text ", 20000) + `","usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}`)
			if streaming {
				name = encoding + "/sse"
				contentType = "text/event-stream; charset=utf-8"
				body = []byte(strings.Repeat("data: {\"choices\":[{\"delta\":{\"content\":\"重复文本\"}}]}\n\n", 5000) +
					"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":123,\"completion_tokens\":45,\"total_tokens\":168}}\n\ndata: [DONE]\n\n")
			}
			t.Run(name, func(t *testing.T) {
				compressed := compressUsage(t, encoding, body)
				if len(body) <= len(compressed)*32 {
					t.Fatal("fixture does not reproduce the compression ratio bug")
				}
				got := Extract(testConfig(), "openai", contentType, encoding, compressed)
				if got.InputTokens == nil || *got.InputTokens != 123 || got.OutputTokens == nil || *got.OutputTokens != 45 || got.TotalTokens == nil || *got.TotalTokens != 168 {
					t.Fatalf("trailing usage not extracted: %+v", got)
				}
			})
		}
	}
}

func TestUsageReaderStackedEncodingsAndLimit(t *testing.T) {
	body := []byte(strings.Repeat("read-only evidence\n", 10000))
	compressed := compressUsage(t, "br", compressUsage(t, "gzip", body))
	reader, err := newUsageReader(compressed, "gzip, br", maxUsageDecompressBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(decoded, body) {
		t.Fatalf("stacked encoding decode failed: bytes=%d err=%v", len(decoded), err)
	}
	reader, err = newUsageReader(compressUsage(t, "gzip", body), "gzip", 4096)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoded, err = io.ReadAll(reader)
	if err != nil || len(decoded) != 4096 {
		t.Fatalf("decompression limit not respected: bytes=%d err=%v", len(decoded), err)
	}
}

func TestExtractRejectsUndecodableUsage(t *testing.T) {
	body := []byte(`{"usage":{"total_tokens":42}}`)
	for _, encoding := range []string{"unknown", "gzip", "zstd"} {
		if got := Extract(testConfig(), "openai", "application/json", encoding, body); got.TotalTokens != nil {
			t.Fatalf("encoded body used without successful decoding: encoding=%s %+v", encoding, got)
		}
	}
}
