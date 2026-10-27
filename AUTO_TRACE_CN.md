# 自动调用链与测试批次

PrismCat 可在没有 `X-PrismCat-Trace-ID` 请求头时，推断一次用户指令中的模型调用归属。已有显式 Trace 头仍然优先。

## 启用

在“设置 → 日志设置 → 基础设置 → 调用链关联”打开“自动关联请求”。调用链页面仅展示状态及“关联设置”入口，也可在配置中设置：

```yaml
trace_correlation:
  enabled: true
```

默认关闭。仅启用后的新流量参与关联，不自动重建历史日志。关联结果只用于本地日志与分析，不向模型服务注入标识或改写流量。

## 支持范围

支持 HTTP/SSE 的 OpenAI Chat Completions、OpenAI Responses、Anthropic Messages 和 Gemini GenerateContent；端点分别以 `/chat/completions`、`/responses`、`/messages`、`:generateContent` 或 `:streamGenerateContent` 结尾。另支持 `/responses` 的 WebSocket 模式。客户端使用这些协议且流量经过 PrismCat 即可参与，客户端版本兼容性仍需按真实流量验证。

- 优先利用已观察到的响应 ID、工具调用 ID 与新增工具结果建立续接关系。
- 没有直接引用时，用结构化消息摘要匹配先前请求及响应的上下文。
- 同会话的新普通用户消息会推断为新任务；工具结果不因为被包装为 `user` 角色就拆分。
- 多个候选冲突时保持独立并标记未确定；后到的证据可以修正此前的推断。
- 不因相同 IP、模型、系统 Prompt、时间接近或完全相同的初始指令而归并。
- 默认只在同一上游入口内关联。建议为不同客户端配置不同入口，底层目标服务可以相同。

日志详情展示归属来源、关联依据、Session 和直接前序日志。一个 Session 可以包含多个任务 Trace。对话时序图按日志顺序展示；“请求关联图”仅依据直接前序画虚线，支持分支和多个前序，不为相邻请求补连线。自动归属的时间线按唯一前序折叠历史，避免并行分支互相遮蔽。序号不证明因果或完成顺序。

自动关联异步处理，请求 body 快照和完成后的响应用于补充证据，页面可能短暂显示未关联。日志详情的归属信息会自动刷新，不重复读取完整 body。

## 客户端原生字段

- Codex：优先读取每次调用的 `client_metadata["x-codex-turn-metadata"]`，兼容同名 HTTP 头及平铺的 `thread_id` / `turn_id`。`thread_id + turn_id` 在同入口中确定任务归属；同任务的重试、上下文改写可以保持归属。该标识不自动产生父子边。原生 ID 以摘要保存到关联特征中。
- 仅有 `thread-id`、OpenCode 的 `x-opencode-session`，或 Pi/兼容服务的 `x-session-id` / `x-session-affinity` 时，仅标记 Session，仍需工具或上下文证据才能合并 Trace。已知 Session 冲突的请求不会互相关联。
- 不读取通用 `metadata.user_id`、账户 ID、`prompt_cache_key` 或 `x-client-request-id` 作为任务身份。未发送上述字段的 Cherry Studio、Claude Code、OpenCode、Pi、dsh 和自研客户端继续使用协议级关联；本项目尚未逐个验证这些产品的所有版本。

## Responses WebSocket

启用关联且入口允许记录 `/responses` 时，保留连接握手日志，并为每条 `response.create` 生成独立调用记录。记录中的 POST 表示规范化后的模型调用，实际仍通过原 WebSocket 转发。连接 ID 与 `stream_id` 只用于定位连接和分发事件，不直接合并任务；`previous_response_id` 和工具回传仍参与任务判断。

支持掩码、分片、控制帧、并行 `stream_id`、同通道 FIFO、协商的 `permessage-deflate`（包括 context takeover）。原始字节与扩展协商保持透传；解码后的响应事件以 SSE 形式保存，以复用时间线、JSONL 导出、用量提取及分析。详情中显示 WebSocket 类型、连接和通道；HTTP 重放与 cURL 导出在此类日志中不可用。

批次归属在每条 `response.create` 接收完成时冻结，因此复用长连接也可先后加入不同批次。WebSocket 握手中的 Codex turn 头不用于后续调用的任务身份，避免长连接复用后读取旧 turn；每次调用携带的原生元数据仍有效。

每个连接的旁路队列上限为 128 块/2 MiB，单消息（含解压后）8 MiB，待完成调用最多 64 个、其请求和响应合计 16 MiB，同时遵守日志配置的 body 上限。超限、未知扩展/客户端事件、未匹配响应或断线会停止当前连接的观察，在连接及未完成调用中标记原因，原始转发继续；无法确认完成的调用状态记为未知，不伪造成功。`response.steer` 自动续答及 Realtime 二进制协议尚未适配。

