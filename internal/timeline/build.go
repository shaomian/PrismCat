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
	contexts := map[string][]string{}
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
			TraceAssociation:  item.TraceAssociation,
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
		base := prevFPs
		if item.TraceAssociation != nil && item.TraceAssociation.Source != "explicit" {
			base = nil
			// Parallel branches inherit their own parent context. Without a
			// unique parent, retain all messages rather than hiding a sibling.
			if len(item.TraceAssociation.Parents) == 1 {
				base = contexts[item.TraceAssociation.Parents[0]]
			}
		}
		carried := 0
		if len(reqMsgs) > 0 {
			limit := len(base)
			if len(reqMsgs) < limit {
				limit = len(reqMsgs)
			}
			for carried < limit && base[carried] == reqMsgs[carried].fp {
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
		contexts[item.LogID] = fps

		tl.Summary.TotalCarried += carried
		tl.Summary.TotalNewEvents += len(entry.NewEvents) + len(entry.ResponseEvents)
		tl.Requests = append(tl.Requests, entry)
	}

	tl.Summary.RequestCount = len(items)
	return tl
}

// TruncateEvents 返回按 maxChars 截断事件内容的深拷贝时间线。
//
// 用于缓存场景：一次完整构建（MaxEventChars 不限制）后按需派生各种
// 截断级别，与 Build(items, Options{MaxEventChars: maxChars}) 的输出
// 完全一致——指纹与 carried 在解析阶段确定，与截断无关。
func TruncateEvents(tl *Timeline, maxChars int) *Timeline {
	if tl == nil {
		return nil
	}
	out := &Timeline{
		TraceID: tl.TraceID,
		Summary: tl.Summary,
	}
	if len(tl.Requests) > 0 {
		out.Requests = make([]RequestEntry, len(tl.Requests))
		for i, req := range tl.Requests {
			entry := req
			entry.NewEvents = truncateEvents(req.NewEvents, maxChars)
			entry.ResponseEvents = truncateEvents(req.ResponseEvents, maxChars)
			out.Requests[i] = entry
		}
	}
	return out
}

func truncateEvents(events []Event, maxChars int) []Event {
	if events == nil {
		return nil
	}
	out := make([]Event, len(events))
	for i, ev := range events {
		out[i] = truncateEvent(ev, maxChars)
	}
	return out
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
	// 字节长度 ≥ rune 数：len(s) ≤ maxChars 时必然无需截断，
	// 跳过 []rune 转换（对不限截断的完整构建，可为每个事件省去
	// 数倍于内容体积的临时分配）
	if len(s) <= maxChars {
		return s, false
	}
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s, false
	}
	return string(runes[:maxChars]) + "…", true
}
