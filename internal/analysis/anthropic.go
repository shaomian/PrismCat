package analysis

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// The shared tool loop keeps protocol-neutral messages; only the wire format
// differs. Consecutive tool results belong to one native user message.
func anthropicPayload(model string, messages []Message, tools any) map[string]any {
	var system []string
	converted := []map[string]any{}
	for _, m := range messages {
		if m.Role == "system" {
			system = append(system, m.Content)
			continue
		}
		role := m.Role
		blocks := []map[string]any{}
		if role == "tool" {
			role = "user"
			blocks = append(blocks, map[string]any{"type": "tool_result", "tool_use_id": m.toolCallID, "content": m.Content})
		} else if role == "assistant" && m.nativeContent != nil {
			blocks = append(blocks, m.nativeContent...)
		} else {
			if m.Content != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, call := range m.toolCalls {
				input := map[string]any{}
				if err := json.Unmarshal([]byte(call.args), &input); err != nil {
					input = map[string]any{"invalid_json": call.args}
				}
				if input == nil {
					input = map[string]any{}
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": call.id, "name": call.name, "input": input})
			}
		}
		if len(blocks) == 0 {
			continue
		}
		if len(converted) > 0 && converted[len(converted)-1]["role"] == role {
			last := converted[len(converted)-1]
			last["content"] = append(last["content"].([]map[string]any), blocks...)
		} else {
			converted = append(converted, map[string]any{"role": role, "content": blocks})
		}
	}
	payload := map[string]any{"model": model, "messages": converted, "stream": true, "max_tokens": 8192}
	if len(system) > 0 {
		payload["system"] = strings.Join(system, "\n\n")
	}
	if specs, ok := tools.([]map[string]any); ok && len(specs) > 0 {
		native := []map[string]any{}
		for _, spec := range specs {
			if function, ok := spec["function"].(map[string]any); ok {
				native = append(native, map[string]any{"name": function["name"], "description": function["description"], "input_schema": function["parameters"]})
			}
		}
		payload["tools"] = native
	}
	return payload
}

type anthropicBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
	raw   map[string]any
}

func (b *anthropicBlock) UnmarshalJSON(data []byte) error {
	type fields anthropicBlock
	if err := json.Unmarshal(data, (*fields)(b)); err != nil {
		return err
	}
	return json.Unmarshal(data, &b.raw)
}

type anthropicMessage struct {
	Type       string           `json:"type"`
	Content    []anthropicBlock `json:"content"`
	Usage      map[string]any   `json:"usage"`
	StopReason string           `json:"stop_reason"`
}

