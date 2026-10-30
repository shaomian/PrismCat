import i18n from '../i18n'
import type { TraceOrigin, TraceTaskMetadata } from './experiments'

// API 响应类型
export interface RequestLog {
    id: string
    created_at: string
    upstream: string
    upstream_target?: string
    target_url: string
    method: string
    path: string
    query?: string
    request_headers?: Record<string, string[]>
    request_body?: string
    request_body_original?: string
    request_body_final?: string
    request_body_ref?: string
    request_body_size: number
    status_code: number
    response_headers?: Record<string, string[]>
    response_body?: string
    response_body_ref?: string
    response_body_size: number
    streaming: boolean
    latency_ms: number
    error?: string
    truncated: boolean
    tag?: string
    trace_association?: TraceAssociation
    capture_run_id?: string
    websocket?: { connection_id: string; stream_id?: string; kind: 'connection' | 'response'; observation_error?: string }
    trace_id?: string
    parent_log_id?: string
    trace_seq?: number
    request_override_applied?: boolean
    request_override_rules?: string[]
    request_override_error?: string
    request_header_override_applied?: boolean
    request_header_override_changes?: HeaderOverrideChange[]
    request_headers_original?: Record<string, string[]>
    usage_input_tokens?: number
    usage_output_tokens?: number
    usage_total_tokens?: number
    usage_raw?: string
    usage_source?: string
    annotation: LogAnnotation
}

export interface HeaderOverrideChange {
    op: string
    name: string
    value?: string
    old_value?: string
}

export type LogAnnotationStatus = 'none' | 'todo' | 'done'

export interface LogAnnotation {
    saved: boolean
    status: LogAnnotationStatus
    note?: string
    labels?: string[]
    created_at?: string
    updated_at?: string
}

export type LiveLogEvent =
    | {
        type: 'snapshot'
        log?: RequestLog
    }
    | {
        type: 'response_chunk'
        chunk?: string
        size_delta?: number
    }
    | {
        type: 'completed'
        log?: RequestLog
    }

export interface LogListResponse {
    logs: RequestLog[]
    total: number
    offset: number
    limit: number
}

export interface LogStats {
    total_requests: number
    success_count: number
    error_count: number
    streaming_count: number
    avg_latency_ms: number
    by_upstream: Record<string, number>
    by_status_code: Record<string, number>
    /** 统计窗口按固定桶数切分的时间序列,供迷你趋势图使用 */
    trend_interval_ms?: number
    trend?: StatsTrendPoint[]
}

export interface StatsTrendPoint {
    start_unix_ms: number
    total: number
    errors: number
}

export interface Upstream {
    name: string
    target: string
    timeout: number
    response_header_timeout: number
    response_body_first_byte_timeout: number
    response_body_idle_timeout: number
    sse_keepalive: SSEKeepaliveConfig
    order: number
    outbound_proxy: string
    logging_enabled: boolean
    logging_path_filter?: LoggingPathFilter
    active_target?: string
    targets?: Record<string, UpstreamTarget>
}

export type SSEKeepaliveMode = 'off' | 'after_headers' | 'full'

export interface SSEKeepaliveConfig {
    mode: SSEKeepaliveMode
    interval: number
}

export type PathMatcher = 'ant' | 'regex'
export type LoggingPathMode = 'all' | 'allowlist' | 'denylist'

export interface LoggingPathRule {
    matcher: PathMatcher
    pattern: string
}

export interface LoggingPathFilter {
    mode: LoggingPathMode
    rules: LoggingPathRule[]
}

export interface ModelPathTemplate {
    tag: string
    rules: LoggingPathRule[]
}

export interface SystemModelPathTemplate extends ModelPathTemplate {
    display_name: string
    description: string
    provider: string
    category: string
    initialize: boolean
    includes?: string[]
}

export interface ModelPathTemplatesResponse {
    templates: ModelPathTemplate[]
    system_defaults: SystemModelPathTemplate[]
}

export interface IgnoredPathRecord {
    upstream: string
    path: string
    request_count: number
    last_seen: string
}

