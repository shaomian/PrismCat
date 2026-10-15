package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/paopaoandlingyia/PrismCat/internal/analysis"
	"github.com/paopaoandlingyia/PrismCat/internal/timeline"
)

// analysisContextEventChars 分析上下文中单事件内容上限（比展示视图更紧凑）
const analysisContextEventChars = 600

// analysisToolResultChars 单次工具调取原文的最大返回字符数
const analysisToolResultChars = 12000

// handleAnalysisChat AI 分析对话: POST /api/analysis/chat
//
// 请求体:
//
//	{
//	  "trace_id": "xxx",
//	  "messages": [{"role":"user","content":"这个 agent 做了什么？"}, ...]  // 客户端持有的多轮对话
//	}
//
// 响应为 SSE，事件类型：
//
//	data: {"delta":"..."}                                    最终回答的文本增量
//	data: {"tool_call":{"name":"get_request_detail","args":"..."}}  模型发起原文调取
//	data: {"tool_result":{"name":"...","chars":1234}}        调取完成
//	data: {"reset":true}                                     本轮含工具调用，清空临时文本
//	data: {"done":true,"usage":{...},"rounds":2,"tool_calls":1}
//	data: {"error":"..."}                                    出错
func (h *Handler) handleAnalysisChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}

	cfg := h.cfg.AnalysisSnapshot()
	if !cfg.Enabled || cfg.BaseURL == "" || cfg.Model == "" {
		h.jsonError(w, "AI 分析未配置：请先在分析面板中配置模型（base_url / api_key / model）", http.StatusBadRequest)
		return
	}

	var req struct {
		TraceID  string             `json:"trace_id"`
		Messages []analysis.Message `json:"messages"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, "无效的请求体", http.StatusBadRequest)
		return
	}
	req.TraceID = strings.TrimSpace(req.TraceID)
	if req.TraceID == "" {
		h.jsonError(w, "缺少 trace ID", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 || req.Messages[len(req.Messages)-1].Role != "user" {
		h.jsonError(w, "最后一条消息必须是用户提问", http.StatusBadRequest)
		return
	}
	// 对话历史限额，防止滥用
	if len(req.Messages) > 50 {
		h.jsonError(w, "对话轮数过多", http.StatusBadRequest)
		return
	}
	for _, m := range req.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			h.jsonError(w, "消息角色仅支持 user/assistant", http.StatusBadRequest)
			return
		}
		if len(m.Content) > 128*1024 {
			h.jsonError(w, "单条消息过大", http.StatusBadRequest)
			return
		}
	}

	requests, err := h.repo.GetTraceRequests(req.TraceID)
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(requests) == 0 {
		h.jsonError(w, "trace 不存在", http.StatusNotFound)
		return
	}

	// 构建压缩时间线上下文（确定性压缩，无需 LLM）
	tl := timeline.Build(req.TraceID, h.traceTimelineInputs(r, requests),
		timeline.Options{MaxEventChars: analysisContextEventChars})
	contextText := timeline.FormatText(tl, timeline.FormatOptions{
		MaxEventChars: analysisContextEventChars,
		MaxTotalChars: 60000,
	})
	messages := analysis.BuildMessages(contextText, req.Messages)

	// SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, _ := w.(http.Flusher)

	writeEvent := func(payload map[string]any) bool {
		data, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := w.Write(append(append([]byte("data: "), data...), '\n', '\n')); err != nil {
			return false
		}
		if flusher != nil {
			flusher.Flush()
		}
		return true
	}

	exec := &traceToolExecutor{h: h, traceID: req.TraceID}
	result, err := analysis.RunChat(r.Context(), cfg, messages,
		analysis.StreamOptions{TraceID: req.TraceID},
		exec,
		analysis.Callbacks{
			OnDelta: func(delta string) error {
				if !writeEvent(map[string]any{"delta": delta}) {
					return errClientClosed
				}
				return nil
			},
			OnToolCall: func(name, args string) error {
				if !writeEvent(map[string]any{"tool_call": map[string]any{"name": name, "args": args}}) {
					return errClientClosed
				}
				return nil
			},
			OnToolResult: func(name string, resultChars int) error {
				if !writeEvent(map[string]any{"tool_result": map[string]any{"name": name, "chars": resultChars}}) {
					return errClientClosed
				}
				return nil
			},
			OnReset: func() error {
				if !writeEvent(map[string]any{"reset": true}) {
					return errClientClosed
				}
				return nil
			},
		})
	if err != nil {
		writeEvent(map[string]any{"error": err.Error()})
		return
	}
	writeEvent(map[string]any{
		"done":       true,
		"usage":      result.Usage,
		"rounds":     result.Rounds,
		"tool_calls": result.ToolCalls,
	})
}

// errClientClosed 客户端断开时的哨兵错误（SSE 写失败即静默终止）
var errClientClosed = errClosed{}

type errClosed struct{}

func (errClosed) Error() string { return "client closed" }

// traceToolExecutor 分析工具执行器：按日志 ID 调取原始请求/响应全文
type traceToolExecutor struct {
	h       *Handler
	traceID string
}

func (e *traceToolExecutor) ExecuteToolCall(ctx context.Context, name string, args json.RawMessage) (string, error) {
	if name != analysis.ToolGetRequestDetail {
		return "", fmt.Errorf("未知工具: %s", name)
	}

	var params struct {
		LogID string `json:"log_id"`
		Part  string `json:"part"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	if strings.TrimSpace(params.LogID) == "" {
		return "", fmt.Errorf("缺少 log_id 参数")
	}
	part := strings.ToLower(strings.TrimSpace(params.Part))
	if part == "" {
		part = "both"
	}

	log, err := e.h.repo.GetLog(params.LogID)
	if err != nil {
		return "", fmt.Errorf("日志 %s 不存在", params.LogID)
	}
	// 限定只能调取本调用链内的请求，防止跨链读取
	if log.TraceID != e.traceID {
		return "", fmt.Errorf("日志 %s 不属于当前调用链", params.LogID)
	}

	logging := e.h.cfg.LoggingSnapshot()
	var b strings.Builder
	fmt.Fprintf(&b, "[日志ID=%s] 上游=%s %s 状态=%d 耗时=%dms 流式=%v",
		log.ID, log.Upstream, log.Path, log.StatusCode, log.Latency, log.Streaming)
	if log.Error != "" {
		fmt.Fprintf(&b, "\n[错误] %s", log.Error)
	}

	appendBody := func(label, inline, ref string, headers map[string][]string, maxBytes int64) {
		if part != "both" && part != label {
			return
		}
		body := e.h.recoverBody(ctx, inline, ref, headers, maxBytes)
		if strings.TrimSpace(body) == "" {
			fmt.Fprintf(&b, "\n%s体: (空)", label)
			return
		}
		fmt.Fprintf(&b, "\n%s体:\n%s", label, body)
	}
	appendBody("请求", log.RequestBody, log.RequestBodyRef, log.RequestHeaders, logging.MaxRequestBody)
	appendBody("响应", log.ResponseBody, log.ResponseBodyRef, log.ResponseHeaders, logging.MaxResponseBody)

	out := b.String()
	if runes := []rune(out); len(runes) > analysisToolResultChars {
		out = string(runes[:analysisToolResultChars]) + "\n…(内容过长已截断)"
	}
	return out, nil
}