func consumeAnthropicStream(body io.Reader, cb Callbacks) (*streamRound, error) {
	round := &streamRound{}
	usage := map[string]any{}
	var content strings.Builder
	blocks := map[int]map[string]any{}
	parts := map[int]*strings.Builder{}
	signatures := map[int]*strings.Builder{}
	tools := map[int]*roundToolCall{}
	// message_delta usage is cumulative within a round, not additional usage.
	defer func() {
		round.usage = normalizeAnthropicUsage(usage)
		round.content = content.String()
		for _, index := range slices.Sorted(maps.Keys(blocks)) {
			block := blocks[index]
			if part := parts[index]; part != nil {
				field := "text"
				if block["type"] == "thinking" {
					field = "thinking"
				}
				block[field] = part.String()
			}
			if signature := signatures[index]; signature != nil {
				block["signature"] = signature.String()
			}
			if call := tools[index]; call != nil {
				input := map[string]any{}
				if err := json.Unmarshal([]byte(call.args), &input); err != nil {
					input = map[string]any{"invalid_json": call.args}
				}
				if input == nil {
					input = map[string]any{}
				}
				block["input"] = input
			}
			round.nativeContent = append(round.nativeContent, block)
		}
	}()
	merge := func(next map[string]any) { maps.Copy(usage, next) }
	emit := func(text string) error {
		content.WriteString(text)
		if text != "" && cb.OnDelta != nil {
			return cb.OnDelta(text)
		}
		return nil
	}
	partial := map[int]bool{}
	var jsonBody strings.Builder
	sawSSE, stopped := false, false
	stopReason := ""
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") {
			sawSSE = true
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			if !sawSSE {
				if jsonBody.Len()+len(line) > 4<<20 {
					return round, errors.New("Anthropic response is too large")
				}
				jsonBody.WriteString(line)
			}
			continue
		}
		sawSSE = true
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var event struct {
			Type         string           `json:"type"`
			Index        int              `json:"index"`
			Message      anthropicMessage `json:"message"`
			ContentBlock anthropicBlock   `json:"content_block"`
			Delta        struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				Thinking    string `json:"thinking"`
				Signature   string `json:"signature"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			Usage map[string]any `json:"usage"`
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return round, fmt.Errorf("解析 Anthropic 响应失败: %w", err)
		}
		switch event.Type {
		case "message_start":
			merge(event.Message.Usage)
		case "content_block_start":
			b := event.ContentBlock
			blocks[event.Index] = b.raw
			if b.Type == "text" || b.Type == "thinking" {
				parts[event.Index] = &strings.Builder{}
				field := "text"
				if b.Type == "thinking" {
					field = "thinking"
				}
				if value, ok := b.raw[field].(string); ok {
					parts[event.Index].WriteString(value)
				}
			}
			if b.Type == "text" {
				if err := emit(b.Text); err != nil {
					return round, err
				}
			} else if b.Type == "tool_use" {
				args := strings.TrimSpace(string(b.Input))
				if args == "{}" || args == "null" {
					args = ""
				}
				tools[event.Index] = &roundToolCall{id: b.ID, name: b.Name, args: args}
			}
		case "content_block_delta":
			if event.Delta.Type == "text_delta" {
				if part := parts[event.Index]; part != nil {
					part.WriteString(event.Delta.Text)
				}
				if err := emit(event.Delta.Text); err != nil {
					return round, err
				}
			} else if event.Delta.Type == "thinking_delta" {
				if part := parts[event.Index]; part != nil {
					part.WriteString(event.Delta.Thinking)
				}
			} else if event.Delta.Type == "signature_delta" {
				if signatures[event.Index] == nil {
					signatures[event.Index] = &strings.Builder{}
					if initial, ok := blocks[event.Index]["signature"].(string); ok {
						signatures[event.Index].WriteString(initial)
					}
				}
				signatures[event.Index].WriteString(event.Delta.Signature)
			} else if event.Delta.Type == "input_json_delta" {
				call := tools[event.Index]
				if call == nil {
					return round, errors.New("invalid Anthropic tool stream")
				}
				if !partial[event.Index] {
					call.args = ""
					partial[event.Index] = true
				}
				if len(call.args)+len(event.Delta.PartialJSON) > 1<<20 {
					return round, errors.New("Anthropic tool arguments are too large")
				}
				call.args += event.Delta.PartialJSON
			}
		case "message_delta":
			merge(event.Usage)
			stopReason = event.Delta.StopReason
		case "message_stop":
			stopped = true
		case "error":
			return round, fmt.Errorf("Anthropic: %s", event.Error.Message)
		}
	}
	if err := scanner.Err(); err != nil {
		return round, fmt.Errorf("读取 Anthropic 响应失败: %w", err)
	}
	if !sawSSE {
		var message anthropicMessage
		if err := json.Unmarshal([]byte(jsonBody.String()), &message); err != nil || message.Type != "message" {
			return round, errors.New("invalid Anthropic response")
		}
		merge(message.Usage)
		stopReason = message.StopReason
		for i, b := range message.Content {
			blocks[i] = b.raw
			if b.Type == "text" {
				if err := emit(b.Text); err != nil {
					return round, err
				}
			} else if b.Type == "tool_use" {
				tools[i] = &roundToolCall{id: b.ID, name: b.Name, args: string(b.Input)}
			}
		}
	} else if !stopped {
		return round, errors.New("Anthropic 响应流提前结束，请重试")
	}
	if stopReason == "max_tokens" || stopReason == "model_context_window_exceeded" {
		return round, ErrOutputTruncated
	}
	for _, index := range slices.Sorted(maps.Keys(tools)) {
		call := tools[index]
		if call.args == "" {
			call.args = "{}"
		}
		round.toolCalls = append(round.toolCalls, *call)
	}
	return round, nil
}

func normalizeAnthropicUsage(raw map[string]any) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	out := maps.Clone(raw)
	in, inputKnown := toFloat(raw["input_tokens"])
	for _, name := range []string{"cache_creation_input_tokens", "cache_read_input_tokens"} {
		if value, ok := toFloat(raw[name]); ok {
			in += value
		}
	}
	output, outputKnown := toFloat(raw["output_tokens"])
	if inputKnown {
		out["prompt_tokens"] = in
	}
	if outputKnown {
		out["completion_tokens"] = output
	}
	if inputKnown && outputKnown {
		out["total_tokens"] = in + output
	}
	return out
}
