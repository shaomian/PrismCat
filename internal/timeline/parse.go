package timeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// message 内部消息表示：完整内容的事件 + 用于跨请求去重的指纹
type message struct {
	fp string
	ev Event
}

const (
	formatUnknown         = "unknown"
	formatOpenAIChat      = "openai-chat"
	formatAnthropic       = "anthropic"
	formatGemini          = "gemini"
	formatOpenAIResponses = "openai-responses"
)

// parseRequest 解析请求体为消息序列，返回 API 格式、模型名、消息列表
func parseRequest(path, body string) (format, model string, msgs []message, parseErr string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return formatUnknown, "", nil, "请求体为空"
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return formatUnknown, "", nil, "请求体不是有效的 JSON"
	}

	format = detectRequestFormat(path, doc)
	model, _ = doc["model"].(string)

	switch format {
	case formatOpenAIChat:
		msgs = parseOpenAIChatMessages(doc)
	case formatAnthropic:
		msgs = parseAnthropicMessages(doc)
	case formatGemini:
		msgs = parseGeminiContents(doc)
	case formatOpenAIResponses:
		msgs = parseResponsesInput(doc)
	default:
		return formatUnknown, model, nil, "无法识别的请求格式"
	}
	return format, model, msgs, ""
}

// parseResponse 解析响应体为消息序列（自动处理 SSE 流式响应）
func parseResponse(format, body string, streaming bool) (msgs []message, parseErr string) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, ""
	}

	// 流式响应先合并为完整文档
	if streaming || looksLikeSSE(trimmed) {
		merged, err := mergeSSE(format, trimmed)
		if err != nil {
			return nil, err.Error()
		}
		if merged == nil {
			return nil, ""
		}
		return responseMessagesFromDoc(format, merged)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, "响应体不是有效的 JSON"
	}
	return responseMessagesFromDoc(format, doc)
}

// responseMessagesFromDoc 从完整（或合并后的）响应文档中提取消息
func responseMessagesFromDoc(format string, doc map[string]any) ([]message, string) {
	switch format {
	case formatOpenAIChat:
		return parseOpenAIChatResponse(doc), ""
	case formatAnthropic:
		return parseAnthropicResponse(doc), ""
	case formatGemini:
		return parseGeminiResponse(doc), ""
	case formatOpenAIResponses:
		return parseResponsesOutput(doc), ""
	}
	// 格式未知时尝试自动嗅探响应形状
	if choices, ok := doc["choices"].([]any); ok && len(choices) > 0 {
		return parseOpenAIChatResponse(doc), ""
	}
	if _, ok := doc["candidates"]; ok {
		return parseGeminiResponse(doc), ""
	}
	if t, _ := doc["type"].(string); t == "message" {
		if _, ok := doc["content"]; ok {
			return parseAnthropicResponse(doc), ""
		}
	}
	if _, ok := doc["output"]; ok {
		return parseResponsesOutput(doc), ""
	}
	return nil, ""
}

// detectRequestFormat 依据请求路径与 body 形状识别 API 格式
func detectRequestFormat(path string, doc map[string]any) string {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "chat/completions"):
		return formatOpenAIChat
	case strings.Contains(p, "/messages"):
		return formatAnthropic
	case strings.Contains(p, "/responses"):
		return formatOpenAIResponses
	case strings.Contains(p, "generatecontent"):
		return formatGemini
	}

	if _, ok := doc["contents"]; ok {
		return formatGemini
	}
	if _, hasMessages := doc["messages"]; hasMessages {
		// Anthropic 与 OpenAI 都有 messages 字段，通过特征区分
		if looksAnthropic(doc) {
			return formatAnthropic
		}
		return formatOpenAIChat
	}
	if _, ok := doc["input"]; ok {
		return formatOpenAIResponses
	}
	return formatUnknown
}