export interface IgnoredPathListResponse {
    paths: IgnoredPathRecord[]
    total: number
    total_requests: number
}

export interface IgnoredPathFilter {
    upstream?: string
    path?: string
    sort?: 'last_seen' | 'count'
    order?: 'asc' | 'desc'
    offset?: number
    limit?: number
}

export interface RuleBinding {
    enabled: boolean
    rule_names: string[]
}

export interface UpstreamTarget {
    url: string
    timeout?: number
    response_header_timeout?: number
    response_body_first_byte_timeout?: number
    response_body_idle_timeout?: number
    sse_keepalive?: SSEKeepaliveConfig
    outbound_proxy?: string
    request_overrides?: RuleBinding
    usage_extraction?: RuleBinding
}

// 查询过滤参数
export interface LogFilter {
    capture_run_id?: string
    upstream?: string
    method?: string
    path?: string
    status_code?: number
    tag?: string
    trace_id?: string
    saved?: boolean
    annotation_status?: LogAnnotationStatus
    annotation_label?: string
    start_time?: string
    end_time?: string
    offset?: number
    limit?: number
}

// Trace 类型
export interface TraceSummary {
	origin?: TraceOrigin
	metadata?: TraceTaskMetadata
    sources?: string[]
    trace_id: string
    request_count: number
    first_time: number
    last_time: number
    total_latency_ms: number
    error_count: number
    usage_input_tokens?: number
    usage_output_tokens?: number
    usage_total_tokens?: number
    upstreams: string[] | null
    tags: string[] | null
}

export interface TraceFilter {
	mergeable?: boolean
	q?: string
	archived?: boolean
	pinned?: boolean
	status?: string
	label?: string
    trace_id?: string
    upstream?: string
    tag?: string
    has_error?: boolean
    start_time?: string
    end_time?: string
    offset?: number
    limit?: number
}

export interface TraceListResponse {
    traces: TraceSummary[] | null
    total: number
    offset: number
    limit: number
}

export interface TraceDetail {
    trace_id: string
    requests: RequestLog[]
    summary: {
        request_count: number
        total_latency_ms: number
        error_count: number
        first_time: number
        last_time: number
        upstreams: string[]
        usage_input_tokens?: number
        usage_output_tokens?: number
        usage_total_tokens?: number
    }
}

// API 调用函数
const API_BASE = '/api'
export const DEFAULT_UPSTREAM_TIMEOUT_SECONDS = 300
export const DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS = 15

// ApiError 携带 HTTP 状态码，调用方可按错误类别决策（展示层仍读 message）
export class ApiError extends Error {
    status: number

    constructor(message: string, status: number) {
        super(message)
        this.name = 'ApiError'
        this.status = status
    }
}

// ---------------------------------------------------------------------------
// 全局 401 处理：数据接口收到 401 意味着会话过期，由 App 注册回调统一
// 回到登录页，而不是每个页面各自弹一堆"加载失败"。
// 认证流程自身（登录/初始化/状态查询）不触发该事件——登录密码输错
// 也返回 401，不该被当作会话过期。
// ---------------------------------------------------------------------------
type AuthExpiredHandler = () => void
let authExpiredHandler: AuthExpiredHandler | null = null

export function onAuthExpired(handler: AuthExpiredHandler | null) {
    authExpiredHandler = handler
}

function emitAuthExpired() {
    try {
        authExpiredHandler?.()
    } catch {
        // 回调异常不影响 API 层语义
    }
}

interface ErrorBody {
    error?: string
}

export async function apiRequest(path: string, init: RequestInit, fallbackKey: string): Promise<Response> {
    const response = await fetch(path, init)
    if (!response.ok) {
        if (response.status === 401) {
            emitAuthExpired()
        }
        const error = await response.json().catch(() => ({ error: '' })) as ErrorBody
        throw new ApiError(error.error || i18n.t(fallbackKey), response.status)
    }
    return response
}

export function jsonInit(method: string, body?: unknown): RequestInit {
    return {
        method,
        headers: { 'Content-Type': 'application/json' },
        ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    }
}

