package usage

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// 压缩 SSE 的重复帧常超过 32 倍压缩比，不能按压缩大小裁掉尾部 usage。
// 逐帧解压、逐帧解析，并在每层保留绝对读取上限，避免完整解压正文常驻内存。
func newUsageReader(body []byte, contentEncoding string, limit int64) (io.ReadCloser, error) {
	r := &usageReadCloser{Reader: io.LimitReader(bytes.NewReader(body), limit)}
	tokens := strings.Split(contentEncoding, ",")
	for i := len(tokens) - 1; i >= 0; i-- {
		var decoded io.Reader
		switch strings.ToLower(strings.TrimSpace(tokens[i])) {
		case "", "identity":
			continue
		case "gzip":
			decoder, err := gzip.NewReader(r.Reader)
			if err != nil {
				r.Close()
				return nil, err
			}
			r.close = append(r.close, func() { _ = decoder.Close() })
			decoded = decoder
		case "deflate":
			decoder := flate.NewReader(r.Reader)
			r.close = append(r.close, func() { _ = decoder.Close() })
			decoded = decoder
		case "br":
			decoded = brotli.NewReader(r.Reader)
		case "zstd":
			decoder, err := zstd.NewReader(r.Reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(limit)))
			if err != nil {
				r.Close()
				return nil, err
			}
			r.close = append(r.close, decoder.Close)
			decoded = decoder
		default:
			r.Close()
			return nil, fmt.Errorf("unsupported usage content encoding: %s", tokens[i])
		}
		r.Reader = io.LimitReader(decoded, limit)
	}
	return r, nil
}

type usageReadCloser struct {
	io.Reader
	close []func()
}

func (r *usageReadCloser) Close() error {
	for i := len(r.close) - 1; i >= 0; i-- {
		r.close[i]()
	}
	r.close = nil
	return nil
}
