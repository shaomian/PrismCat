# Release notes / 更新记录

## v2.3.0

### 新增与改进

- **调用链加载优化**：对话、流程图及日志详情按需加载；同一调用链的并发时间线构建复用缓存，减少重复正文读取与解析。
- **长任务分批展示**：对话按请求和事件分批展开，流程图每页展示 25 个请求，保留原始序号与跨页父请求提示；完整导出保持可用。
- **内容摘要预览**：录制批次展示调用链、异常、用量缺失及录制范围；复盘展示参与任务、Agent／模型、所选观测点和背景摘要。支持延迟加载、取消、缓存和键盘操作，不自动调用 AI。
- **统计与存储性能**：合并统计查询；调用链详情直接读取元数据，打开单条日志时再读取正文。修复趋势图窗口末尾请求少算。
- **交互可靠性**：补充视图加载失败重试、流程图渲染任务复用，以及空预览隐藏和键盘切换处理。

### 升级说明

- 兼容已有配置与 SQLite 数据，无需手动迁移。
- 流程图界面按页显示；不带分页参数的图形 API 和完整日志导出保留原有行为。
- 更新后需重新构建并重启程序。详情见 [性能检查记录](PERFORMANCE_REVIEW_CN.md)。

### English

- Load trace views, diagrams and log details on demand; share concurrent timeline builds and avoid reading bodies for metadata-only detail lists.
- Render long conversations in batches and diagrams in pages of 25 requests, preserving sequence numbers, external-parent references and complete exports.
- Add useful capture/review previews with delayed loading, cancellation, caching and keyboard support, without automatic AI calls.
- Consolidate statistics queries and fix lost counts at the final trend bucket; improve loading retries and diagram rendering reuse.

Existing configuration and SQLite data remain compatible. Rebuild and restart to apply the update.

## v2.2.0

### 新增与改进

- **调用链人工整理**：支持整条调用链合并、多选请求移入或拆分为新 Trace；显式指定 Trace ID 的任务支持拆分。合并后旧链接可定位到目标任务，已有复盘保留原始请求范围与证据。
- **任务管理与标签**：新增置顶，保留收藏和归档；手动拆分任务显示来源标记。标签支持输入、逐项移除、列表展示、搜索及点击筛选。
- **调用链列表与预览**：长 ID 以首尾片段显示，可查看和复制完整值；错误按需显示数量标记，最近请求包含日期。悬停预览补充名称、备注、上游与首次／最近请求时间。
- **复盘工作页与 Markdown 导出**：按任务、观测点和结果分区组织；AI 建议观测点入口更清晰，报告支持导出 Markdown。
- **统一交互与视觉**：任务操作按悬停或键盘聚焦出现，简化重复操作；统一标签、按钮、输入框、页签和卡片样式。
- **日志收藏与使用体验**：日志“保存”改为“收藏”，移除待处理／已处理入口并保留旧数据；返回列表恢复筛选、页码和滚动位置，区分空列表与筛选无结果，改进 AI 连接、超时和失败重试提示。

### 升级说明

- 兼容现有配置和 SQLite 数据，沿用原始请求、显式 Trace ID 和日志正文；人工整理改变有效归组。
- 日志旧处理状态不再显示，已有收藏及保留保护继续有效；取消收藏时同步清除该日志的旧处理状态。
- 操作说明：[任务管理与清理](TASK_MANAGEMENT_CN.md)、[自动调用链](AUTO_TRACE_CN.md)、[对比实验与 AI 复盘](EXPERIMENTS_CN.md)。

### English

- Add whole-trace merging, multi-request moves and splitting, including splits of explicitly identified traces. Preserve historical review evidence and redirect merged trace links.
- Add task pinning, split provenance, editable label chips, label search and exact label filters.
- Refine trace lists with abbreviated IDs, full-ID viewing/copying, contextual error counts, dated timestamps and previews with notes, upstreams and request time ranges.
- Organize review pages into task, observation and report sections, and export reports as Markdown.
- Unify controls and contextual task actions; simplify log favorites while retaining existing annotations and retention protections.
- Restore list navigation state, distinguish filtered empty results, and improve AI timeout, connection and retry feedback.

Existing configuration and SQLite data remain compatible. Original request bodies and explicit trace identifiers are preserved when regrouping tasks. See [Task management](TASK_MANAGEMENT.md) and [Automatic traces](AUTO_TRACE.md).

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
