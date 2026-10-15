package timeline

import (
	"fmt"
	"strings"
)

// FormatOptions 文本序列化选项
type FormatOptions struct {
	MaxEventChars int // 单个事件内容最大字符数，0 用默认值 800
	MaxTotalChars int // 全文最大字符数（超出则保留首尾、省略中间请求），0 用默认值 60000
}

const (
	defaultFormatEventChars = 800
	defaultFormatTotalChars = 60000
)

// FormatText 将时间线序列化为 LLM 友好的紧凑文本。
//
// 专为"喂给分析模型"设计：相比 JSON，去掉了结构噪音，每行一个事件，
// 并内置两级压缩——单事件截断 + 全文超限时保留首尾请求、省略中段。
func FormatText(tl *Timeline, opts FormatOptions) string {
	maxEvent := opts.MaxEventChars
	if maxEvent <= 0 {
		maxEvent = defaultFormatEventChars
	}
	maxTotal := opts.MaxTotalChars
	if maxTotal <= 0 {
		maxTotal = defaultFormatTotalChars
	}

	sections := make([]string, 0, len(tl.Requests))
	for _, req := range tl.Requests {
		sections = append(sections, formatRequest(req, maxEvent))
	}

	header := fmt.Sprintf("调用链 %s：共 %d 个请求，%d 个新增事件，折叠重复历史 %d 条",
		tl.TraceID, tl.Summary.RequestCount, tl.Summary.TotalNewEvents, tl.Summary.TotalCarried)

	total := len(header)
	for _, s := range sections {
		total += len(s) + 2
	}
	if total <= maxTotal || len(sections) <= 2 {
		return header + "\n\n" + strings.Join(sections, "\n\n")
	}

	// 全文超限：保留第一个请求（任务起点）+ 尾部尽可能多的请求（最新进展），
	// 中段请求降级为单行摘要（保留日志 ID，供 AI drill-down 按需调取原文）
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\n")
	b.WriteString(sections[0])

	budget := maxTotal - len(header) - len(sections[0]) - 160 // 省略摘要区预留
	var tail []string
	for i := len(sections) - 1; i >= 1; i-- {
		if budget-len(sections[i])-2 < 0 {
			break
		}
		tail = append([]string{sections[i]}, tail...)
		budget -= len(sections[i]) + 2
	}
	omittedCount := len(sections) - 1 - len(tail)
	if omittedCount > 0 {
		fmt.Fprintf(&b, "\n\n…（中间 %d 个请求的详细事件已省略，仅保留摘要，如需原文可调取工具 get_request_detail）…\n", omittedCount)
		// 中段摘要：每请求一行
		firstOmitted := 1
		lastOmitted := len(sections) - 1 - len(tail)
		for i := firstOmitted; i <= lastOmitted; i++ {
			b.WriteString(requestDigest(tl.Requests[i]))
			b.WriteString("\n")
		}
	}
	for _, s := range tail {
		b.WriteString("\n\n")
		b.WriteString(s)
	}
	return b.String()
}

// requestDigest 生成单个请求的单行摘要（用于省略段，保留 drill-down 所需的日志 ID）
func requestDigest(req RequestEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[请求 #%d](日志ID=%s) 状态=%d", req.Seq, req.LogID, req.StatusCode)
	if req.Model != "" {
		fmt.Fprintf(&b, " 模型=%s", req.Model)
	}
	if req.Carried > 0 {
		fmt.Fprintf(&b, " 复用%d条历史", req.Carried)
	}
	// 工具动作
	var tools []string
	preview := ""
	for _, ev := range req.NewEvents {
		if ev.Kind == "tool_result" && ev.ToolName != "" {
			tools = append(tools, ev.ToolName+"结果")
		}
		if preview == "" && ev.Kind == "message" && ev.Role == "user" {
			preview = truncateOneLine(ev.Content, 40)
		}
	}
	for _, ev := range req.ResponseEvents {
		if ev.Kind == "tool_call" && ev.ToolName != "" {
			tools = append(tools, "调用"+ev.ToolName)
		}
		if preview == "" && ev.Kind == "message" {
			preview = truncateOneLine(ev.Content, 40)
		}
	}
	if req.Error != "" {
		tools = append(tools, "错误:"+truncateOneLine(req.Error, 30))
	}
	if len(tools) > 0 {
		fmt.Fprintf(&b, " 动作: %s", strings.Join(tools, ", "))
	}
	if preview != "" {
		fmt.Fprintf(&b, " 内容: %s", preview)
	}
	return b.String()
}

// truncateOneLine 压平为单行并截断
func truncateOneLine(s string, maxChars int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if maxChars > 0 && len(runes) > maxChars {
		return string(runes[:maxChars]) + "…"
	}
	return s
}

func formatRequest(req RequestEntry, maxEvent int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[请求 #%d](日志ID=%s) 上游=%s %s", req.Seq, req.LogID, req.Upstream, req.Path)
	if req.Model != "" {
		fmt.Fprintf(&b, " | 模型=%s", req.Model)
	}
	fmt.Fprintf(&b, " | 状态=%d | 耗时=%dms", req.StatusCode, req.LatencyMs)
	if req.UsageInputTokens != nil || req.UsageOutputTokens != nil {
		b.WriteString(" | tokens:")
		if req.UsageInputTokens != nil {
			fmt.Fprintf(&b, " in=%d", *req.UsageInputTokens)
		}
		if req.UsageOutputTokens != nil {
			fmt.Fprintf(&b, " out=%d", *req.UsageOutputTokens)
		}
	}
	if req.Carried > 0 {
		fmt.Fprintf(&b, "\n（复用 %d 条历史消息，内容同上，不再重复展示）", req.Carried)
	}
	if req.ParseError != "" {
		fmt.Fprintf(&b, "\n[解析异常] %s（该请求展示可能不完整）", req.ParseError)
	}
	if req.Error != "" {
		fmt.Fprintf(&b, "\n[请求错误] %s", req.Error)
	}

	writeEvents := func(prefix string, events []Event) {
		for _, ev := range events {
			content, _ := truncateRunes(ev.Content, maxEvent)
			args, _ := truncateRunes(ev.ToolArgs, maxEvent)
			switch ev.Kind {
			case "tool_call":
				fmt.Fprintf(&b, "\n%s [assistant 调用工具: %s] %s", prefix, ev.ToolName, args)
			case "tool_result":
				name := ev.ToolName
				if name == "" {
					name = ev.ToolCallID
				}
				fmt.Fprintf(&b, "\n%s [工具结果: %s] %s", prefix, name, content)
			case "thinking":
				fmt.Fprintf(&b, "\n%s [%s 思考] %s", prefix, ev.Role, content)
			case "image":
				fmt.Fprintf(&b, "\n%s [%s 图片]", prefix, ev.Role)
			default:
				fmt.Fprintf(&b, "\n%s [%s] %s", prefix, ev.Role, content)
			}
		}
	}

	writeEvents("NEW ", req.NewEvents)
	writeEvents("RESP", req.ResponseEvents)
	return b.String()
}