export interface AuthStatus {
    authenticated: boolean
    auth_required: boolean
    setup_required: boolean
    session_expires_at?: string
}

// 认证流程函数：不触发全局 401（登录密码错误等业务性 401 不代表会话过期）
export async function fetchAuthStatus(): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/me`)
    if (!response.ok) throw new ApiError(i18n.t('errors.load_auth_status'), response.status)
    return response.json()
}

export async function login(password: string): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/login`, jsonInit('POST', { password }))
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '' })) as ErrorBody
        throw new ApiError(error.error || i18n.t('errors.login'), response.status)
    }
    return response.json()
}

export async function setupPassword(password: string): Promise<AuthStatus> {
    const response = await fetch(`${API_BASE}/auth/setup`, jsonInit('POST', { password }))
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '' })) as ErrorBody
        throw new ApiError(error.error || i18n.t('errors.setup'), response.status)
    }
    return response.json()
}

export async function logout(): Promise<void> {
    const response = await fetch(`${API_BASE}/auth/logout`, { method: 'POST' })
    if (!response.ok) {
        const error = await response.json().catch(() => ({ error: '' })) as ErrorBody
        throw new ApiError(error.error || i18n.t('errors.logout'), response.status)
    }
}

export async function fetchLogs(filter: LogFilter = {}, signal?: AbortSignal): Promise<LogListResponse> {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })

    const response = await apiRequest(`${API_BASE}/logs?${params}`, { signal }, 'errors.load_logs')
    return response.json()
}

export function buildLogsExportUrl(filter: LogFilter = {}, includeBody = true): string {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (key === 'offset' || key === 'limit') return
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })
    params.set('format', 'jsonl')
    params.set('include_body', String(includeBody))
    return `${API_BASE}/logs/export?${params}`
}

export async function fetchLog(id: string, signal?: AbortSignal): Promise<RequestLog> {
    const response = await apiRequest(`${API_BASE}/logs/${encodeURIComponent(id)}`, { signal }, 'errors.load_log_detail')
    return response.json()
}

export async function fetchStats(since?: string): Promise<LogStats> {
    const params = since ? `?since=${since}` : ''
    const response = await apiRequest(`${API_BASE}/stats${params}`, {}, 'errors.load_stats')
    return response.json()
}

export async function fetchUpstreams(): Promise<Upstream[]> {
    const response = await apiRequest(`${API_BASE}/upstreams`, {}, 'errors.load_upstreams')
    return response.json()
}

export async function fetchTraces(filter: TraceFilter = {}, signal?: AbortSignal): Promise<TraceListResponse> {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (value !== undefined && value !== '') {
            params.append(key, String(value))
        }
    })
    const response = await apiRequest(`${API_BASE}/traces?${params}`, { signal }, 'errors.load_traces')
    return response.json()
}

export async function fetchTraceDetail(traceId: string, signal?: AbortSignal): Promise<TraceDetail> {
    const response = await apiRequest(`${API_BASE}/traces/${encodeURIComponent(traceId)}`, { signal }, 'errors.load_trace_detail')
    return response.json()
}

// 调用链对话时间线
export interface TimelineEvent {
    role: string
    kind: 'message' | 'tool_call' | 'tool_result' | 'thinking' | 'image'
    content?: string
    truncated?: boolean
    tool_name?: string
    tool_call_id?: string
    tool_args?: string
}

export interface TimelineRequest {
    trace_association?: TraceAssociation
    log_id: string
    seq: number
    created_at: number
    latency_ms: number
    status_code: number
    error?: string
    upstream: string
    path: string
    model?: string
    format: string
    carried: number
    new_events: TimelineEvent[] | null
    response_events: TimelineEvent[] | null
    parse_error?: string
    usage_input_tokens?: number
    usage_output_tokens?: number
}

export interface TraceTimeline {
    trace_id: string
    requests: TimelineRequest[]
    summary: {
        request_count: number
        parsed_count: number
        total_new_events: number
        total_carried: number
    }
}

