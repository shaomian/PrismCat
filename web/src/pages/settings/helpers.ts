import { DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS, DEFAULT_UPSTREAM_TIMEOUT_SECONDS } from '@/lib/api'
import type { LoggingPathFilter, SSEKeepaliveMode, UpstreamTarget } from '@/lib/api'

export type TranslationFn = (key: string, options?: Record<string, unknown>) => string

export const requestOverrideExample = JSON.stringify([
    {
        name: 'Example: default max_tokens',
        enabled: false,
        match: {
            methods: ['POST'],
        },
        patch: [
            { op: 'default', path: 'max_tokens', value: 4096 },
        ],
    },
], null, 2)

export const usageExtractionExample = JSON.stringify([
    {
        name: 'OpenAI compatible',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/prompt_tokens', '/usage/input_tokens'],
            output_tokens: ['/usage/completion_tokens', '/usage/output_tokens'],
            total_tokens: ['/usage/total_tokens'],
            raw_usage: ['/usage'],
        },
    },
    {
        name: 'OpenAI Responses',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/input_tokens', '/response/usage/input_tokens'],
            output_tokens: ['/usage/output_tokens', '/response/usage/output_tokens'],
            total_tokens: ['/usage/total_tokens', '/response/usage/total_tokens'],
            raw_usage: ['/usage', '/response/usage'],
        },
    },
    {
        name: 'Anthropic',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usage/input_tokens', '/message/usage/input_tokens'],
            output_tokens: ['/usage/output_tokens', '/message/usage/output_tokens'],
            raw_usage: ['/usage', '/message/usage'],
        },
    },
    {
        name: 'Gemini',
        enabled: true,
        match: {
            content_types: ['application/json', 'text/event-stream'],
        },
        paths: {
            input_tokens: ['/usageMetadata/promptTokenCount'],
            output_tokens: ['/usageMetadata/candidatesTokenCount'],
            total_tokens: ['/usageMetadata/totalTokenCount'],
            raw_usage: ['/usageMetadata'],
        },
    },
], null, 2)

export function getErrorMessage(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback
}

export type OverrideBinding = {
    enabled: boolean
    rule_names: string[]
}

export type EditingUpstream = {
    name: string
    target: string
    timeout: number
    responseHeaderTimeout: number
    responseBodyFirstByteTimeout: number
    responseBodyIdleTimeout: number
    sseKeepaliveMode: SSEKeepaliveMode
    sseKeepaliveInterval: number
    order: number
    outboundProxy: string
    loggingEnabled: boolean
    loggingPathFilter: LoggingPathFilter
    overrideEnabled: boolean
    ruleNames: string[]
    usageEnabled: boolean
    usageRuleNames: string[]
    usesTargetPresets: boolean
    activeTarget: string
    selectedTarget: string
    targets: Record<string, UpstreamTarget>
}

export type OutboundProxyMode = 'env' | 'direct' | 'custom'
export type RuleTab = 'request_overrides' | 'usage_extraction'
export type OverrideRuleObject = Record<string, unknown>
export type HeaderOp = { op: string; name: string; value?: string }

export const customProxyPlaceholder = 'http://127.0.0.1:7890'

export function editingBufferAsTarget(upstream: EditingUpstream): UpstreamTarget {
    return {
        url: upstream.target,
        timeout: upstream.timeout,
        response_header_timeout: upstream.responseHeaderTimeout,
        response_body_first_byte_timeout: upstream.responseBodyFirstByteTimeout,
        response_body_idle_timeout: upstream.responseBodyIdleTimeout,
        sse_keepalive: {
            mode: upstream.sseKeepaliveMode,
            interval: upstream.sseKeepaliveInterval,
        },
        outbound_proxy: upstream.outboundProxy,
        request_overrides: {
            enabled: upstream.overrideEnabled,
            rule_names: upstream.ruleNames,
        },
        usage_extraction: {
            enabled: upstream.usageEnabled,
            rule_names: upstream.usageRuleNames,
        },
    }
}

export function commitSelectedTarget(upstream: EditingUpstream): EditingUpstream {
    if (!upstream.usesTargetPresets || !upstream.selectedTarget) return upstream
    return {
        ...upstream,
        targets: {
            ...upstream.targets,
            [upstream.selectedTarget]: editingBufferAsTarget(upstream),
        },
    }
}

