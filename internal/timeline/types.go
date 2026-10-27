// Package timeline 将调用链中的原始 HTTP 请求/响应重建为可读的对话事件时间线。
//
// 核心能力：
//   - 识别 OpenAI Chat / Anthropic Messages / Gemini / OpenAI Responses 四种 API 格式
//   - 将 SSE 流式响应合并为完整消息
//   - 跨请求做前缀去重：Agent 循环中每个请求都携带完整历史，
//     时间线只展示"本次新增的消息 + 本次响应"，重复的历史消息折叠为一个计数。
package timeline

import "github.com/shaomian/PrismCat/internal/trace"

// Event 时间线上的一个对话事件
type Event struct {
	Role       string `json:"role"`                   // system / user / assistant / tool
	Kind       string `json:"kind"`                   // message / tool_call / tool_result / thinking / image
	Content    string `json:"content,omitempty"`      // 文本内容（可能被截断）
	Truncated  bool   `json:"truncated,omitempty"`    // 内容是否被截断
	ToolName   string `json:"tool_name,omitempty"`    // 工具名（tool_call / tool_result）
	ToolCallID string `json:"tool_call_id,omitempty"` // 工具调用 ID
	ToolArgs   string `json:"tool_args,omitempty"`    // 工具调用参数（JSON 字符串，可能被截断）
}

// Input 构建时间线所需的单条日志输入（与 storage 层解耦）
type Input struct {
	TraceAssociation *trace.Assignment
	LogID            string
	Seq              int
	CreatedAtMs      int64
	LatencyMs        int64
	StatusCode       int
	Error            string
	Upstream         string
	Path             string
	Streaming        bool

	RequestBody  string
	ResponseBody string

	UsageInputTokens  *int64
	UsageOutputTokens *int64
}

// RequestEntry 调用链中单个请求的时间线条目
type RequestEntry struct {
	TraceAssociation *trace.Assignment `json:"trace_association,omitempty"`
	LogID            string            `json:"log_id"`
	Seq              int               `json:"seq"`
	CreatedAt        int64             `json:"created_at"` // unix 毫秒
	LatencyMs        int64             `json:"latency_ms"`
	StatusCode       int               `json:"status_code"`
	Error            string            `json:"error,omitempty"`
	Upstream         string            `json:"upstream"`
	Path             string            `json:"path"`

	Model  string `json:"model,omitempty"` // 从请求体解析出的模型名
	Format string `json:"format"`          // openai-chat / anthropic / gemini / openai-responses / unknown

	Carried        int     `json:"carried"` // 从上一请求上下文中继承（折叠）的消息数
	NewEvents      []Event `json:"new_events"`
	ResponseEvents []Event `json:"response_events"`

	ParseError string `json:"parse_error,omitempty"` // 解析失败原因（不阻断整条链）

	UsageInputTokens  *int64 `json:"usage_input_tokens,omitempty"`
	UsageOutputTokens *int64 `json:"usage_output_tokens,omitempty"`
}

// Summary 整条时间线的汇总
type Summary struct {
	RequestCount   int `json:"request_count"`
	ParsedCount    int `json:"parsed_count"`     // 成功识别为 LLM 对话的请求数
	TotalNewEvents int `json:"total_new_events"` // 新增事件总数
	TotalCarried   int `json:"total_carried"`    // 折叠的历史消息总数
}

// Timeline 调用链的完整对话时间线
type Timeline struct {
	TraceID  string         `json:"trace_id"`
	Requests []RequestEntry `json:"requests"`
	Summary  Summary        `json:"summary"`
}

// Options 构建选项
type Options struct {
	MaxEventChars int // 单个事件内容最大字符数，0 使用默认值 2000
}

const defaultMaxEventChars = 2000

// DefaultEventChars 单个事件内容的默认最大字符数（Options.MaxEventChars <= 0 时生效）。
// 供缓存派生等场景引用。
const DefaultEventChars = defaultMaxEventChars

func (o Options) maxEventChars() int {
	if o.MaxEventChars > 0 {
		return o.MaxEventChars
	}
	return defaultMaxEventChars
}