export async function fetchTraceTimeline(traceId: string, signal?: AbortSignal): Promise<TraceTimeline> {
    const response = await apiRequest(`${API_BASE}/traces/${encodeURIComponent(traceId)}/timeline`, { signal }, 'errors.load_timeline')
    return response.json()
}

// 按需获取单个请求的完整（未截断）时间线条目。
// 事件数量与顺序和列表构建一致，可按 log_id 原位替换列表中的条目。
export async function fetchTraceTimelineEntry(traceId: string, logId: string): Promise<TimelineRequest> {
    const response = await apiRequest(
        `${API_BASE}/traces/${encodeURIComponent(traceId)}/timeline?full_log=${encodeURIComponent(logId)}`,
        {},
        'errors.load_trace_entry',
    )
    return response.json()
}

export interface DiagramLabel {
    text: string
    log_id?: string
    truncated: boolean
}

export interface TraceDiagram {
    mermaid: string
    labels: DiagramLabel[] | null
    request_offset?: number
    request_total?: number
    request_count?: number
}

export async function fetchTraceDiagram(traceId: string, view: 'sequence' | 'dependencies' = 'sequence', options: { signal?: AbortSignal; offset?: number; limit?: number } = {}): Promise<TraceDiagram> {
    const params = new URLSearchParams({ view })
    if (options.offset !== undefined) params.set('offset', String(options.offset))
    if (options.limit !== undefined) params.set('limit', String(options.limit))
    const response = await apiRequest(`${API_BASE}/traces/${encodeURIComponent(traceId)}/diagram?${params}`, { signal: options.signal }, 'errors.load_diagram')
    return response.json()
}

// 调用链 JSONL 导出下载地址（浏览器直接下载，依赖会话 cookie）
export function traceExportUrl(traceId: string, mode: 'events' | 'raw'): string {
    return `${API_BASE}/traces/${encodeURIComponent(traceId)}/export?mode=${mode}`
}

// AI 分析对话（SSE 流式）
export interface AnalysisMessage {
    role: 'user' | 'assistant'
    content: string
}

export type AnalysisEvent =
    | { kind: 'tool_call'; name: string; args: string }
    | { kind: 'tool_result'; name: string; chars: number }
    | { kind: 'reset' }

export interface AnalysisStreamResult {
    content: string
    error?: string
    usage?: Record<string, unknown>
    rounds?: number
    toolCalls?: number
}

export async function streamAnalysisChat(
    traceId: string,
    messages: AnalysisMessage[],
    onDelta: (delta: string) => void,
    onEvent?: (ev: AnalysisEvent) => void,
    signal?: AbortSignal,
): Promise<AnalysisStreamResult> {
    const response = await fetch(`${API_BASE}/analysis/chat`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ trace_id: traceId, messages }),
        signal,
    })
    if (!response.ok || !response.body) {
        if (response.status === 401) {
            emitAuthExpired()
        }
        const error = await response.json().catch(() => ({ error: '' })) as ErrorBody
        throw new ApiError(error.error || i18n.t('errors.analysis_request'), response.status)
    }

    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    let content = ''
    let result: AnalysisStreamResult = { content: '' }

    const handleEvent = (data: string) => {
        try {
            const parsed = JSON.parse(data)
            if (typeof parsed.delta === 'string') {
                content += parsed.delta
                result = { ...result, content }
                onDelta(parsed.delta)
            } else if (parsed.tool_call) {
                onEvent?.({ kind: 'tool_call', name: parsed.tool_call.name, args: parsed.tool_call.args })
            } else if (parsed.tool_result) {
                onEvent?.({ kind: 'tool_result', name: parsed.tool_result.name, chars: parsed.tool_result.chars })
            } else if (parsed.reset) {
                content = ''
                result = { ...result, content: '' }
                onEvent?.({ kind: 'reset' })
            } else if (parsed.error) {
                result = { ...result, error: parsed.error }
            } else if (parsed.done) {
                result = { ...result, usage: parsed.usage, rounds: parsed.rounds, toolCalls: parsed.tool_calls }
            }
        } catch {
            // 忽略无法解析的行
        }
    }

    for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''
        for (const part of parts) {
            const dataLines = part.split('\n')
                .filter(l => l.startsWith('data:'))
                .map(l => l.slice(5).trim())
            if (dataLines.length > 0) handleEvent(dataLines.join('\n'))
        }
    }
    if (buffer.trim().startsWith('data:')) handleEvent(buffer.trim().slice(5).trim())
    return result
}