// looksAnthropic 判断带 messages 的请求是否为 Anthropic 格式
func looksAnthropic(doc map[string]any) bool {
	// 顶层 system 字段（字符串或块数组）是 Anthropic 的强特征
	if sys, ok := doc["system"]; ok {
		if _, isStr := sys.(string); isStr {
			return true
		}
		if _, isArr := sys.([]any); isArr {
			return true
		}
	}
	// messages 内容块中出现 tool_use / tool_result 类型是 Anthropic 独有
	for _, m := range asArray(doc["messages"]) {
		mm, _ := m.(map[string]any)
		for _, block := range asArray(mm["content"]) {
			if b, ok := block.(map[string]any); ok {
				switch b["type"] {
				case "tool_use", "tool_result":
					return true
				}
			}
		}
	}
	return false
}

// ---------- OpenAI Chat Completions ----------

func parseOpenAIChatMessages(doc map[string]any) []message {
	var out []message
	for _, m := range asArray(doc["messages"]) {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := asString(mm["role"])
		switch role {
		case "assistant":
			out = append(out, openAIAssistantMessages(mm)...)
		case "tool":
			out = append(out, makeMessage(Event{
				Role: "tool", Kind: "tool_result",
				Content: openAIContentText(mm["content"]), ToolCallID: asString(mm["tool_call_id"]),
			}))
		default: // system / user / developer 等
			out = append(out, openAITextEvents(role, mm["content"])...)
		}
	}
	return out
}

// openAIAssistantMessages 解析 assistant 消息（正文 + 工具调用）
func openAIAssistantMessages(mm map[string]any) []message {
	var out []message
	if text := openAIContentText(mm["content"]); text != "" {
		out = append(out, makeMessage(Event{Role: "assistant", Kind: "message", Content: text}))
	}
	if rc := asString(mm["reasoning_content"]); rc != "" {
		out = append(out, makeMessage(Event{Role: "assistant", Kind: "thinking", Content: rc}))
	}
	for _, tc := range asArray(mm["tool_calls"]) {
		t, ok := tc.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := t["function"].(map[string]any)
		out = append(out, makeMessage(Event{
			Role: "assistant", Kind: "tool_call",
			ToolName: asString(fn["name"]), ToolArgs: asString(fn["arguments"]), ToolCallID: asString(t["id"]),
		}))
	}
	return out
}

// openAITextEvents 将 string 或 content-parts 形式的内容转成事件
func openAITextEvents(role string, content any) []message {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []message{makeMessage(Event{Role: role, Kind: "message", Content: s})}
	}
	var out []message
	var texts []string
	flush := func() {
		if len(texts) > 0 {
			out = append(out, makeMessage(Event{Role: role, Kind: "message", Content: strings.Join(texts, "\n")}))
			texts = nil
		}
	}
	for _, part := range asArray(content) {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		switch asString(p["type"]) {
		case "text":
			texts = append(texts, asString(p["text"]))
		case "image_url", "input_image", "image":
			flush()
			out = append(out, makeMessage(Event{Role: role, Kind: "image", Content: "[图片]"}))
		}
	}
	flush()
	return out
}

// openAIContentText 提取 OpenAI content 字段的全部文本
func openAIContentText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var texts []string
	for _, m := range openAITextEvents("", content) {
		if m.ev.Kind == "message" {
			texts = append(texts, m.ev.Content)
		}
	}
	return strings.Join(texts, "\n")
}

func parseOpenAIChatResponse(doc map[string]any) []message {
	var out []message
	for _, c := range asArray(doc["choices"]) {
		choice, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if msg, ok := choice["message"].(map[string]any); ok {
			out = append(out, openAIAssistantMessages(msg)...)
			continue
		}
		// 兼容 text completion 形状
		if text := asString(choice["text"]); text != "" {
			out = append(out, makeMessage(Event{Role: "assistant", Kind: "message", Content: text}))
		}
	}
	return out
}

// ---------- Anthropic Messages ----------

func parseAnthropicMessages(doc map[string]any) []message {
	var out []message
	switch sys := doc["system"].(type) {
	case string:
		if sys != "" {
			out = append(out, makeMessage(Event{Role: "system", Kind: "message", Content: sys}))
		}
	case []any:
		var texts []string
		for _, block := range sys {
			if b, ok := block.(map[string]any); ok {
				texts = append(texts, asString(b["text"]))
			}
		}
		if joined := strings.Join(texts, "\n"); joined != "" {
			out = append(out, makeMessage(Event{Role: "system", Kind: "message", Content: joined}))
		}
	}
	for _, m := range asArray(doc["messages"]) {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role := asString(mm["role"])
		out = append(out, anthropicBlockEvents(role, mm["content"])...)
	}
	return out
}

