package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shaomian/PrismCat/internal/httpbody"
	"github.com/shaomian/PrismCat/internal/storage"
	"github.com/shaomian/PrismCat/internal/timeline"
)

const (
	bodySearchLogLimit   = 500
	bodySearchHitLimit   = 50
	bodySearchByteLimit  = 128 << 20
	bodySearchBodyLimit  = 32 << 20
	bodySearchToolLimit  = 2 << 20
	bodySearchQueryLimit = 256
)

var errBodySearchLimit = errors.New("body search scan limit reached")

type bodySearchHit struct {
	LogID     string    `json:"log_id"`
	CreatedAt time.Time `json:"created_at"`
	Upstream  string    `json:"upstream"`
	Method    string    `json:"method"`
	Path      string    `json:"path"`
	Part      string    `json:"part"`
	Tool      string    `json:"tool,omitempty"`
	Before    string    `json:"before"`
	Match     string    `json:"match"`
	After     string    `json:"after"`
	priority  int
}

type bodySearchResult struct {
	Hits       []bodySearchHit          `json:"hits"`
	Scanned    int                      `json:"scanned"`
	Matched    int                      `json:"matched"`
	Incomplete int                      `json:"incomplete"`
	Limited    bool                     `json:"limited"`
	Next       *storage.LogBodyPosition `json:"next,omitempty"`
}

