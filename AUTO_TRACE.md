# Automatic traces and capture runs

PrismCat can infer task traces without `X-PrismCat-Trace-ID`. Explicit trace headers remain authoritative. Enable **Automatically correlate requests** under **Settings → Logging → General → Trace correlation**. Traces shows a compact status and a link to this setting. Alternatively, set:

```yaml
trace_correlation:
  enabled: true
```

The feature is off by default and applies to new traffic. It does not inject headers, modify forwarded bodies, or rebuild historical logs.

## Correlation

Supports HTTP/SSE endpoints ending in `/chat/completions`, `/responses`, `/messages`, `:generateContent`, or `:streamGenerateContent` (OpenAI Chat, Responses, Anthropic Messages and Gemini). Responses WebSocket calls are also observed individually. Client compatibility depends on the actual protocol and version, rather than a product name.

Observed response IDs and newly returned tool results are preferred evidence. Otherwise, structural context digests can identify continuations. New ordinary user input is inferred as a new task within the same session. Tool results wrapped in a user message are treated as tool results. Conflicting candidates remain separate, and later evidence can revise earlier inferences.

Matching is isolated by upstream entry. Identical prompts, models, IPs, system prompts, or nearby timestamps alone do not merge tasks. Use separate entries for different clients, even when they point to the same provider.

Log details show source, reason, session, and observed parents. Assignment metadata refreshes automatically. Processing is asynchronous and uses request-body snapshots and completed response captures, so grouping can briefly lag forwarding. The diagram tab offers a request relationship graph that draws only recorded parent links, including forks and joins. Automatic timelines fold against a unique parent rather than the preceding sibling. Sequence numbers do not prove causality or completion order.

## Native client metadata and WebSocket

Codex per-call `client_metadata["x-codex-turn-metadata"]` (object or JSON string), the equivalent HTTP header, and flat compatibility `thread_id` / `turn_id` identify a task within one upstream. Native identifiers are digested in correlation features. Retries and context changes with the same native turn can retain membership without inventing parent links. A `thread-id` header alone, OpenCode `x-opencode-session`, and Pi/compatible service `x-session-id` or `x-session-affinity` establish sessions only. Account IDs, generic `metadata.user_id`, cache keys and request IDs are not task identities. Clients without these fields use protocol evidence. Individual versions of Cherry Studio, Claude Code, Codex, OpenCode, Pi, dsh and custom clients still require real-traffic validation.

For WebSocket `/responses`, the handshake remains a separate log and each `response.create` becomes a normalized POST call record. Its response events are stored as SSE for inspection, usage extraction, timeline and export. Forwarded frames are unchanged. Connection IDs and `stream_id` route events only; lineage comes from response IDs, tool results or native per-call metadata. Capture membership freezes for each complete create event, allowing reused connections to span capture runs. Handshake Codex turn metadata is not reused for subsequent calls. HTTP replay and cURL export are unavailable for WebSocket records.

The passive observer handles masking, fragmentation, control frames, FIFO lanes, parallel streams and negotiated permessage-deflate including context takeover. Limits per connection: 128 chunks/2 MiB queued, 8 MiB per message including decompression, 64 pending calls and 16 MiB total pending request/response data, plus configured body limits. Limits, unsupported extensions/client events, unmatched responses or disconnects mark incomplete observation and leave forwarding running. Unknown completion remains status zero. `response.steer` automatic successors and Realtime binary messages are not supported.

## Capture experiments

Choose **New capture** in Traces, select an upstream with logging enabled, and start recording. The name is optional; leaving it blank generates one from the entry and time. Use the **Capture runs** tab to stop recording and open a run's request details in a side panel. Closing the dialog or leaving the page does not stop recording. Each entry can have one active run; separate entries can record concurrently. Membership is frozen when a request enters the proxy, so in-flight streams remain in their original run after stopping.

A regular run works without automatic correlation and includes all logged traffic on its entry, including unassigned auxiliary calls, while retaining internal trace groups. If an entry is dedicated to exactly one task, enable **Group this capture into one trace** under **More options** in the dialog. This requires correlation to be enabled and still preserves explicit trace headers.

The list shows 20 runs per page, with active captures first, and displays request counts, request-span wall time, and known tokens. Details show input/output tokens and missing-usage counts, with 50 requests per page and bodies loaded only when opened. Missing usage is unknown, not zero. Enable existing usage extraction if token totals are needed. Display deduplication never subtracts repeated calls or billed history tokens.

Traces loads lightweight status only; capture aggregates load when their tab opens. Active captures refresh every five seconds. Stopped captures do not poll; refresh manually after in-flight requests finish. Polling pauses while the browser page is hidden.

## Corrections and limits

In log details, choose **Adjust assignment** to move a request to a trace or split it into a new one. Corrections are version checked, locked against automatic changes, and audited in the database. Explicit-header assignments cannot be changed here. Lists, filters, exports, timelines, and analysis use the same effective grouping, with revision-aware cache invalidation.

Cross-entry matching, historical rebuilds, generic compaction bridges and complex subagent relationships remain future work. Independent subagents, auxiliary calls, compressed context, and automated user-role messages may lack sufficient identity information. Capture runs and manual corrections cover those cases without inventing causal relationships.

Parsing is bounded to 8 MiB of captured/decoded data. The independent correlation queue is limited to 32 items and 16 MiB of raw data. Active indices are limited to 2048 observations and 65536 message features; recent 30-minute summaries support restart recovery. Incomplete or unsupported content degrades conservatively. Completed SSE responses provide response-content evidence; interrupted streams without a complete response remain available as raw logs for manual grouping. Disabled logging/path filters remain respected.

Queue pressure skips correlation observations without blocking forwarding; existing log capture limits and best-effort persistence still apply. The switch displays skipped-observation counts, also available at `GET /api/trace-correlation/status`.

Management endpoints use the existing console authentication: `GET/POST /api/capture-runs`, `POST /api/capture-runs/{id}/stop`, and `GET/PATCH /api/logs/{id}/trace`. Log queries/exports accept `capture_run_id`. Assignment PATCH accepts a target `trace_id` and current `revision`; an empty target creates a separate trace.

Protocol references: [OpenAI WebSocket mode](https://developers.openai.com/api/docs/guides/websocket-mode), [Codex metadata](https://github.com/openai/codex/blob/main/codex-rs/core/src/responses_metadata.rs), [Gemini GenerateContent](https://ai.google.dev/api/generate-content), [Pi session affinity](https://github.com/badlogic/pi-mono/blob/main/packages/ai/src/api/anthropic-messages.ts). Upstream fields may change between versions.

## Managing tasks and captures

Use the Manage menu to archive, restore or delete items. Click Batch manage to select items on the current page; individual menus are hidden in this mode. Trace deletion includes its logs; capture deletion preserves original logs and trace membership. Active or referenced sources must be resolved before deletion. See [Task management and cleanup](TASK_MANAGEMENT.md).
