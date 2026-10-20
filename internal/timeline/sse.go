package timeline

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// sortedIntKeys 升序返回 map 的 index 键。部分上游会以非 0 起始/非连续的
// choice 或 tool_call index 发送增量，按 len 循环遍历会静默丢失事件。
func sortedIntKeys[V any](m map[int]V) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// sseEvent 一条 SSE 消息
type sseEvent struct {
	event string
	data  map[string]any
}

// looksLikeSSE 判断文本是否为 SSE 事件流
func looksLikeSSE(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		return strings.HasPrefix(line, "data:") || strings.HasPrefix(line, "event:")
	}
	return false
}

// parseSSEEvents 将 SSE 文本解析为事件列表（忽略非 JSON 的 data 与 [DONE]）
func parseSSEEvents(raw string) []sseEvent {
	normalized := strings.ReplaceAll(raw, "\r\n", "\n")
	blocks := strings.Split(normalized, "\n\n")
	var events []sseEvent
	for _, block := range blocks {
		eventName := "message"
		var dataLines []string
		for _, line := range strings.Split(block, "\n") {
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				if v := strings.TrimSpace(strings.TrimPrefix(line, "event:")); v != "" {
					eventName = v
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimLeft(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		payload := strings.TrimSpace(strings.Join(dataLines, "\n"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(payload), &data); err != nil {
			continue
		}
		events = append(events, sseEvent{event: eventName, data: data})
	}
	return events
}

// mergeSSE 按 API 格式将 SSE 事件流合并为完整响应文档
func mergeSSE(format, raw string) (map[string]any, error) {
	events := parseSSEEvents(raw)
	if len(events) == 0 {
		return nil, errors.New("SSE 事件流为空或无法解析")
	}

	// 格式未知时嗅探事件形状
	if format == formatUnknown {
		format = sniffSSEFormat(events)
	}

	switch format {
	case formatOpenAIChat:
		return mergeOpenAIChatSSE(events), nil
	case formatAnthropic:
		return mergeAnthropicSSE(events), nil
	case formatGemini:
		return mergeGeminiSSE(events), nil
	case formatOpenAIResponses:
		if doc := mergeResponsesSSE(events); doc != nil {
			return doc, nil
		}
		return nil, errors.New("Responses 流中未找到 response.completed 事件")
	}
	return nil, errors.New("无法识别的 SSE 响应格式")
}

func sniffSSEFormat(events []sseEvent) string {
	for _, e := range events {
		switch e.event {
		case "message_start", "message_delta", "content_block_start", "content_block_delta", "content_block_stop":
			return formatAnthropic
		}
		if strings.HasPrefix(e.event, "response.") {
			return formatOpenAIResponses
		}
		if t := asString(e.data["type"]); strings.HasPrefix(t, "response.") {
			return formatOpenAIResponses
		}
		switch asString(e.data["type"]) {
		case "message_start", "message_delta", "content_block_start", "content_block_delta", "content_block_stop":
			return formatAnthropic
		}
		if asString(e.data["object"]) == "chat.completion.chunk" {
			return formatOpenAIChat
		}
		if _, ok := e.data["candidates"]; ok {
			return formatGemini
		}
	}
	return formatUnknown
}

// mergeOpenAIChatSSE 合并 OpenAI chat.completion.chunk 流
func mergeOpenAIChatSSE(events []sseEvent) map[string]any {
	type toolCallAcc struct {
		id   string
		name string
		args strings.Builder
	}
	type choiceAcc struct {
		role             string
		content          strings.Builder
		reasoningContent strings.Builder
		toolCalls        map[int]*toolCallAcc
	}
	choices := map[int]*choiceAcc{}

	for _, e := range events {
		for _, c := range asArray(e.data["choices"]) {
			choice, ok := c.(map[string]any)
			if !ok {
				continue
			}
			idx := 0
			if f, ok := choice["index"].(float64); ok {
				idx = int(f)
			}
			acc, ok := choices[idx]
			if !ok {
				acc = &choiceAcc{toolCalls: map[int]*toolCallAcc{}}
				choices[idx] = acc
			}
			delta, _ := choice["delta"].(map[string]any)
			if delta == nil {
				continue
			}
			if role := asString(delta["role"]); role != "" {
				acc.role = role
			}
			acc.content.WriteString(openAIContentText(delta["content"]))
			if rc := asString(delta["reasoning_content"]); rc != "" {
				acc.reasoningContent.WriteString(rc)
			}
			for _, tc := range asArray(delta["tool_calls"]) {
				t, ok := tc.(map[string]any)
				if !ok {
					continue
				}
				tIdx := 0
				if f, ok := t["index"].(float64); ok {
					tIdx = int(f)
				}
				tool, ok := acc.toolCalls[tIdx]
				if !ok {
					tool = &toolCallAcc{}
					acc.toolCalls[tIdx] = tool
				}
				if id := asString(t["id"]); id != "" {
					tool.id = id
				}
				if fn, ok := t["function"].(map[string]any); ok {
					tool.name += asString(fn["name"])
					tool.args.WriteString(asString(fn["arguments"]))
				}
			}
		}
	}

	var choiceList []any
	for _, cIdx := range sortedIntKeys(choices) {
		acc := choices[cIdx]
		msg := map[string]any{"role": "assistant"}
		if acc.role != "" {
			msg["role"] = acc.role
		}
		if acc.content.Len() > 0 {
			msg["content"] = acc.content.String()
		}
		if acc.reasoningContent.Len() > 0 {
			msg["reasoning_content"] = acc.reasoningContent.String()
		}
		var toolCalls []any
		for _, tIdx := range sortedIntKeys(acc.toolCalls) {
			tool := acc.toolCalls[tIdx]
			toolCalls = append(toolCalls, map[string]any{
				"id":   tool.id,
				"type": "function",
				"function": map[string]any{
					"name":      tool.name,
					"arguments": tool.args.String(),
				},
			})
		}
		if len(toolCalls) > 0 {
			msg["tool_calls"] = toolCalls
		}
		choiceList = append(choiceList, map[string]any{"index": cIdx, "message": msg})
	}
	return map[string]any{"choices": choiceList}
}

// mergeAnthropicSSE 合并 Anthropic Messages 事件流
func mergeAnthropicSSE(events []sseEvent) map[string]any {
	blocks := map[int]map[string]any{}
	maxIdx := -1

	ensure := func(idx int) map[string]any {
		b, ok := blocks[idx]
		if !ok {
			b = map[string]any{}
			blocks[idx] = b
		}
		if idx > maxIdx {
			maxIdx = idx
		}
		return b
	}

	for _, e := range events {
		typ := asString(e.data["type"])
		if typ == "" {
			typ = e.event
		}
		switch typ {
		case "content_block_start":
			idx := dataIndex(e.data, 0)
			if cb, ok := e.data["content_block"].(map[string]any); ok {
				b := ensure(idx)
				for k, v := range cb {
					b[k] = v
				}
			}
		case "content_block_delta":
			idx := dataIndex(e.data, 0)
			b := ensure(idx)
			delta, _ := e.data["delta"].(map[string]any)
			if delta == nil {
				continue
			}
			switch asString(delta["type"]) {
			case "text_delta":
				if b["type"] == nil {
					b["type"] = "text"
				}
				b["text"] = asString(b["text"]) + asString(delta["text"])
			case "thinking_delta":
				if b["type"] == nil {
					b["type"] = "thinking"
				}
				thinking := asString(delta["thinking"])
				if thinking == "" {
					thinking = asString(delta["text"])
				}
				b["thinking"] = asString(b["thinking"]) + thinking
			case "input_json_delta":
				if b["type"] == nil {
					b["type"] = "tool_use"
				}
				b["_partial_json"] = asString(b["_partial_json"]) + asString(delta["partial_json"])
			}
		}
	}

	var content []any
	for i := 0; i <= maxIdx; i++ {
		b, ok := blocks[i]
		if !ok {
			continue
		}
		// 将流式累积的 partial JSON 还原为 input 对象
		if partial := asString(b["_partial_json"]); partial != "" {
			delete(b, "_partial_json")
			var input map[string]any
			if err := json.Unmarshal([]byte(partial), &input); err == nil {
				b["input"] = input
			} else {
				b["input"] = partial
			}
		}
		content = append(content, b)
	}
	return map[string]any{"role": "assistant", "content": content}
}

// mergeGeminiSSE 合并 Gemini streamGenerateContent 流
func mergeGeminiSSE(events []sseEvent) map[string]any {
	type candAcc struct {
		parts []any
	}
	cands := map[int]*candAcc{}

	for _, e := range events {
		for _, c := range asArray(e.data["candidates"]) {
			candidate, ok := c.(map[string]any)
			if !ok {
				continue
			}
			idx := 0
			if f, ok := candidate["index"].(float64); ok {
				idx = int(f)
			}
			acc, ok := cands[idx]
			if !ok {
				acc = &candAcc{}
				cands[idx] = acc
			}
			content, _ := candidate["content"].(map[string]any)
			if content == nil {
				continue
			}
			for _, p := range asArray(content["parts"]) {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				// 连续 text part 合并；其他类型（functionCall 等）直接追加
				if text := asString(part["text"]); text != "" {
					if n := len(acc.parts); n > 0 {
						if last, ok := acc.parts[n-1].(map[string]any); ok {
							if _, lastIsText := last["text"]; lastIsText && len(last) == 1 {
								last["text"] = asString(last["text"]) + text
								continue
							}
						}
					}
					acc.parts = append(acc.parts, map[string]any{"text": text})
					continue
				}
				acc.parts = append(acc.parts, part)
			}
		}
	}

	var candidates []any
	for i := 0; i < len(cands); i++ {
		acc, ok := cands[i]
		if !ok {
			continue
		}
		candidates = append(candidates, map[string]any{
			"index":   i,
			"content": map[string]any{"role": "model", "parts": acc.parts},
		})
	}
	return map[string]any{"candidates": candidates}
}

// mergeResponsesSSE 从 Responses 事件流中取 response.completed 的完整响应
func mergeResponsesSSE(events []sseEvent) map[string]any {
	for i := len(events) - 1; i >= 0; i-- {
		if asString(events[i].data["type"]) == "response.completed" {
			if resp, ok := events[i].data["response"].(map[string]any); ok {
				return resp
			}
		}
	}
	return nil
}

func dataIndex(data map[string]any, fallback int) int {
	if f, ok := data["index"].(float64); ok {
		return int(f)
	}
	return fallback
}