// anthropicBlockEvents 解析 Anthropic 内容块
func anthropicBlockEvents(role string, content any) []message {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []message{makeMessage(Event{Role: role, Kind: "message", Content: s})}
	}
	var out []message
	for _, block := range asArray(content) {
		b, ok := block.(map[string]any)
		if !ok {
			continue
		}
		switch asString(b["type"]) {
		case "text":
			out = append(out, makeMessage(Event{Role: role, Kind: "message", Content: asString(b["text"])}))
		case "thinking":
			out = append(out, makeMessage(Event{Role: role, Kind: "thinking", Content: asString(b["thinking"])}))
		case "tool_use":
			out = append(out, makeMessage(Event{
				Role: "assistant", Kind: "tool_call",
				ToolName: asString(b["name"]), ToolArgs: canonicalJSON(b["input"]), ToolCallID: asString(b["id"]),
			}))
		case "tool_result":
			out = append(out, makeMessage(Event{
				Role: "tool", Kind: "tool_result",
				Content: anthropicToolResultText(b["content"]), ToolCallID: asString(b["tool_use_id"]),
			}))
		case "image":
			out = append(out, makeMessage(Event{Role: role, Kind: "image", Content: "[图片]"}))
		}
	}
	return out
}

func anthropicToolResultText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	var texts []string
	for _, block := range asArray(content) {
		if b, ok := block.(map[string]any); ok {
			texts = append(texts, asString(b["text"]))
		}
	}
	return strings.Join(texts, "\n")
}

func parseAnthropicResponse(doc map[string]any) []message {
	role := asString(doc["role"])
	if role == "" {
		role = "assistant"
	}
	return anthropicBlockEvents(role, doc["content"])
}

// ---------- Gemini ----------

func parseGeminiContents(doc map[string]any) []message {
	var out []message
	if sys, ok := doc["systemInstruction"].(map[string]any); ok {
		if text := geminiPartsText(asArray(sys["parts"])); text != "" {
			out = append(out, makeMessage(Event{Role: "system", Kind: "message", Content: text}))
		}
	}
	for _, c := range asArray(doc["contents"]) {
		content, ok := c.(map[string]any)
		if !ok {
			continue
		}
		role := asString(content["role"])
		if role == "model" {
			role = "assistant"
		} else if role == "" {
			role = "user"
		}
		out = append(out, geminiPartEvents(role, asArray(content["parts"]))...)
	}
	return out
}

func geminiPartEvents(role string, parts []any) []message {
	var out []message
	var texts []string
	flush := func() {
		if len(texts) > 0 {
			out = append(out, makeMessage(Event{Role: role, Kind: "message", Content: strings.Join(texts, "\n")}))
			texts = nil
		}
	}
	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		switch {
		case part["text"] != nil:
			texts = append(texts, asString(part["text"]))
		case part["functionCall"] != nil:
			flush()
			if fc, ok := part["functionCall"].(map[string]any); ok {
				out = append(out, makeMessage(Event{
					Role: "assistant", Kind: "tool_call",
					ToolName: asString(fc["name"]), ToolArgs: canonicalJSON(fc["args"]), ToolCallID: asString(fc["id"]),
				}))
			}
		case part["functionResponse"] != nil:
			flush()
			if fr, ok := part["functionResponse"].(map[string]any); ok {
				out = append(out, makeMessage(Event{
					Role: "tool", Kind: "tool_result",
					Content: canonicalJSON(fr["response"]), ToolName: asString(fr["name"]), ToolCallID: asString(fr["id"]),
				}))
			}
		case part["inlineData"] != nil || part["inline_data"] != nil:
			flush()
			out = append(out, makeMessage(Event{Role: role, Kind: "image", Content: "[图片]"}))
		}
	}
	flush()
	return out
}

