package api

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/timeline"
)

// handleTraceDiagram 生成调用链的 Mermaid 流程图: GET /api/traces/{id}/diagram
func (h *Handler) handleTraceDiagram(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	offset, limit := 0, 0
	for name, target := range map[string]*int{"offset": &offset, "limit": &limit} {
		if value := r.URL.Query().Get(name); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < 0 || (name == "limit" && (n == 0 || n > 100)) {
				h.jsonError(w, "无效的流程图分页参数", http.StatusBadRequest)
				return
			}
			*target = n
		}
	}
	tl := h.buildTraceTimeline(w, r, traceID)
	if tl == nil {
		return
	}
	total := len(tl.Requests)
	if limit == 0 {
		limit = total // 不带分页参数的 API 调用保持返回完整图。
	}
	offset = min(offset, total)
	end := offset + min(limit, total-offset)
	view := *tl
	view.Requests = tl.Requests[offset:end]

	opts := timeline.DiagramOptions{}
	if v := r.URL.Query().Get("label_chars"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			opts.LabelChars = n
		}
	}
	var src string
	var labels []timeline.DiagramLabel
	if r.URL.Query().Get("view") == "dependencies" {
		src = timeline.Dependencies(&view)
	} else {
		src, labels = timeline.Diagram(&view, opts)
	}
	h.jsonResponse(w, map[string]interface{}{
		"mermaid": src, "labels": labels,
		"request_offset": offset, "request_total": total,
		"request_count": end - offset,
	})
}

// handleTraceExport 导出调用链 JSONL: GET /api/traces/{id}/export?mode=events|raw
//
//   - events（默认）：压缩后的对话事件流，每行一个请求（含新增/响应事件），
//     数据量通常为原始日志的 1/10 ~ 1/50，适合直接喂给 LLM 分析。
//   - raw：原始请求/响应完整内容，每行一条日志。
func (h *Handler) handleTraceExport(w http.ResponseWriter, r *http.Request, traceID string) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	mode := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mode")))
	if mode == "" {
		mode = "events"
	}
	if mode != "events" && mode != "raw" {
		h.jsonError(w, "不支持的导出模式", http.StatusBadRequest)
		return
	}

	requests, err := h.repo.GetTraceRequests(traceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(requests) == 0 {
		h.jsonError(w, "trace 不存在", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+traceExportFilename(traceID, mode)+"\"")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	encoder := json.NewEncoder(w)
	flusher, _ := w.(http.Flusher)
	writeLine := func(v interface{}) {
		if encoder.Encode(v) == nil && flusher != nil {
			flusher.Flush()
		}
	}

	if mode == "raw" {
		for _, req := range requests {
			h.fillExportBodies(r.Context(), req)
			writeLine(req)
		}
		return
	}

	// events 模式：复用时间线构建（含前缀去重、SSE 合并；走缓存）
	tl := h.buildTraceTimeline(w, r, traceID)
	if tl == nil {
		return
	}
	writeLine(map[string]interface{}{"type": "summary", "trace_id": traceID, "summary": tl.Summary})
	for _, entry := range tl.Requests {
		writeLine(map[string]interface{}{"type": "request", "request": entry})
	}
}

// buildTraceTimeline 拉取调用链并构建时间线；出错时已写响应，返回 nil
func (h *Handler) buildTraceTimeline(w http.ResponseWriter, r *http.Request, traceID string) *timeline.Timeline {
	return h.buildTraceTimelineOpts(w, r, traceID, timeline.Options{})
}

// buildTraceTimelineOpts 同 buildTraceTimeline，但允许自定义构建选项。
//
// 实际构建带缓存：每次读取前用 GetTraceFingerprint（条数、最新创建
// 时间及修订，走索引）判断失效；完整构建（不截断）按 traceID 缓存（LRU），
// 各种截断级别用 timeline.TruncateEvents 派生。此前每次查看时间线/
// 流程图/分析对话都会对每条日志做全量 blob 恢复 + 解压 + JSON 解析，
// 大调用链上一次点击可达 GB 级 IO。
func (h *Handler) buildTraceTimelineOpts(w http.ResponseWriter, r *http.Request, traceID string, opts timeline.Options) *timeline.Timeline {
	if traceID == "" {
		h.jsonError(w, "缺少 trace ID", http.StatusBadRequest)
		return nil
	}
	fp, err := h.repo.GetTraceFingerprint(traceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if fp.Count == 0 {
		h.jsonError(w, "trace 不存在", http.StatusNotFound)
		return nil
	}

	if cached := h.tlCache.get(traceID, fp); cached != nil {
		return deriveTimeline(cached, opts)
	}

	release, err := h.tlCache.lockBuild(r.Context(), traceID)
	if err != nil {
		return nil
	}
	defer release()
	// 等待期间日志可能更新，重新核对指纹与其他请求刚生成的缓存。
	fp, err = h.repo.GetTraceFingerprint(traceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	if fp.Count == 0 {
		h.jsonError(w, "trace 不存在", http.StatusNotFound)
		return nil
	}
	if cached := h.tlCache.get(traceID, fp); cached != nil {
		return deriveTimeline(cached, opts)
	}

	requests, err := h.repo.GetTraceRequests(traceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return nil
	}
	// 完整构建一次，截断级别由派生产出
	inputs := h.traceTimelineInputs(r, requests)
	if r.Context().Err() != nil {
		return nil
	}
	full := timeline.Build(traceID, inputs,
		timeline.Options{MaxEventChars: math.MaxInt})
	// 输入完整时，纯解析阶段即使遇到客户端取消仍可复用构建结果。
	h.tlCache.put(traceID, fp, full)
	if r.Context().Err() != nil {
		return nil
	}
	return deriveTimeline(full, opts)
}

// traceTimelineInputs 将存储层日志转换为时间线输入（含 blob 还原）
func (h *Handler) traceTimelineInputs(r *http.Request, requests []*storage.RequestLog) []timeline.Input {
	logging := h.cfg.LoggingSnapshot()
	inputs := make([]timeline.Input, 0, len(requests))
	for _, req := range requests {
		if r.Context().Err() != nil {
			return nil
		}
		inputs = append(inputs, timeline.Input{
			TraceAssociation: req.TraceAssociation,
			LogID:            req.ID,
			Seq:              req.TraceSeq,
			CreatedAtMs:      req.CreatedAt.UnixMilli(),
			LatencyMs:        req.Latency,
			StatusCode:       req.StatusCode,
			Error:            req.Error,
			Upstream:         req.Upstream,
			Path:             req.Path,
			Streaming:        req.Streaming,
			RequestBody: h.recoverBody(r.Context(), req.RequestBody, req.RequestBodyRef,
				req.RequestHeaders, logging.MaxRequestBody),
			ResponseBody: h.recoverBody(r.Context(), req.ResponseBody, req.ResponseBodyRef,
				req.ResponseHeaders, logging.MaxResponseBody),
			UsageInputTokens:  req.UsageInputTokens,
			UsageOutputTokens: req.UsageOutputTokens,
		})
	}
	return inputs
}

var unsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func traceExportFilename(traceID, mode string) string {
	safe := unsafeFilenameChars.ReplaceAllString(traceID, "_")
	const maxLen = 48
	if len(safe) > maxLen {
		safe = safe[:maxLen]
	}
	return "prismcat-trace-" + safe + "-" + mode + "-" + time.Now().UTC().Format("20060102T150405Z") + ".jsonl"
}
