package timeline

// FindToolCall visits parsed tool calls until accept returns true. The same
// parser reconstructs arguments split across SSE events in the timeline.
func FindToolCall(path, body string, response, streaming bool, accept func(Event) bool) {
	var messages []message
	if response {
		format := detectRequestFormat(path, nil)
		if format == formatOpenAIResponses && (streaming || looksLikeSSE(body)) {
			events := parseSSEEvents(body)
			messages = parseResponsesOutput(mergeResponsesSSE(events))
			for _, event := range events {
				if asString(event.data["type"]) == "response.output_item.done" {
					messages = append(messages, responsesItemEvents([]any{event.data["item"]})...)
				}
			}
		} else {
			messages, _ = parseResponse(format, body, streaming)
		}
	} else {
		_, _, messages, _ = parseRequest(path, body)
	}
	for _, msg := range messages {
		if msg.ev.Kind == "tool_call" && accept(msg.ev) {
			return
		}
	}
}