export async function addUpstream(
    name: string,
    target: string,
    timeout: number = DEFAULT_UPSTREAM_TIMEOUT_SECONDS,
    response_header_timeout: number = 0,
    response_body_first_byte_timeout: number = 0,
    response_body_idle_timeout: number = 0,
    order: number = 0,
    outbound_proxy: string = 'env',
    logging_enabled: boolean = true,
    active_target: string = '',
    targets?: Record<string, UpstreamTarget>,
    logging_path_filter?: LoggingPathFilter,
    sse_keepalive?: SSEKeepaliveConfig,
): Promise<void> {
    await apiRequest(`${API_BASE}/upstreams`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, target, timeout, response_header_timeout, response_body_first_byte_timeout, response_body_idle_timeout, order, outbound_proxy, logging_enabled, active_target, targets, logging_path_filter, sse_keepalive }),
    }, 'errors.add_upstream')
}

export async function fetchModelPathTemplates(): Promise<ModelPathTemplatesResponse> {
    const response = await apiRequest(`${API_BASE}/logging-rules/model-path-templates`, {}, 'errors.load_model_path_templates')
    return response.json()
}

export async function saveModelPathTemplates(templates: ModelPathTemplate[]): Promise<void> {
    await apiRequest(`${API_BASE}/logging-rules/model-path-templates`, jsonInit('PUT', { templates }), 'errors.save_model_path_templates')
}

export async function fetchIgnoredPaths(filter: IgnoredPathFilter = {}): Promise<IgnoredPathListResponse> {
    const params = new URLSearchParams()
    Object.entries(filter).forEach(([key, value]) => {
        if (value !== undefined && value !== '') params.set(key, String(value))
    })
    const response = await apiRequest(`${API_BASE}/logging-rules/ignored-paths?${params}`, {}, 'errors.load_ignored_paths')
    return response.json()
}

export async function deleteIgnoredPaths(upstream = '', path = ''): Promise<number> {
    const params = new URLSearchParams()
    if (upstream) params.set('upstream', upstream)
    if (path) params.set('path', path)
    const response = await apiRequest(`${API_BASE}/logging-rules/ignored-paths?${params}`, { method: 'DELETE' }, 'errors.delete_ignored_paths')
    const result = await response.json() as { deleted: number }
    return result.deleted
}

export async function activateUpstreamTarget(upstream: string, target: string): Promise<void> {
    await apiRequest(`${API_BASE}/upstreams/active-target`, jsonInit('PATCH', { upstream, target }), 'errors.switch_target')
}

export async function removeUpstream(name: string): Promise<void> {
    await apiRequest(`${API_BASE}/upstreams?name=${encodeURIComponent(name)}`, { method: 'DELETE' }, 'errors.remove_upstream')
}

// 应用配置类型
export interface AppConfig {
    trace_correlation: {
        enabled: boolean
        max_entries?: number
        idle_minutes?: number
    }
    version: string
    server: {
        proxy_domains: string[]
        enable_path_routing: boolean
        path_routing_prefix: string
    }
    logging: {
        max_request_body: number
        max_response_body: number
        sensitive_headers: string[]
        early_request_body_snapshot: boolean
        detach_body_over_bytes: number
        body_preview_bytes: number
        store_base64: boolean
    }
    storage: {
        database: string
        retention_days: number
        max_storage_bytes: number
    }
    request_overrides: {
        enabled: boolean
        max_body_bytes: number
        upstreams: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules: unknown[]
    }
    usage_extraction: {
        enabled: boolean
        upstreams: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules: unknown[]
    }
    analysis: {
        enabled: boolean
        protocol?: 'openai' | 'anthropic'
        base_url: string
        model: string
        has_api_key: boolean
    }
}

