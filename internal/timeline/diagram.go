package timeline

import (
	"fmt"
	"strings"
)

// DiagramOptions Mermaid 图生成选项
type DiagramOptions struct {
	LabelChars int // 箭头标签最大字符数，0 使用默认值 40
}

// DiagramLabel 流程图中一条消息/注释标签的完整内容。
// Mermaid 源码中的标签会被截断以保证图形紧凑，此结构把未截断的
// 原文一并返回，供前端做「悬停预览 / 点击查看全文」等渐进式披露交互。
//
//   - Text: 未截断的完整单行文本
//   - LogID: 标签所属请求的日志 ID
//   - Truncated: 图中展示的文本是否被截断
type DiagramLabel struct {
	Text      string `json:"text"`
	LogID     string `json:"log_id,omitempty"`
	Truncated bool   `json:"truncated"`
}

const defaultLabelChars = 40

// Diagram 从时间线生成 Mermaid sequenceDiagram 源码，
// 并同步返回每条消息标签的完整内容（与源码中消息的出现顺序一致）。
//
// 参与者固定为 User / Agent / LLM / Tools 四个泳道：
//   - 第一条 user 消息画为 User ->> Agent
//   - 工具调用画为 Agent ->> Tools，工具结果画为 Tools -->> Agent
//   - LLM 响应画为 LLM -->> Agent
//
// Agent 循环里的往返（Agent ->> LLM）按请求 #seq 依次展开。
func Diagram(tl *Timeline, opts DiagramOptions) (string, []DiagramLabel) {
	labelChars := opts.LabelChars
	if labelChars <= 0 {
		labelChars = defaultLabelChars
	}

	var b strings.Builder
	labels := make([]DiagramLabel, 0)
	// label 记录标签全文（供前端展示），返回截断后的展示文本（写入源码）
	label := func(full, logID string) string {
		flat := strings.Join(strings.Fields(full), " ")
		displayed := truncateLabel(full, labelChars)
		labels = append(labels, DiagramLabel{Text: flat, LogID: logID, Truncated: displayed != flat})
		return displayed
	}
	b.WriteString("sequenceDiagram\n")
	b.WriteString("    autonumber\n")
	b.WriteString("    actor U as User\n")
	b.WriteString("    participant A as Agent\n")

	// LLM 参与者标签带上第一个识别出的模型名
	model := ""
	for _, req := range tl.Requests {
		if req.Model != "" {
			model = req.Model
			break
		}
	}
	if model != "" {
		fmt.Fprintf(&b, "    participant L as LLM (%s)\n", esc(model))
	} else {
		b.WriteString("    participant L as LLM\n")
	}
	b.WriteString("    participant T as Tools\n")

	for _, req := range tl.Requests {
		// 先画请求体中新增的事件（按时间顺序它们发生在本轮 LLM 请求之前）：
		// tool_result = 工具执行完返回给 Agent；user message = 用户输入/追加
		for _, ev := range req.NewEvents {
			switch ev.Kind {
			case "tool_result":
				fmt.Fprintf(&b, "    T-->>A: %s\n", esc(label(eventSummary(ev), req.LogID)))
			case "message":
				if ev.Role == "user" {
					fmt.Fprintf(&b, "    U->>A: %s\n", esc(label(ev.Content, req.LogID)))
				}
			}
		}

		// 请求箭头：Agent ->> LLM
		reqLabel := fmt.Sprintf("#%d 请求", req.Seq)
		if req.Carried > 0 {
			reqLabel += fmt.Sprintf("（携带 %d 条历史）", req.Carried)
		}
		fmt.Fprintf(&b, "    A->>L: %s\n", esc(label(reqLabel, req.LogID)))

		// 响应事件
		var respTexts []string
		for _, ev := range req.ResponseEvents {
			switch ev.Kind {
			case "tool_call":
				fmt.Fprintf(&b, "    L-->>A: %s\n", esc(label("调用工具 "+ev.ToolName, req.LogID)))
				fmt.Fprintf(&b, "    A->>T: %s\n", esc(label(ev.ToolName+"("+ev.ToolArgs+")", req.LogID)))
			case "message":
				respTexts = append(respTexts, ev.Content)
			case "thinking":
				respTexts = append(respTexts, "[思考] "+ev.Content)
			}
		}
		if len(respTexts) > 0 {
			fmt.Fprintf(&b, "    L-->>A: %s\n", esc(label(strings.Join(respTexts, " "), req.LogID)))
		}

		// 错误标注
		if req.Error != "" || req.StatusCode >= 400 {
			fmt.Fprintf(&b, "    Note over L: %s\n", esc(label(fmt.Sprintf("请求失败 (%d) %s", req.StatusCode, req.Error), req.LogID)))
		}
	}
	return b.String(), labels
}

// eventSummary 事件的单行摘要
func eventSummary(ev Event) string {
	if ev.ToolName != "" {
		if ev.Content != "" {
			return fmt.Sprintf("%s → %s", ev.ToolName, ev.Content)
		}
		return ev.ToolName
	}
	return ev.Content
}

// truncateLabel 截断并压平为单行
func truncateLabel(s string, maxChars int) string {
	s = strings.Join(strings.Fields(s), " ") // 压缩所有空白为单空格
	runes := []rune(s)
	if maxChars > 0 && len(runes) > maxChars {
		return string(runes[:maxChars]) + "…"
	}
	return s
}

// esc Mermaid 标签清洗。
// sequenceDiagram 语法中 ";" 充当语句结束符、"#" 之后到行尾的内容会被当作注释丢弃，
// 且标签必须单行，因此压缩空白并把这两个字符替换为全角标点；其余字符在消息文本区均安全。
func esc(s string) string {
	s = strings.Join(strings.Fields(s), " ") // 单行化
	s = strings.ReplaceAll(s, ";", "；")
	s = strings.ReplaceAll(s, "#", "＃")
	return strings.TrimSpace(s)
}
