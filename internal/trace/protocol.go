package trace

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

// Features contain only structural digests and protocol IDs, never prompt text.
type Features struct {
	Format        string    `json:"format"`
	Events        []Message `json:"events,omitempty"`
	PreviousID    string    `json:"previous_id,omitempty"`
	Conversation  string    `json:"conversation,omitempty"`
	ResponseID    string    `json:"response_id,omitempty"`
	Client        string    `json:"client,omitempty"`
	NativeSession string    `json:"native_session,omitempty"`
	NativeTurn    string    `json:"native_turn,omitempty"`
	Valid         bool      `json:"valid"`
}

type Message struct {
	Role   string `json:"role"`
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
	CallID string `json:"call_id,omitempty"`
	Size   int    `json:"size"`
}

const MaxParseBytes = 8 << 20
const maxMessages = 4096

func SupportedPath(path string) bool {
	return strings.HasSuffix(path, "/chat/completions") || strings.HasSuffix(path, "/responses") || strings.HasSuffix(path, "/messages") || geminiPath(path)
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func text(v any) string           { s, _ := v.(string); return s }
func object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func array(v any) []any           { a, _ := v.([]any); return a }

func decodeJSON(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}
func eventIndex(v any) int {
	if n, ok := v.(json.Number); ok {
		i, _ := strconv.Atoi(n.String())
		return i
	}
	return 0
}

func (f *Features) add(role, kind, id string, value any) {
	b, _ := json.Marshal(value)
	f.Events = append(f.Events, Message{Role: role, Kind: kind, CallID: id, Size: len(b), Digest: digest([]any{role, kind, id, value})})
}

func canonicalArguments(v any) any {
	if s, ok := v.(string); ok {
		var decoded any
		if decodeJSON([]byte(s), &decoded) == nil {
			return decoded
		}
	}
	return v
}

func (f *Features) chat(m map[string]any) {
	role := text(m["role"])
	if role == "tool" {
		f.add(role, "tool_result", text(m["tool_call_id"]), m["content"])
		return
	}
	if content := m["content"]; content != nil && content != "" {
		// Normalize a single text part to the equivalent string, preserving all media.
		if parts := array(content); len(parts) == 1 && text(object(parts[0])["type"]) == "text" {
			content = object(parts[0])["text"]
		}
		f.add(role, "message", "", content)
	}
	for _, c := range array(m["tool_calls"]) {
		call := object(c)
		fn := object(call["function"])
		f.add("assistant", "tool_call", text(call["id"]), []any{fn["name"], canonicalArguments(fn["arguments"])})
	}
}

func (f *Features) anthropic(m map[string]any) {
	role := text(m["role"])
	if s, ok := m["content"].(string); ok {
		f.add(role, "message", "", s)
		return
	}
	for _, p := range array(m["content"]) {
		b := object(p)
		switch text(b["type"]) {
		case "text":
			f.add(role, "message", "", b["text"])
		case "tool_use":
			f.add("assistant", "tool_call", text(b["id"]), []any{b["name"], b["input"]})
		case "tool_result":
			f.add("tool", "tool_result", text(b["tool_use_id"]), []any{b["content"], b["is_error"]})
		default:
			f.add(role, "block", "", b)
		}
	}
}

func (f *Features) responses(items any) {
	if s, ok := items.(string); ok {
		f.add("user", "message", "", s)
		return
	}
	for _, item := range array(items) {
		m := object(item)
		switch text(m["type"]) {
		case "function_call":
			f.add("assistant", "tool_call", text(m["call_id"]), []any{m["name"], canonicalArguments(m["arguments"])})
		case "function_call_output":
			f.add("tool", "tool_result", text(m["call_id"]), m["output"])
		case "message", "":
			role := text(m["role"])
			if role == "" {
				role = "user"
			}
			if s, ok := m["content"].(string); ok {
				f.add(role, "message", "", s)
				continue
			}
			for _, part := range array(m["content"]) {
				p := object(part)
				switch text(p["type"]) {
				case "input_text", "output_text", "text":
					f.add(role, "message", "", p["text"])
				default:
					f.add(role, "block", "", p)
				}
			}
		default:
			// Keep non-message items opaque. They must not create a false text match.
			f.add("assistant", "opaque", "", m)
		}
	}
}

// ParseRequest is bounded and only accepts supported model endpoints.
func ParseRequest(path string, raw []byte) Features {
	f := Features{}
	if len(raw) == 0 || len(raw) > MaxParseBytes {
		return f
	}
	var doc map[string]any
	if decodeJSON(raw, &doc) != nil {
		return f
	}
	switch {
	case geminiPath(path):
		f.Format = "gemini"
		if system := doc["systemInstruction"]; system != nil {
			f.gemini(object(system), "system")
		}
		for _, content := range array(doc["contents"]) {
			f.gemini(object(content), "")
		}
	case strings.HasSuffix(path, "/chat/completions"):
		f.Format = "openai-chat"
		for _, m := range array(doc["messages"]) {
			f.chat(object(m))
		}
	case strings.HasSuffix(path, "/messages"):
		f.Format = "anthropic"
		if doc["system"] != nil {
			f.add("system", "message", "", doc["system"])
		}
		for _, m := range array(doc["messages"]) {
			f.anthropic(object(m))
		}
	case strings.HasSuffix(path, "/responses"):
		f.Format = "openai-responses"
		f.PreviousID = text(doc["previous_response_id"])
		f.Conversation = text(doc["conversation"])
		if f.Conversation == "" {
			f.Conversation = text(object(doc["conversation"])["id"])
		}
		if doc["instructions"] != nil {
			f.add("system", "message", "", doc["instructions"])
		}
		f.responses(doc["input"])
	default:
		return f
	}
	f.Valid = len(f.Events) <= maxMessages && (len(f.Events) > 0 || f.PreviousID != "")
	if !f.Valid {
		f.Events = nil
	}
	return f
}

func responseDocument(format string, doc map[string]any) Features {
	f := Features{Format: format, Valid: true, ResponseID: text(doc["id"])}
	switch format {
	case "openai-chat":
		choices := array(doc["choices"])
		if len(choices) != 1 {
			f.Valid = false
			break
		}
		f.chat(object(object(choices[0])["message"]))
	case "anthropic":
		f.anthropic(doc)
	case "openai-responses":
		f.responses(doc["output"])
	case "gemini":
		f.ResponseID = text(doc["responseId"])
		candidates := array(doc["candidates"])
		if len(candidates) != 1 {
			return Features{}
		}
		candidate := object(candidates[0])
		if finish := text(candidate["finishReason"]); finish != "STOP" {
			return Features{}
		}
		f.gemini(object(candidate["content"]), "assistant")
	default:
		f.Valid = false
	}
	if len(f.Events) > maxMessages {
		return Features{}
	}
	return f
}

// ParseResponse assembles captured SSE without changing the forwarded stream.
// Only complete protocol documents become content evidence; partial output is not hashed.
func ParseResponse(format string, raw []byte, streaming bool) Features {
	if len(raw) == 0 || len(raw) > MaxParseBytes {
		return Features{}
	}
	if format == "gemini" && (streaming || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:"))) {
		return geminiStream(raw)
	}
	if !streaming && !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("event:")) && !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:")) {
		var doc map[string]any
		if decodeJSON(raw, &doc) != nil {
			return Features{}
		}
		return responseDocument(format, doc)
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), MaxParseBytes)
	var data []string
	var result Features
	var anthropic map[string]any
	blocks := map[int]map[string]any{}
	arguments := map[int]string{}
	var chat map[string]any
	calls := map[int]map[string]any{}
	chatText := ""
	complete := false
	consume := func() {
		payload := strings.Join(data, "\n")
		data = nil
		if payload == "[DONE]" {
			complete = true
			return
		}
		var doc map[string]any
		if decodeJSON([]byte(payload), &doc) != nil {
			return
		}
		switch format {
		case "openai-responses":
			if text(doc["type"]) == "response.completed" {
				result = responseDocument(format, object(doc["response"]))
				complete = true
			}
		case "anthropic":
			i := eventIndex(doc["index"])
			switch text(doc["type"]) {
			case "message_start":
				anthropic = object(doc["message"])
			case "content_block_start":
				blocks[i] = object(doc["content_block"])
			case "content_block_delta":
				b := blocks[i]
				if b == nil {
					return
				}
				d := object(doc["delta"])
				if text(d["type"]) == "text_delta" {
					b["text"] = text(b["text"]) + text(d["text"])
				}
				if text(d["type"]) == "input_json_delta" {
					arguments[i] += text(d["partial_json"])
				}
			case "message_stop":
				complete = true
			}
		case "openai-chat":
			if chat == nil {
				chat = map[string]any{"role": "assistant"}
			}
			choices := array(doc["choices"])
			if len(choices) > 1 {
				return
			}
			for _, c := range choices {
				choice := object(c)
				if eventIndex(choice["index"]) != 0 {
					continue
				}
				d := object(choice["delta"])
				chatText += text(d["content"])
				for _, item := range array(d["tool_calls"]) {
					v := object(item)
					k := eventIndex(v["index"])
					if calls[k] == nil {
						calls[k] = map[string]any{"id": "", "function": map[string]any{"name": "", "arguments": ""}}
					}
					call := calls[k]
					fn := object(call["function"])
					delta := object(v["function"])
					if id := text(v["id"]); id != "" {
						call["id"] = id
					}
					fn["name"] = text(fn["name"]) + text(delta["name"])
					fn["arguments"] = text(fn["arguments"]) + text(delta["arguments"])
				}
			}
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if len(data) > 0 {
				consume()
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(data) > 0 {
		consume()
	}
	if scanner.Err() != nil || !complete {
		return Features{}
	}
	if format == "anthropic" && anthropic != nil {
		var content []any
		for i := 0; i < len(blocks); i++ {
			b := blocks[i]
			if b == nil {
				return Features{}
			}
			if s := arguments[i]; s != "" {
				var v any
				if decodeJSON([]byte(s), &v) != nil {
					return Features{}
				}
				b["input"] = v
			}
			content = append(content, b)
		}
		anthropic["content"] = content
		return responseDocument(format, anthropic)
	}
	if format == "openai-chat" && chat != nil {
		var toolCalls []any
		for i := 0; i < len(calls); i++ {
			if calls[i] == nil {
				return Features{}
			}
			toolCalls = append(toolCalls, calls[i])
		}
		chat["content"] = chatText
		chat["tool_calls"] = toolCalls
		return responseDocument(format, map[string]any{"choices": []any{map[string]any{"message": chat}}})
	}
	return result
}
