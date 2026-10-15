package timeline

import (
	"fmt"
	"strings"
)

// DiagramOptions Mermaid 图生成选项
type DiagramOptions struct {
	LabelChars int // 箭头标签最大字符数，0 使用默认值 40
}

const defaultLabelChars = 40

// Diagram 从时间线生成 Mermaid sequenceDiagram 源码。
//
// 参与者固定为 User / Agent / LLM / Tools 四个泳道：
//   - 第一条 user 消息画为 User ->> Agent
//   - 工具调用画为 Agent ->> Tools，工具结果画为 Tools -->> Agent
//   - LLM 响应画为 LLM -->> Agent
//
// Agent 循环里的往返（Agent ->> LLM）按请求 #seq 依次展开。
func Diagram(tl *Timeline, opts DiagramOptions) string {
	labelChars := opts.LabelChars
	if labelChars <= 0 {
		labelChars = defaultLabelChars
	}

	var b strings.Builder
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
				fmt.Fprintf(&b, "    T-->>A: %s\n", esc(truncateLabel(eventSummary(ev), labelChars)))
			case "message":
				if ev.Role == "user" {
					fmt.Fprintf(&b, "    U->>A: %s\n", esc(truncateLabel(ev.Content, labelChars)))
				}
			}
		}

		// 请求箭头：Agent ->> LLM
		label := fmt.Sprintf("#%d 请求", req.Seq)
		if req.Carried > 0 {
			label += fmt.Sprintf("（携带 %d 条历史）", req.Carried)
		}
		fmt.Fprintf(&b, "    A->>L: %s\n", esc(label))

		// 响应事件
		var respTexts []string
		for _, ev := range req.ResponseEvents {
			switch ev.Kind {
			case "tool_call":
				fmt.Fprintf(&b, "    L-->>A: 调用工具 %s\n", esc(ev.ToolName))
				fmt.Fprintf(&b, "    A->>T: %s(%s)\n", esc(ev.ToolName), esc(truncateLabel(ev.ToolArgs, labelChars)))
			case "message":
				respTexts = append(respTexts, ev.Content)
			case "thinking":
				respTexts = append(respTexts, "[思考] "+ev.Content)
			}
		}
		if len(respTexts) > 0 {
			fmt.Fprintf(&b, "    L-->>A: %s\n", esc(truncateLabel(strings.Join(respTexts, " "), labelChars)))
		}

		// 错误标注
		if req.Error != "" || req.StatusCode >= 400 {
			fmt.Fprintf(&b, "    Note over L: 请求失败 (%d) %s\n", req.StatusCode, esc(truncateLabel(req.Error, labelChars)))
		}
	}
	return b.String()
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