export function loadTargetBuffer(upstream: EditingUpstream, targetName: string): EditingUpstream {
    const target = upstream.targets[targetName]
    if (!target) return upstream
    return {
        ...upstream,
        selectedTarget: targetName,
        target: target.url || '',
        timeout: target.timeout || DEFAULT_UPSTREAM_TIMEOUT_SECONDS,
        responseHeaderTimeout: target.response_header_timeout || 0,
        responseBodyFirstByteTimeout: target.response_body_first_byte_timeout || 0,
        responseBodyIdleTimeout: target.response_body_idle_timeout || 0,
        sseKeepaliveMode: target.sse_keepalive?.mode || 'off',
        sseKeepaliveInterval: target.sse_keepalive?.interval || DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS,
        outboundProxy: target.outbound_proxy || 'env',
        overrideEnabled: target.request_overrides?.enabled ?? false,
        ruleNames: target.request_overrides?.rule_names || [],
        usageEnabled: target.usage_extraction?.enabled ?? false,
        usageRuleNames: target.usage_extraction?.rule_names || [],
    }
}

export function getOverrideRuleName(rule: unknown, fallback: string) {
    if (!rule || typeof rule !== 'object') return fallback
    const name = (rule as { name?: unknown }).name
    return typeof name === 'string' && name.trim() ? name.trim() : fallback
}

// canonicalJson 生成与键序无关的规范化 JSON，用于「编辑器内容是否
// 与规则对象语义一致」的判断（见选中规则同步 effect）。
export function canonicalJson(value: unknown): string {
    if (value === null || typeof value !== 'object') return JSON.stringify(value) ?? 'null'
    if (Array.isArray(value)) return `[${value.map(canonicalJson).join(',')}]`
    const entries = Object.entries(value as Record<string, unknown>)
        .filter(([, v]) => v !== undefined)
        .sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
    return `{${entries.map(([k, v]) => `${JSON.stringify(k)}:${canonicalJson(v)}`).join(',')}}`
}

export function getOverrideRuleEnabled(rule: unknown) {
    if (!rule || typeof rule !== 'object') return false
    const enabled = (rule as { enabled?: unknown }).enabled
    return enabled !== false
}

export function getBindingRuleNames(binding?: OverrideBinding) {
    return Array.isArray(binding?.rule_names) ? binding.rule_names : []
}

// 高级设置面板「有改动才默认展开」的判据。记录日志不在其中 —— 它是上游级的,
// 已经移到面板外面了。
export function hasAdvancedValues(source?: {
    response_header_timeout?: number
    response_body_first_byte_timeout?: number
    response_body_idle_timeout?: number
    sse_keepalive?: { mode?: SSEKeepaliveMode }
    request_overrides?: OverrideBinding
    usage_extraction?: OverrideBinding
}) {
    if (!source) return false
    return (source.response_header_timeout || 0) > 0
        || (source.response_body_first_byte_timeout || 0) > 0
        || (source.response_body_idle_timeout || 0) > 0
        || (source.sse_keepalive?.mode || 'off') !== 'off'
        || (source.request_overrides?.enabled ?? false)
        || (source.usage_extraction?.enabled ?? false)
        || getBindingRuleNames(source.request_overrides).length > 0
        || getBindingRuleNames(source.usage_extraction).length > 0
}

export type BindingKind = 'request_overrides' | 'usage_extraction'

export function outboundProxyMode(value?: string): OutboundProxyMode {
    const normalized = (value || '').trim().toLowerCase()
    if (!normalized || normalized === 'env') return 'env'
    if (normalized === 'direct') return 'direct'
    return 'custom'
}

export function normalizedOutboundProxy(value: string): string {
    return value.trim() || 'env'
}

export function uniqueRuleName(baseName: string, rules: OverrideRuleObject[]) {
    const names = new Set(rules.map(rule => getOverrideRuleName(rule, '')))
    if (!names.has(baseName)) return baseName
    let suffix = 2
    while (names.has(`${baseName} ${suffix}`)) suffix += 1
    return `${baseName} ${suffix}`
}

export function isSensitiveHeaderName(name: string, configuredHeaders: string) {
    const normalized = name.trim().toLowerCase()
    if (!normalized) return false
    const defaults = ['authorization', 'proxy-authorization', 'x-api-key', 'api-key']
    const configured = configuredHeaders
        .split('\n')
        .map(value => value.trim().toLowerCase())
        .filter(Boolean)
    return defaults.includes(normalized) || configured.includes(normalized)
}

export function redactSensitiveRuleHeaders(rule: OverrideRuleObject, configuredHeaders: string) {
    const headers = Array.isArray(rule.headers) ? rule.headers : []
    return {
        ...rule,
        ...(headers.length > 0
            ? {
                headers: headers.map(header => {
                    if (!header || typeof header !== 'object' || Array.isArray(header)) return header
                    const item = header as Record<string, unknown>
                    const name = typeof item.name === 'string' ? item.name : ''
                    if (!isSensitiveHeaderName(name, configuredHeaders) || typeof item.value !== 'string') return item
                    return { ...item, value: '••••••••' }
                }),
            }
            : {}),
    }
}