export async function updateLogAnnotation(
    id: string,
    update: Partial<Pick<LogAnnotation, 'saved' | 'status' | 'note' | 'labels'>>,
): Promise<LogAnnotation> {
    const response = await apiRequest(
        `${API_BASE}/logs/${encodeURIComponent(id)}/annotation`,
        jsonInit('PATCH', update),
        'errors.save_annotation',
    )
    return response.json()
}

export interface ConfigUpdate {
    trace_correlation?: {
        enabled?: boolean
        max_entries?: number
        idle_minutes?: number
    }
    server?: {
        enable_path_routing?: boolean
        path_routing_prefix?: string
    }
    analysis?: {
        enabled?: boolean
        protocol?: 'openai' | 'anthropic'
        base_url?: string
        api_key?: string
        model?: string
    }
    logging?: {
        max_request_body?: number
        max_response_body?: number
        sensitive_headers?: string[]
        early_request_body_snapshot?: boolean
        detach_body_over_bytes?: number
        body_preview_bytes?: number
        store_base64?: boolean
    }
    storage?: {
        retention_days?: number
        max_storage_bytes?: number
    }
    request_overrides?: {
        enabled?: boolean
        max_body_bytes?: number
        upstreams?: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules?: unknown[]
    }
    usage_extraction?: {
        enabled?: boolean
        upstreams?: Record<string, {
            enabled: boolean
            rule_names: string[]
        }>
        rules?: unknown[]
    }
}

export async function fetchConfig(): Promise<AppConfig> {
    const response = await apiRequest(`${API_BASE}/config`, {}, 'errors.load_config')
    return response.json()
}

export async function fetchAnalysisConfig(): Promise<AppConfig['analysis']> {
    const response = await apiRequest(`${API_BASE}/analysis/config`, {}, 'errors.load_config')
    return response.json()
}

export interface AnalysisModel { id: string; name: string }
export async function syncAnalysisModels(draft: { protocol: 'openai' | 'anthropic'; base_url: string; api_key?: string }, signal: AbortSignal): Promise<{ models: AnalysisModel[]; truncated: boolean }> {
    const response = await apiRequest(`${API_BASE}/analysis/models`, { ...jsonInit('POST', draft), signal }, 'analysis.models_failed')
    return response.json()
}

export interface SystemMetrics {
    timestamp: string
    platform: string
    runtime: {
        go_version: string
        num_cpu: number
        goroutines: number
        uptime_seconds: number
    }
    process: {
        pid: number
        rss_bytes?: number
        heap_alloc_bytes: number
        heap_sys_bytes: number
        cpu_seconds?: number
        cpu_percent?: number
    }
    memory: {
        total_bytes?: number
        used_bytes?: number
        available_bytes?: number
        source: string
    }
}

export async function fetchSystemMetrics(): Promise<SystemMetrics> {
    const response = await apiRequest(`${API_BASE}/system/metrics`, {}, 'errors.load_metrics')
    return response.json()
}

export interface StorageUsage {
    calculated_at: string
    blob_store: string
    database_bytes: number
    database_files: number
    blob_bytes: number
    blob_files: number
    total_bytes: number
}

export async function fetchStorageUsage(): Promise<StorageUsage> {
    const response = await apiRequest(`${API_BASE}/system/storage`, {}, 'errors.load_storage')
    return response.json()
}

export interface UpdateAsset {
    name: string
    download_url: string
    size: number
}

export interface UpdateInfo {
    current_version: string
    latest_version: string
    latest_tag: string
    update_available: boolean
    release_url: string
    published_at: string
    platform: string
    arch: string
    assets: UpdateAsset[]
    matching_asset?: UpdateAsset
}

export async function fetchUpdateInfo(): Promise<UpdateInfo> {
    const response = await apiRequest(`${API_BASE}/system/update`, {}, 'errors.check_update')
    return response.json()
}

export async function updateConfig(update: ConfigUpdate): Promise<void> {
    await apiRequest(`${API_BASE}/config`, jsonInit('PUT', update), 'errors.update_config')
}

