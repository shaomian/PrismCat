package api

import (
	"context"
	"net/http"

	"github.com/paopaoandlingyia/PrismCat/internal/httpbody"
	"github.com/paopaoandlingyia/PrismCat/internal/storage"
)

// handleTraceTimeline 构建调用链的对话时间线: GET /api/traces/{id}/timeline
func (h *Handler) handleTraceTimeline(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	tl := h.buildTraceTimeline(w, r, traceID)
	if tl == nil {
		return
	}
	h.jsonResponse(w, tl)
}

// recoverBody 还原完整 body：优先从 blob 存储按 ref 取回原始内容并解码，
// 否则回退到内联预览（可能已被截断，解析层会优雅降级）。
func (h *Handler) recoverBody(ctx context.Context, inline, ref string, headers map[string][]string, maxBytes int64) string {
	if ref == "" || h.blobs == nil {
		return inline
	}
	data, err := h.blobs.Get(ctx, ref)
	if err != nil {
		return inline
	}
	formatted := httpbody.FormatForDisplay(
		storage.FirstHeaderValue(headers, "Content-Type"),
		storage.FirstHeaderValue(headers, "Content-Encoding"),
		data,
		httpbody.FormatOptions{
			MaxOutputBytes:               maxBytes,
			TrimLargeBase64:              true,
			RequireContentEncodingDecode: true,
		},
	)
	if formatted.DecodeFailed || formatted.Binary {
		return inline
	}
	return formatted.Text
}
