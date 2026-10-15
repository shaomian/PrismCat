package timeline

// Build 将调用链的有序请求列表构建为对话时间线。
//
// 去重规则：Agent 循环中第 N+1 个请求的消息列表通常是第 N 个请求
// （消息 + 响应）的超集。对相邻请求按消息指纹做最长公共前缀（LCP）
// 匹配，匹配上的部分记为 carried（折叠），其余作为新增事件展示。
// 当客户端改写了历史（如上下文压缩）时，LCP 在首个分叉点停止，
// 之后的消息全部视为新增，保证不漏看。
func Build(traceID string, items []Input, opts Options) *Timeline {
	tl := &Timeline{TraceID: traceID, Requests: make([]RequestEntry, 0, len(items))}
	maxChars := opts.maxEventChars()

	var prevFPs []string
	toolNames := map[string]string{} // tool_call_id -> 工具名（跨请求关联工具结果与调用）
	enrich := func(events []Event) {
		for i := range events {
			ev := &events[i]
			if ev.Kind == "tool_call" && ev.ToolCallID != "" && ev.ToolName != "" {
				toolNames[ev.ToolCallID] = ev.ToolName
			}
			if ev.Kind == "tool_result" && ev.ToolName == "" && ev.ToolCallID != "" {
				if name, ok := toolNames[ev.ToolCallID]; ok {
					ev.ToolName = name
				}
			}
		}
	}
	for _, item := range items {
		entry := RequestEntry{
			LogID:             item.LogID,
			Seq:               item.Seq,
			CreatedAt:         item.CreatedAtMs,
			LatencyMs:         item.LatencyMs,
			StatusCode:        item.StatusCode,
			Error:             item.Error,
			Upstream:          item.Upstream,
			Path:              item.Path,
			UsageInputTokens:  item.UsageInputTokens,
			UsageOutputTokens: item.UsageOutputTokens,
		}

		format, model, reqMsgs, parseErr := parseRequest(item.Path, item.RequestBody)
		entry.Format = format
		entry.Model = model
		if parseErr != "" {
			entry.ParseError = parseErr
		} else {
			tl.Summary.ParsedCount++
		}

		// 跨请求最长公共前缀
		carried := 0
		if len(reqMsgs) > 0 {
			limit := len(prevFPs)
			if len(reqMsgs) < limit {
				limit = len(reqMsgs)
			}
			for carried < limit && prevFPs[carried] == reqMsgs[carried].fp {
				carried++
			}
		}
		entry.Carried = carried

		for _, m := range reqMsgs[carried:] {
			entry.NewEvents = append(entry.NewEvents, truncateEvent(m.ev, maxChars))
		}

		respMsgs, respErr := parseResponse(format, item.ResponseBody, item.Streaming)
		if respErr != "" && entry.ParseError == "" {
			entry.ParseError = respErr
		}
		for _, m := range respMsgs {
			entry.ResponseEvents = append(entry.ResponseEvents, truncateEvent(m.ev, maxChars))
		}

		// 按顺序做工具名富化（new_events 在前、response_events 在后）
		enrich(entry.NewEvents)
		enrich(entry.ResponseEvents)

		// 更新累积上下文：当前请求消息 + 当前响应消息
		fps := make([]string, 0, len(reqMsgs)+len(respMsgs))
		for _, m := range reqMsgs {
			fps = append(fps, m.fp)
		}
		for _, m := range respMsgs {
			fps = append(fps, m.fp)
		}
		prevFPs = fps

		tl.Summary.TotalCarried += carried
		tl.Summary.TotalNewEvents += len(entry.NewEvents) + len(entry.ResponseEvents)
		tl.Requests = append(tl.Requests, entry)
	}

	tl.Summary.RequestCount = len(items)
	return tl
}

// truncateEvent 按字符数截断事件内容（按 rune 截断，保证中文不断码）
func truncateEvent(ev Event, maxChars int) Event {
	ev.Content, ev.Truncated = truncateRunes(ev.Content, maxChars)
	if args, truncated := truncateRunes(ev.ToolArgs, maxChars); truncated {
		ev.ToolArgs = args
		ev.Truncated = true
	}
	return ev
}

func truncateRunes(s string, maxChars int) (string, bool) {
	if maxChars <= 0 {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, false
	}
	return string(runes[:maxChars]) + "…", true
}
