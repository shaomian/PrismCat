// Package analysis 将调用链时间线交给 AI 模型进行分析。
//
// 设计要点：
//   - 无状态：每次请求由调用方重建时间线上下文，服务端不存会话
//   - 兼容 OpenAI Chat Completions 协议（含 /v1 路径自动补全）
//   - Agentic drill-down：模型可通过 get_request_detail 工具按需调取
//     单条请求的原始全文，主上下文只放压缩时间线
//   - 流式透传：最终回答的增量经 OnDelta 回调交给调用方（再转发给网页）
//   - dogfooding：请求自动带上 X-PrismCat-Tag/X-PrismCat-Trace-ID，
//     若 base_url 指向 PrismCat 自身，分析流量也会被记录成独立调用链
package analysis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/paopaoandlingyia/PrismCat/internal/config"
)

// Message 对话消息
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`

	// OpenAI 工具调用扩展字段（仅包内使用，序列化时经 messagesToAny 展开为标准格式）
	toolCallID string
	toolCalls  []roundToolCall
}

// ToolGetRequestDetail 调取请求原文的工具名
const ToolGetRequestDetail = "get_request_detail"

// maxToolRounds 工具调用最大轮次（防止模型死循环调工具）
const maxToolRounds = 6

// SystemPrompt 分析助手的系统提示词
const SystemPrompt = `你是 PrismCat（LLM API 透明代理网关）内置的调用链分析助手。
用户会给你一段 AI Agent 执行任务的调用链日志（已做前缀去重压缩），格式说明：
- 每个「[请求 #N]」是 Agent 对 LLM 的一次 API 调用，按时间顺序排列
- 「复用 N 条历史消息」表示该请求携带了与上文重复的历史，已省略展示
- NEW 行：该请求相对上一轮新增的输入消息；RESP 行：LLM 本轮的响应
- 事件标签：[system]/[user]/[assistant] 为消息；「调用工具: 名」为工具调用（后接参数）；「工具结果: 名」为工具返回；「思考」为思维链；行尾 … 表示内容被截断
- 当中段请求被省略时，会提供每条的单行摘要（含日志 ID）

你有一个工具 get_request_detail(log_id, part)：当时间线中的内容被截断、省略，
或你需要核对某个请求的完整原文（如完整工具参数、完整响应、响应头状态）时，调用它。
不要为了"看一眼"就调用；先基于时间线推理，确有需要再调取。

回答要求：
1. 用与用户提问相同的语言回答
2. 具体引用：第几个请求、工具名、关键参数、状态码，不要泛泛而谈
3. 主动指出异常：报错、重试、重复失败的工具调用、疑似死循环、token 异常增长、偏离任务目标
4. 概述 Agent 行为时按阶段归纳（准备→执行→收尾），让用户 10 秒能看懂整个任务
5. 若日志信息不足以回答，先尝试调取原文，仍不足则明确说明缺什么，不要编造`

// BuildMessages 组装完整对话：系统提示词 + 调用链上下文 + 用户多轮对话
func BuildMessages(contextText string, userMessages []Message) []Message {
	msgs := []Message{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: "以下是 Agent 调用链的压缩时间线：\n\n" + contextText + "\n\n请基于以上时间线回答我的问题。"},
	}
	msgs = append(msgs, userMessages...)
	return msgs
}

// ChatCompletionsURL 规范化 base_url 为完整的 chat/completions 端点
func ChatCompletionsURL(baseURL string) string {
	u := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	switch {
	case u == "":
		return ""
	case strings.HasSuffix(u, "/chat/completions"):
		return u
	case strings.HasSuffix(u, "/v1"):
		return u + "/chat/completions"
	default:
		return u + "/v1/chat/completions"
	}
}

// ToolExecutor 由调用方实现的工具执行器（如查询日志原文）
type ToolExecutor interface {
	// ExecuteToolCall 执行工具调用，返回给模型的结果文本；args 为原始 JSON 参数
	ExecuteToolCall(ctx context.Context, name string, args json.RawMessage) (string, error)
}

// Callbacks 流式回调（均为可选）
type Callbacks struct {
	OnDelta      func(delta string) error                 // 最终回答的文本增量
	OnToolCall   func(name, args string) error            // 模型发起工具调用（状态提示）
	OnToolResult func(name string, resultChars int) error // 工具执行完成
	OnReset      func() error                             // 本轮含工具调用，需清空已透传的临时文本
}

// ChatResult 一次完整分析的产出
type ChatResult struct {
	Content   string
	Usage     map[string]any
	Rounds    int // API 调用轮次
	ToolCalls int // 实际执行的工具调用次数
}

// StreamOptions 流式调用选项
type StreamOptions struct {
	// TraceID 原调用链 ID，用于给分析请求打 dogfooding 标记（可为空）
	TraceID string
	// Timeout 覆盖配置中的超时（0 表示用配置值）
	Timeout time.Duration
}

// RunChat 执行一次分析对话，支持多轮工具调用循环。
//
// 流程：messages → 调用模型（带工具）→ 若模型要求调工具：执行、追加结果、
// 循环（上限 maxToolRounds 轮，超出后强制无工具收尾）→ 最终回答流式透传。
// exec 为 nil 时不携带工具。首轮若上游因不支持工具而报 4xx，自动降级为无工具重试。
func RunChat(ctx context.Context, cfg config.AnalysisConfig, messages []Message, opts StreamOptions, exec ToolExecutor, cb Callbacks) (*ChatResult, error) {
	endpoint := ChatCompletionsURL(cfg.BaseURL)
	if endpoint == "" {
		return nil, fmt.Errorf("分析模型 base_url 未配置")
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		seconds := cfg.TimeoutSeconds
		if seconds <= 0 {
			seconds = 300
		}
		timeout = time.Duration(seconds) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result := &ChatResult{}
	rounds := 0
	toolsEnabled := exec != nil
	var accumulatedUsage map[string]any

	for {
		rounds++
		result.Rounds = rounds
		// 达到轮次上限后不再提供工具，强制模型收尾作答
		offerTools := toolsEnabled && rounds <= maxToolRounds

		msgsAny, err := messagesToAny(messages)
		if err != nil {
			return result, err
		}
		payload := map[string]any{
			"model":    cfg.Model,
			"messages": msgsAny,
			"stream":   true,
		}
		if offerTools {
			payload["tools"] = toolSpecs()
		}

		resp, err := postChat(ctx, cfg, endpoint, payload, opts)
		if err != nil {
			// 首轮带工具被拒（模型不支持）→ 降级为无工具重试一次
			if offerTools && rounds == 1 {
				toolsEnabled = false
				rounds--
				continue
			}
			return result, err
		}

		round, err := consumeStream(resp.Body, cb)
		if err != nil {
			return result, err
		}
		if round.usage != nil {
			accumulatedUsage = round.usage
		}

		// 本轮有工具调用：执行并循环
		if len(round.toolCalls) > 0 && offerTools {
			if round.content != "" && cb.OnReset != nil {
				// 清掉本轮已透传的临时文本（通常是"让我看一下…"）
				if err := cb.OnReset(); err != nil {
					return result, err
				}
			}
			messages = append(messages, round.assistantMessage())
			for _, tc := range round.toolCalls {
				if cb.OnToolCall != nil {
					if err := cb.OnToolCall(tc.name, tc.args); err != nil {
						return result, err
					}
				}
				out, toolErr := exec.ExecuteToolCall(ctx, tc.name, json.RawMessage(tc.args))
				result.ToolCalls++
				if toolErr != nil {
					out = "工具执行失败: " + toolErr.Error()
				}
				if cb.OnToolResult != nil {
					if err := cb.OnToolResult(tc.name, len([]rune(out))); err != nil {
						return result, err
					}
				}
				messages = append(messages, Message{
					Role: "tool", Content: out, // tool_call_id 经扩展字段透传
				})
				// 补记 tool_call_id：OpenAI 协议要求 tool 消息携带 id
				messages[len(messages)-1].toolCallID = tc.id
			}
			continue
		}

		// 本轮为最终回答
		result.Content = round.content
		result.Usage = accumulatedUsage
		return result, nil
	}
}

// ---------- 消息序列化 ----------

func messagesToAny(messages []Message) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(messages))
	for i := range messages {
		m := &messages[i]
		entry := map[string]any{"role": m.Role, "content": m.Content}
		if m.toolCallID != "" {
			entry["tool_call_id"] = m.toolCallID
		}
		if len(m.toolCalls) > 0 {
			calls := make([]any, 0, len(m.toolCalls))
			for _, tc := range m.toolCalls {
				calls = append(calls, map[string]any{
					"id":   tc.id,
					"type": "function",
					"function": map[string]any{
						"name":      tc.name,
						"arguments": tc.args,
					},
				})
			}
			entry["tool_calls"] = calls
			// 带 tool_calls 的 assistant 消息 content 允许为空串
			if m.Content == "" {
				entry["content"] = nil
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// ---------- 工具定义 ----------

func toolSpecs() []map[string]any {
	return []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name":        ToolGetRequestDetail,
			"description": "获取调用链中某个请求的原始请求体/响应体全文。当时间线内容被截断、被省略，或需要核对完整工具参数、完整模型响应时调用。",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"log_id": map[string]any{
						"type":        "string",
						"description": "请求的日志 ID，来自时间线中「[请求 #N](日志ID=xxx)」标注",
					},
					"part": map[string]any{
						"type":        "string",
						"enum":        []string{"request", "response", "both"},
						"description": "获取部分：request=请求体，response=响应体，both=两者（默认）",
					},
				},
				"required": []string{"log_id"},
			},
		},
	}}
}

// ---------- HTTP 与流解析 ----------

func postChat(ctx context.Context, cfg config.AnalysisConfig, endpoint string, payload map[string]any, opts StreamOptions) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	// dogfooding 标记：若 base_url 指向 PrismCat 自身，分析流量会归入独立调用链
	req.Header.Set("X-PrismCat-Tag", "prismcat-analysis")
	if opts.TraceID != "" {
		req.Header.Set("X-PrismCat-Trace-ID", "analysis-"+opts.TraceID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		resp.Body.Close()
		return nil, fmt.Errorf("分析模型返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return resp, nil
}

type roundToolCall struct {
	id   string
	name string
	args string
}

// streamRound 一轮流式响应的聚合结果
type streamRound struct {
	content   string
	toolCalls []roundToolCall
	usage     map[string]any
}

// assistantMessage 构造回传给模型的 assistant 工具调用消息
// （写入 Message 的扩展字段，序列化时展开为 OpenAI 格式）
func (r *streamRound) assistantMessage() Message {
	m := Message{Role: "assistant", Content: r.content}
	m.toolCalls = r.toolCalls
	return m
}

// consumeStream 消费一轮响应流：增量透传 OnDelta，同时聚合 tool_calls 与 usage。
// 兼容 SSE 流与"忽略 stream 参数"的完整 JSON 两种上游行为。
func consumeStream(body io.Reader, cb Callbacks) (*streamRound, error) {
	round := &streamRound{}

	// 流式 tool_calls 片段累积（按 index 合并）
	var content strings.Builder
	toolAcc := map[int]*roundToolCall{}

	emitDelta := func(s string) error {
		if s == "" {
			return nil
		}
		content.WriteString(s)
		if cb.OnDelta != nil {
			return cb.OnDelta(s)
		}
		return nil
	}

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var sawSSE bool
	var jsonBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			sawSSE = true
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content   string `json:"content"`
						ToolCalls []struct {
							Index    float64 `json:"index"`
							ID       string  `json:"id"`
							Function struct {
								Name      string `json:"name"`
								Arguments string `json:"arguments"`
							} `json:"function"`
						} `json:"tool_calls"`
					} `json:"delta"`
				} `json:"choices"`
				Usage map[string]any `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue // 心跳等非标准行
			}
			if chunk.Usage != nil {
				round.usage = chunk.Usage
			}
			for _, c := range chunk.Choices {
				if err := emitDelta(c.Delta.Content); err != nil {
					return round, err
				}
				for _, tc := range c.Delta.ToolCalls {
					idx := int(tc.Index)
					acc, ok := toolAcc[idx]
					if !ok {
						acc = &roundToolCall{}
						toolAcc[idx] = acc
					}
					if tc.ID != "" {
						acc.id = tc.ID
					}
					if tc.Function.Name != "" {
						acc.name += tc.Function.Name
					}
					acc.args += tc.Function.Arguments
				}
			}
		} else if !sawSSE {
			jsonBuf.WriteString(line)
		}
	}
	if err := scanner.Err(); err != nil {
		return round, fmt.Errorf("读取分析模型流失败: %w", err)
	}

	if !sawSSE {
		// 上游忽略 stream 参数，返回完整 JSON
		var result struct {
			Choices []struct {
				Message struct {
					Content   string
					ToolCalls []struct {
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(jsonBuf.String()), &result); err != nil {
			return round, fmt.Errorf("解析分析模型响应失败: %w", err)
		}
		if result.Usage != nil {
			round.usage = result.Usage
		}
		for _, c := range result.Choices {
			if err := emitDelta(c.Message.Content); err != nil {
				return round, err
			}
			for _, tc := range c.Message.ToolCalls {
				round.toolCalls = append(round.toolCalls, roundToolCall{
					id: tc.ID, name: tc.Function.Name, args: tc.Function.Arguments,
				})
			}
		}
		return round, nil
	}

	round.content = content.String()
	for i := 0; i < len(toolAcc); i++ {
		if tc, ok := toolAcc[i]; ok {
			round.toolCalls = append(round.toolCalls, *tc)
		}
	}
	return round, nil
}