func (h *Handler) handleBodySearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.jsonError(w, "方法不允许", http.StatusMethodNotAllowed)
		return
	}
	var input struct {
		Query  string                   `json:"query"`
		Before *storage.LogBodyPosition `json:"before"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || strings.TrimSpace(input.Query) == "" || utf8.RuneCountInString(input.Query) > bodySearchQueryLimit {
		h.jsonError(w, "请输入 1 至 256 个字符", http.StatusBadRequest)
		return
	}
	traceID := r.URL.Query().Get("trace_id")
	if strings.TrimSpace(traceID) == "" || (input.Before != nil && input.Before.ID == "") {
		h.jsonError(w, "缺少调用链 ID 或搜索位置无效", http.StatusBadRequest)
		return
	}
	repo, ok := h.repo.(storage.TraceBodySearchRepository)
	if !ok {
		h.jsonError(w, "正文搜索不可用", http.StatusServiceUnavailable)
		return
	}
	if !h.bodySearchMu.TryLock() {
		h.jsonError(w, "已有正文搜索正在运行", http.StatusTooManyRequests)
		return
	}
	defer h.bodySearchMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	result := bodySearchResult{Hits: []bodySearchHit{}}
	var last *storage.LogBodyPosition
	remaining := int64(bodySearchByteLimit)
	trimBase64 := !h.cfg.LoggingSnapshot().StoreBase64
	err := repo.VisitTraceLogBodies(ctx, traceID, input.Before, bodySearchLogLimit+1, func(log *storage.RequestLog, position storage.LogBodyPosition) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if result.Scanned >= bodySearchLogLimit || len(result.Hits) >= bodySearchHitLimit || remaining <= 0 {
			return errBodySearchLimit
		}
		response, responsePartial, err := h.readBodyForSearch(ctx, log.ResponseBody, log.ResponseBodyRef, log.ResponseHeaders, trimBase64, &remaining)
		if err != nil {
			return err
		}
		request, requestPartial, err := h.readBodyForSearch(ctx, log.RequestBody, log.RequestBodyRef, log.RequestHeaders, trimBase64, &remaining)
		if err != nil {
			return err
		}
		hit, found := findLogBodyMatch(log, request, response, input.Query)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result.Scanned++
		last = &position
		if log.Truncated || requestPartial || responsePartial ||
			(len(response) > bodySearchToolLimit && log.Streaming && mayContainToolCalls(response)) {
			result.Incomplete++
		}
		if found {
			result.Hits = append(result.Hits, hit)
			result.Matched++
		}
		return nil
	})
	if r.Context().Err() != nil {
		return
	}
	switch {
	case errors.Is(err, errBodySearchLimit), errors.Is(ctx.Err(), context.DeadlineExceeded):
		result.Limited = true
		result.Next = last
	case err != nil:
		h.jsonError(w, "正文搜索失败", http.StatusInternalServerError)
		return
	}
	slices.SortStableFunc(result.Hits, func(a, b bodySearchHit) int { return b.priority - a.priority })
	w.Header().Set("Cache-Control", "no-store")
	h.jsonResponse(w, result)
}

func (h *Handler) readBodyForSearch(ctx context.Context, preview, ref string, headers map[string][]string, trimBase64 bool, remaining *int64) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	limit := min(*remaining, bodySearchBodyLimit)
	if limit == 0 && ref != "" {
		return "", false, errBodySearchLimit
	}
	partial := ref != ""
	if ref != "" {
		if store, ok := h.blobs.(interface {
			GetLimited(context.Context, string, int64) ([]byte, error)
		}); ok {
			body, err := store.GetLimited(ctx, ref, limit)
			if errors.Is(err, storage.ErrBlobTooLarge) && limit < bodySearchBodyLimit {
				return "", false, errBodySearchLimit
			}
			if err == nil {
				formatted := httpbody.FormatPreviewForDisplay(
					storage.FirstHeaderValue(headers, "Content-Type"),
					storage.FirstHeaderValue(headers, "Content-Encoding"),
					body,
					httpbody.FormatOptions{MaxOutputBytes: limit, TrimLargeBase64: trimBase64, RequireContentEncodingDecode: true},
				)
				*remaining -= int64(max(len(body), len(formatted.Text)))
				if formatted.Binary || formatted.DecodeFailed {
					return "", true, nil
				}
				if formatted.Truncated && limit < bodySearchBodyLimit {
					return "", false, errBodySearchLimit
				}
				return formatted.Text, formatted.Truncated, nil
			}
		}
	}
	if int64(len(preview)) > limit {
		if limit < bodySearchBodyLimit {
			return "", false, errBodySearchLimit
		}
		end := int(limit)
		for end > 0 && !utf8.RuneStart(preview[end]) {
			end--
		}
		preview, partial = preview[:end], true
	}
	*remaining -= int64(len(preview))
	return preview, partial, nil
}

func findLogBodyMatch(log *storage.RequestLog, request, response, query string) (bodySearchHit, bool) {
	base := bodySearchHit{LogID: log.ID, CreatedAt: log.CreatedAt, Upstream: log.Upstream, Method: log.Method, Path: log.Path}
	// The response that emitted a tool call ranks before a request carrying its history.
	for _, part := range []struct {
		name, body string
		priority   int
	}{{"response", response, 2}, {"request", request, 1}} {
		if len(part.body) > bodySearchToolLimit || !mayContainToolCalls(part.body) {
			continue
		}
		var found bool
		timeline.FindToolCall(log.Path, part.body, part.name == "response", log.Streaming, func(event timeline.Event) bool {
			text := matchingToolArgs(event.ToolArgs, query)
			if text == "" {
				return false
			}
			base.Part, base.Tool, base.priority = part.name, event.ToolName, part.priority
			base.Before, base.Match, base.After, found = bodySearchSnippet(text, query)
			return found
		})
		if found {
			return base, true
		}
	}
	base.Part = "request"
	before, match, after, found := bodySearchSnippet(request, query)
	if !found {
		base.Part = "response"
		before, match, after, found = bodySearchSnippet(response, query)
	}
	base.Before, base.Match, base.After = before, match, after
	return base, found
}

func mayContainToolCalls(body string) bool {
	return strings.Contains(body, "tool_calls") || strings.Contains(body, "tool_use") ||
		strings.Contains(body, "function_call") || strings.Contains(body, "functionCall")
}

func matchingToolArgs(args, query string) string {
	if strings.Contains(args, query) {
		return args
	}
	decoder := json.NewDecoder(strings.NewReader(args))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		if value, ok := token.(string); ok && strings.Contains(value, query) {
			return value
		}
	}
}

func bodySearchSnippet(body, query string) (string, string, string, bool) {
	at := strings.Index(body, query)
	if at < 0 {
		return "", "", "", false
	}
	matchEnd := at + len(query)
	start, end := max(0, at-96), min(len(body), matchEnd+96)
	for start < at && !utf8.RuneStart(body[start]) {
		start++
	}
	for end < len(body) && !utf8.RuneStart(body[end]) {
		end++
	}
	before, after := body[start:at], body[matchEnd:end]
	if start > 0 {
		before = "…" + before
	}
	if end < len(body) {
		after += "…"
	}
	return strings.Clone(before), query, strings.Clone(after), true
}
