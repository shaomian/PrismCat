package trace

import (
	"bufio"
	"bytes"
	"strings"
)

func geminiPath(path string) bool {
	return strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent")
}

// Keep signatures, media and thought parts in the digest. A function name alone
// is never a tool identity; older Gemini calls without IDs use context evidence.
func (f *Features) gemini(content map[string]any, role string) {
	if role == "" {
		role = text(content["role"])
		if role == "model" {
			role = "assistant"
		}
		if role == "" {
			role = "user"
		}
	}
	for _, part := range mergeGeminiText(array(content["parts"])) {
		p := object(part)
		switch {
		case p["functionCall"] != nil:
			call := object(p["functionCall"])
			f.add("assistant", "tool_call", text(call["id"]), p)
		case p["functionResponse"] != nil:
			call := object(p["functionResponse"])
			f.add("tool", "tool_result", text(call["id"]), p)
		case p["text"] != nil:
			kind := "message"
			if p["thought"] == true {
				kind = "thinking"
			}
			f.add(role, kind, "", p)
		default:
			f.add(role, "block", "", p)
		}
	}
}

func mergeGeminiText(parts []any) []any {
	var out []any
	for _, value := range parts {
		p := object(value)
		if len(out) > 0 && p["text"] != nil && p["thoughtSignature"] == nil {
			previous := object(out[len(out)-1])
			if previous["text"] != nil && (previous["thought"] == true) == (p["thought"] == true) && previous["thoughtSignature"] == nil && len(p) <= 2 && len(previous) <= 2 {
				previous["text"] = text(previous["text"]) + text(p["text"])
				continue
			}
		}
		out = append(out, value)
	}
	return out
}

func geminiStream(raw []byte) Features {
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), MaxParseBytes)
	var data []string
	var parts []any
	responseID, finish := "", ""
	invalid := false
	consume := func() {
		var doc map[string]any
		if decodeJSON([]byte(strings.Join(data, "\n")), &doc) != nil {
			invalid = true
			data = nil
			return
		}
		data = nil
		candidates := array(doc["candidates"])
		if len(candidates) == 0 && doc["usageMetadata"] != nil {
			return
		}
		if len(candidates) != 1 || finish != "" {
			invalid = true
			return
		}
		candidate := object(candidates[0])
		if eventIndex(candidate["index"]) != 0 {
			invalid = true
		}
		parts = append(parts, array(object(candidate["content"])["parts"])...)
		if len(parts) > maxMessages {
			invalid = true
		}
		finish = text(candidate["finishReason"])
		if id := text(doc["responseId"]); id != "" {
			if responseID != "" && id != responseID {
				invalid = true
			}
			responseID = id
		}
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" && len(data) > 0 {
			consume()
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if len(data) > 0 {
		consume()
	}
	if invalid || scanner.Err() != nil || finish != "STOP" {
		return Features{}
	}
	return responseDocument("gemini", map[string]any{"responseId": responseID, "candidates": []any{map[string]any{
		"finishReason": finish, "content": map[string]any{"role": "model", "parts": mergeGeminiText(parts)},
	}}})
}