func geminiPartsText(parts []any) string {
	var texts []string
	for _, p := range parts {
		if part, ok := p.(map[string]any); ok {
			if t := asString(part["text"]); t != "" {
				texts = append(texts, t)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func parseGeminiResponse(doc map[string]any) []message {
	var out []message
	for _, c := range asArray(doc["candidates"]) {
		candidate, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if content, ok := candidate["content"].(map[string]any); ok {
			out = append(out, geminiPartEvents("assistant", asArray(content["parts"]))...)
		}
	}
	return out
}

// ---------- OpenAI Responses API ----------

func parseResponsesInput(doc map[string]any) []message {
	var out []message
	if instructions := asString(doc["instructions"]); instructions != "" {
		out = append(out, makeMessage(Event{Role: "system", Kind: "message", Content: instructions}))
	}
	// input 可以是字符串或条目数组
	if s, ok := doc["input"].(string); ok {
		if s != "" {
			out = append(out, makeMessage(Event{Role: "user", Kind: "message", Content: s}))
		}
		return out
	}
	out = append(out, responsesItemEvents(asArray(doc["input"]))...)
	return out
}

func responsesItemEvents(items []any) []message {
	var out []message
	for _, item := range items {
		it, ok := item.(map[string]any)
		if !ok {
			continue
		}
		typ := asString(it["type"])
		switch typ {
		case "message", "":
			role := asString(it["role"])
			if role == "" {
				role = "user"
			}
			out = append(out, responsesContentEvents(role, it["content"])...)
		case "function_call":
			out = append(out, makeMessage(Event{
				Role: "assistant", Kind: "tool_call",
				ToolName: asString(it["name"]), ToolArgs: asString(it["arguments"]), ToolCallID: asString(it["call_id"]),
			}))
		case "function_call_output":
			out = append(out, makeMessage(Event{
				Role: "tool", Kind: "tool_result",
				Content: asString(it["output"]), ToolCallID: asString(it["call_id"]),
			}))
		case "reasoning":
			var texts []string
			for _, s := range asArray(it["summary"]) {
				if sv, ok := s.(map[string]any); ok {
					texts = append(texts, asString(sv["text"]))
				}
			}
			if joined := strings.Join(texts, "\n"); joined != "" {
				out = append(out, makeMessage(Event{Role: "assistant", Kind: "thinking", Content: joined}))
			}
		}
	}
	return out
}

func responsesContentEvents(role string, content any) []message {
	if s, ok := content.(string); ok {
		if s == "" {
			return nil
		}
		return []message{makeMessage(Event{Role: role, Kind: "message", Content: s})}
	}
	var texts []string
	for _, part := range asArray(content) {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		switch asString(p["type"]) {
		case "input_text", "output_text", "text":
			texts = append(texts, asString(p["text"]))
		case "input_image", "image":
			texts = append(texts, "[图片]")
		}
	}
	if joined := strings.Join(texts, "\n"); joined != "" {
		return []message{makeMessage(Event{Role: role, Kind: "message", Content: joined})}
	}
	return nil
}

func parseResponsesOutput(doc map[string]any) []message {
	return responsesItemEvents(asArray(doc["output"]))
}

// ---------- 工具函数 ----------

func asArray(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// canonicalJSON 将任意值序列化为紧凑 JSON 字符串（键序稳定，用于展示与指纹）
func canonicalJSON(v any) string {
	if v == nil {
		return ""
	}
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(data)
}

// makeMessage 创建消息并计算内容指纹
func makeMessage(ev Event) message {
	h := sha256.New()
	h.Write([]byte(ev.Role))
	h.Write([]byte{0})
	h.Write([]byte(ev.Kind))
	h.Write([]byte{0})
	h.Write([]byte(ev.ToolName))
	h.Write([]byte{0})
	h.Write([]byte(ev.ToolCallID))
	h.Write([]byte{0})
	h.Write([]byte(ev.Content))
	h.Write([]byte{0})
	h.Write([]byte(ev.ToolArgs))
	return message{fp: hex.EncodeToString(h.Sum(nil)), ev: ev}
}