## 录制一次实验

1. 在调用链页面点击“新建录制”。
2. 选择一个已开启日志的代理入口。名称可选，留空会按入口和时间自动生成。
3. 点击“开始录制”，在对应 Agent 中执行任务。
4. 在“录制批次”标签查看状态，任务完成后点击“停止录制”。点击“查看”在侧边详情中浏览该批次的请求及内部 Trace。

一个入口同时只有一个活动批次，不同入口可以同时录制。请求进入代理时冻结批次归属；停止后新请求不再加入，已开始的流式请求仍留在原批次。

普通批次记录该入口的全部已记录请求，包括无法自动关联的辅助请求，无需启用自动关联。若确认入口独占、一次只运行一项任务，可在新建弹窗的“更多选项”中勾选“将本次录制合成一条调用链”；这需要启用自动关联，来源会标记为录制指定，仍不覆盖显式 Trace 头。关闭弹窗或离开页面不会停止后台录制。

批次列表每页展示 20 个批次，录制中的批次优先，默认展示请求数、请求跨度耗时及已知 Token。侧边详情展示输入/输出 Token 和缺失用量数量，请求记录每页 50 条，点击时才读取原文。用量提取未启用或服务端缺失 usage 时显示未知，不当成零；重复请求和历史 Token 不因展示折叠而扣除。

调用链列表仅加载轻量状态，进入批次标签后才查询该页统计。录制中的批次每 5 秒刷新；停止后不再轮询，已开始的请求完成后可手动刷新。浏览器页面隐藏时暂停轮询。

## 人工纠正

打开日志详情，点击“调整归属”：输入目标 Trace ID 可移动当前请求，点击“拆为新 Trace”可单独建立任务。修改后锁定该请求，后台推断不覆盖，并在数据库保留修改历史。已有显式 Trace 头的日志不允许通过此入口修改。

纠正采用版本检查；若其他操作已改变归属，刷新后重试。列表、筛选、导出、时间线和 AI 分析共用有效归属；关联与日志内容更新会使缓存失效。

## 边界

- 跨入口关联、历史重建、压缩历史的通用桥接及复杂子 Agent 关系尚未适配。原生字段取决于客户端版本和服务端是否保留，不代表对所有客户端的兼容承诺。
- 独立子 Agent、标题/摘要请求、上下文压缩或客户端自动注入的 `user` 消息可能缺少任务边界证据，不能保证自动还原。测试批次可保留这些流量，人工纠正用于消除歧义。
- 原始 body/解压内容超过 8 MiB、捕获不完整、未知协议或关联队列超限时降级。关联队列最多 32 项和 16 MiB 原始数据；活跃索引最多 2048 条观察和 65536 条消息特征，最近 30 分钟的特征可用于重启恢复。
- 未记录的路径不会为关联额外捕获内容。日志仍遵守原有捕获上限、异步队列及保留策略；自动关联不保证保存本已被日志系统丢弃的流量。
- 使用完整结束的 SSE 响应建立响应内容证据。中断且缺少完整响应的流不参与内容续接判断；仍可查看原始日志并人工归类。
- 查看降级计数：`GET /api/trace-correlation/status`。自动关联开关下也会提示本进程跳过的观察数量。

新增管理接口沿用控制台认证：`GET/POST /api/capture-runs`、`POST /api/capture-runs/{id}/stop`、`GET/PATCH /api/logs/{id}/trace`。日志查询与导出支持 `capture_run_id`。手动 PATCH 参数为 `trace_id` 与当前关联 `revision`；空 Trace ID 表示拆为新组。

实现依据：[OpenAI WebSocket 模式](https://developers.openai.com/api/docs/guides/websocket-mode)、[Codex 原生元数据源码](https://github.com/openai/codex/blob/main/codex-rs/core/src/responses_metadata.rs)、[Codex 会话头源码](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/requests/headers.rs)、[Gemini GenerateContent](https://ai.google.dev/api/generate-content)、[Pi 会话 affinity 源码](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/api/anthropic-messages.ts)。这些链接指向当前上游，字段可能随版本变化。

## 管理任务和批次

列表的“管理⌄”菜单支持归档、恢复和删除；点击“批量管理”后，可选择本页项目一起处理，并收起单项管理菜单。调用链删除会同时删除其请求日志；批次删除只删除批次记录，保留原始日志和 Trace 归属。录制中或仍被复盘引用的源任务需先处理后再删除。详见 [任务管理与清理](TASK_MANAGEMENT_CN.md)。