export async function fetchBlob(ref: string): Promise<string> {
    const response = await apiRequest(`${API_BASE}/blobs/${encodeURIComponent(ref)}`, {}, 'errors.load_blob')
    return response.text()
}

export interface LogBodyResponse {
    body: string
    truncated?: boolean
    body_decoded?: boolean
    body_decoded_from?: string
    decode_failed?: boolean
}

export async function fetchLogBody(id: string, part: 'request' | 'response'): Promise<LogBodyResponse> {
    const params = new URLSearchParams({ part })
    const response = await apiRequest(`${API_BASE}/logs/${encodeURIComponent(id)}/body?${params}`, {}, 'errors.load_body')
    return response.json()
}

// Replay (Playground)
export interface ReplayRequest {
    upstream?: string
    target_url?: string
    method: string
    path?: string
    headers: Record<string, string>
    body: string
}

export interface ReplayResponse {
    status_code: number
    headers: Record<string, string[]>
    body: string
    truncated?: boolean
    body_decoded?: boolean
    body_decoded_from?: string
}

export async function sendReplay(req: ReplayRequest): Promise<ReplayResponse> {
    const response = await apiRequest(`${API_BASE}/replay`, jsonInit('POST', req), 'errors.send_replay')
    return response.json()
}

export interface TraceAssociation {
    trace_id: string
    session_id?: string
    client?: string
    source: 'auto' | 'explicit' | 'manual' | 'capture'
    reason: string
    state: string
    parents?: string[]
    revision: number
    locked: boolean
}

export interface CaptureRun {
    id: string
    name: string
    upstream: string
    started_at: string
    stopped_at?: string
    single_task: boolean
    archived?: boolean
    revision?: number
    request_count: number
    total_latency_ms: number
    wall_time_ms: number
    usage_input_tokens?: number
    usage_output_tokens?: number
    usage_missing_count: number
}

export async function fetchCaptureRuns(page: { limit?: number; offset?: number; archived?: boolean } = {}, signal?: AbortSignal): Promise<{ runs: CaptureRun[]; total?: number }> {
    const params = new URLSearchParams()
    if (page.limit !== undefined) params.set('limit', String(page.limit))
    if (page.offset !== undefined) params.set('offset', String(page.offset))
    params.set('archived', String(page.archived ?? false))
    const response = await apiRequest(API_BASE + '/capture-runs?' + params, { signal }, 'correlation.failed')
    return response.json()
}

export async function startCaptureRun(name: string, upstream: string, single_task: boolean): Promise<CaptureRun> {
    const response = await apiRequest(API_BASE + '/capture-runs', jsonInit('POST', { name, upstream, single_task }), 'correlation.failed')
    return response.json()
}

export async function stopCaptureRun(id: string): Promise<void> {
    await apiRequest(API_BASE + '/capture-runs/' + encodeURIComponent(id) + '/stop', jsonInit('POST'), 'correlation.failed')
}

export async function assignLogTrace(id: string, trace_id: string, revision: number): Promise<TraceAssociation> {
    const response = await apiRequest(API_BASE + '/logs/' + encodeURIComponent(id) + '/trace', jsonInit('PATCH', { trace_id, revision }), 'correlation.failed')
    return response.json()
}

export interface CorrelationStatus {
    enabled: boolean
    max_entries: number
    idle_minutes: number
    queued_bytes: number
    dropped_observations: number
    active_captures: { id: string; upstream: string }[]
}

export async function fetchCorrelationStatus(signal?: AbortSignal): Promise<CorrelationStatus> {
 const response = await apiRequest(API_BASE + '/trace-correlation/status', { signal }, 'correlation.failed')
 return response.json()
}

export type TraceMetadata = Pick<RequestLog, 'trace_id' | 'trace_seq' | 'trace_association' | 'capture_run_id'>
export async function fetchTraceMetadata(id: string): Promise<TraceMetadata> {
 const response = await apiRequest(API_BASE + '/logs/' + encodeURIComponent(id) + '/trace', {}, 'correlation.failed')
 return response.json()
}
