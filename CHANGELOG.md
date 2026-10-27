# Release notes / 更新记录

## v2.1.0

### 新增与改进

- **自动调用链关联**：没有自定义 Trace 请求头时，利用协议引用、工具调用及上下文关联一次用户指令的后续请求。支持 HTTP/SSE 的 OpenAI Chat Completions、Responses、Anthropic Messages、Gemini，以及 Responses WebSocket；保留显式 Trace ID 优先级。
- **任务与录制批次**：支持指定入口录制、任务命名与备注、复制 Trace ID、收藏保留，以及手动调整请求归属。详情和关联图展示关联依据与前序关系。
- **对比实验与 AI 复盘**：用卡片组织任务，查看耗时、Token、请求数和错误数；按需选择观测点，由 AI 查证日志并生成报告。提供渗透测试、代码开发、逆向分析等模板，支持自定义观测点、保存模板及查看历史报告。
- **统一分析模型设置**：调用链分析与对比实验共用设置，支持同步模型列表、手动填写模型，以及 OpenAI 兼容和 Anthropic 原生协议。
- **任务管理与清理**：调用链、录制批次、复盘和自定义模板支持归档、恢复、删除及批量管理；删除前预览范围并检查引用与进行中状态。新增手动清理过期日志，保留收藏、复盘引用等保护。
- **简化操作界面**：默认隐藏管理用多选框，按需进入批量管理；统一使用“管理⌄”菜单，点击内容打开详情。调用链搜索与组合筛选集中展示，录制中的批次突出停止操作。
- **用量与报告修复**：改进压缩响应、SSE 和 WebSocket 的用量读取及汇总，缺失值保持未知；完善复盘报告整理、工具调用结束处理与失败提示。
- **本地构建入口**：Windows、macOS 启动脚本统一使用共享构建流程，保留配置和嵌入 UI 占位文件，清理旧资源并检查进程状态。

### 升级说明

- 沿用已有配置和 SQLite 数据；自动调用链关联默认关闭，开启后仅处理新流量，不自动重建历史日志。
- Token 提取仍需启用 Usage 并在对应上游绑定适用规则。开启后不会自动重算历史记录，缺失用量不会作为零参与比较。
- 归档仅隐藏项目，不释放存储或解除日志保留保护。删除和手动清理的范围见 [任务管理与清理](TASK_MANAGEMENT_CN.md)。
- 使用说明：[自动调用链与测试批次](AUTO_TRACE_CN.md)、[对比实验与 AI 任务复盘](EXPERIMENTS_CN.md)。

### English

- Add automatic task correlation without custom trace headers, using protocol references, tool calls and context. Cover OpenAI Chat Completions and Responses, Anthropic Messages, Gemini, and Responses WebSocket while keeping explicit trace IDs authoritative.
- Add capture runs, task metadata, trace ID copying, retention pinning, manual association corrections and predecessor-aware diagrams.
- Add comparison cards and AI reviews with basic usage/latency metrics, selectable observations, reusable/custom templates and saved reports.
- Share analysis model settings across traces and reviews, including model synchronization, manual model IDs, OpenAI-compatible APIs and native Anthropic Messages.
- Add archive/restore/delete and batch management, deletion previews, reference/activity protections and manual expired-log cleanup.
- Simplify task lists with optional selection, labeled Manage menus and consolidated trace filters.
- Improve usage extraction for compressed, streamed and WebSocket responses, and recover report finalization failures without treating unknown usage as zero.
- Unify Windows and macOS local build launchers, preserving configuration and the tracked UI placeholder.

Existing configuration and SQLite data remain usable. Automatic correlation is opt-in and applies to new traffic only. Usage extraction still requires its global and upstream rule settings; existing logs are not automatically recalculated. Archiving preserves retention protections and does not reclaim storage. See [Automatic traces](AUTO_TRACE.md) and [Task management and cleanup](TASK_MANAGEMENT.md).
