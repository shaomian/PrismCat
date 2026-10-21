package api

import (
	"context"
	"math"
	"net/http"

	"github.com/shaomian/PrismCat/internal/httpbody"
	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/timeline"
)

// handleTraceTimeline 构建调用链的对话时间线: GET /api/traces/{id}/timeline
//
// ?full_log=LOGID 变体：不返回整条时间线，而是返回该请求的单个条目，
// 且事件内容不做截断（默认为控制响应体量按 2000 字符截断）。
// 前端展开被截断的内容时按需调取；与列表构建走同一 Build 逻辑，
// 保证事件数量、顺序与列表完全一致，客户端可按 log_id 原位替换。
func (h *Handler) handleTraceTimeline(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	if fullLog := r.URL.Query().Get("full_log"); fullLog != "" {
		tl := h.buildTraceTimelineOpts(w, r, traceID, timeline.Options{MaxEventChars: math.MaxInt})
		if tl == nil {
			return
		}
		for i := range tl.Requests {
			if tl.Requests[i].LogID == fullLog {
				h.jsonResponse(w, tl.Requests[i])
				return
			}
		}
		h.jsonError(w, "请求不存在", http.StatusNotFound)
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
			MaxOutputBytes: maxBytes,
			// 与日志详情/导出路径保持一致：store_base64: true 时保留
			// base64 内容，否则与采集时同样裁剪大段 base64
			TrimLargeBase64:              !h.cfg.LoggingSnapshot().StoreBase64,
			RequireContentEncodingDecode: true,
		},
	)
	if formatted.DecodeFailed || formatted.Binary {
		return inline
	}
	return formatted.Text
}
