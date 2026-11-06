import { useState, useEffect, useId, useMemo, useCallback, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { useLocation } from 'react-router-dom'
import { Send, Plus, Trash2, Loader2, Copy, Check, Braces, AlertCircle } from 'lucide-react'
import { toast } from 'sonner'
import { cn, getStatusColor, formatSize, generateId, FOCUS_RING, FOCUS_RING_DANGER } from '@/lib/utils'
import { copyText } from '@/lib/clipboard'
import { fetchUpstreams, sendReplay } from '@/lib/api'
import type { Upstream, ReplayResponse } from '@/lib/api'
import { JsonViewer } from '@/components/JsonViewer'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

const HTTP_METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'] as const

interface HeaderEntry {
    key: string
    value: string
    id: string
}

interface ReplayLocationState {
    replay?: {
        upstream?: string
        target_url?: string
        targetUrl?: string
        method?: string
        path?: string
        body?: string
        headers?: Record<string, string | string[]>
    }
}

type RequestTab = 'body' | 'headers'
type ResponseViewMode = 'pretty' | 'raw'
type RequestBodyViewMode = 'raw' | 'pretty'
type TargetMode = 'upstream' | 'url'

function getErrorMessage(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback
}

// 模块级组件：定义在 Playground 内部会在每次输入触发重渲染时产生新的
// 组件类型，导致子树被整棵重挂载（丢焦点、丢 hover 态）
function RawBodyViewer({ text }: { text: string }) {
    return (
        <pre className="whitespace-pre-wrap break-all text-xs font-mono leading-relaxed text-foreground select-text">
            {text}
        </pre>
    )
}

function ViewToggle({
    value,
    onChange,
}: {
    value: ResponseViewMode
    onChange: (value: ResponseViewMode) => void
}) {
    const { t } = useTranslation()
    return (
        <div className="flex items-center gap-1 rounded-md border border-border bg-background p-1">
            {([
                { value: 'pretty', label: t('log_detail.view_pretty') },
                { value: 'raw', label: t('log_detail.view_raw') },
            ] as const).map((option) => (
                <Button
                    key={option.value}
                    type="button"
                    variant={value === option.value ? 'secondary' : 'ghost'}
                    size="sm"
                    onClick={() => onChange(option.value)}
                    className={cn(
                        'h-6 px-2 text-xs font-medium',
                        value === option.value && 'shadow-none'
                    )}
                >
                    {option.label}
                </Button>
            ))}
        </div>
    )
}

export function Playground() {
    const { t, i18n } = useTranslation()
    const location = useLocation()
    const jsonErrorId = useId()

    // Form state
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [targetMode, setTargetMode] = useState<TargetMode>('upstream')
    const [upstream, setUpstream] = useState('')
    const [targetUrl, setTargetUrl] = useState('')
    const [method, setMethod] = useState('POST')
    const [path, setPath] = useState('')
    const [headers, setHeaders] = useState<HeaderEntry[]>([
        { key: 'Content-Type', value: 'application/json', id: generateId() },
    ])
    const [body, setBody] = useState('')

    // Response state
    const [response, setResponse] = useState<ReplayResponse | null>(null)
    const [sending, setSending] = useState(false)
    const [error, setError] = useState<string | null>(null)
    const [elapsed, setElapsed] = useState<number | null>(null)
    const [copiedField, setCopiedField] = useState<string | null>(null)

    // UI state
    const [activeTab, setActiveTab] = useState<RequestTab>('body')
    const [requestBodyViewMode, setRequestBodyViewMode] = useState<RequestBodyViewMode>('raw')
    const [responseViewMode, setResponseViewMode] = useState<ResponseViewMode>('pretty')

    // Load upstreams
    useEffect(() => {
        fetchUpstreams()
            .then((data) => {
                setUpstreams(data || [])
                setUpstream((current) => current || data?.[0]?.name || '')
            })
            .catch((err: unknown) => {
                // 上游列表拉不下来时明确提示，而不是静默留空的下拉框
                toast.error(getErrorMessage(err, i18n.t('app.load_upstreams_failed')))
            })
    }, [i18n])

    // Pre-fill from navigation state (replay from LogDetail)
    useEffect(() => {
        const state = location.state as ReplayLocationState | null
        if (state?.replay) {
            const r = state.replay
            const replayTargetUrl = r.target_url || r.targetUrl || ''
            if (replayTargetUrl) setTargetUrl(replayTargetUrl)
            if (r.upstream) {
                setTargetMode('upstream')
                setUpstream(r.upstream)
            } else if (replayTargetUrl) {
                setTargetMode('url')
            }
            if (r.method) setMethod(r.method)
            if (r.path) setPath(r.path)
            if (r.body) setBody(r.body)
            if (r.headers && typeof r.headers === 'object') {
                const entries: HeaderEntry[] = Object.entries(r.headers)
                    .filter(([k]) => {
                        const skip = ['host', 'connection', 'keep-alive', 'transfer-encoding', 'te', 'trailer', 'upgrade', 'proxy-authorization', 'proxy-authenticate', 'proxy-connection']
                        return !skip.includes(k.toLowerCase())
                    })
                    .map(([key, value]) => ({
                        key,
                        value: Array.isArray(value) ? value.join('; ') : value,
                        id: generateId()
                    }))
                if (entries.length > 0) setHeaders(entries)
            }
            window.history.replaceState({}, '')
        }
    }, [location.state])

    const selectedUpstreamExists = useMemo(
        () => upstreams.some((u) => u.name === upstream),
        [upstreams, upstream],
    )
    const selectedUpstreamMissing = targetMode === 'upstream' && upstream !== '' && upstreams.length > 0 && !selectedUpstreamExists

    useEffect(() => {
        if (selectedUpstreamMissing && targetUrl) {
            setTargetMode('url')
        }
    }, [selectedUpstreamMissing, targetUrl])

    const parsedRequestBody = useMemo(() => {
        const text = body.trim()
        if (!text) return null
        try {
            return JSON.parse(text)
        } catch {
            return null
        }
    }, [body])

    const requestBodyJsonError = useMemo(() => {
        const text = body.trim()
        if (!text) return null
        try {
            JSON.parse(text)
            return null
        } catch (err: unknown) {
            return getErrorMessage(err, 'Invalid JSON')
        }
    }, [body])

    // Parsed response body
    const parsedResponseBody = useMemo(() => {
        if (responseViewMode !== 'pretty') return null
        if (!response?.body) return null
        try {
            return JSON.parse(response.body)
        } catch {
            return null
        }
    }, [responseViewMode, response?.body])

    const handleAddHeader = () => {
        setHeaders([...headers, { key: '', value: '', id: generateId() }])
    }

    const handleRemoveHeader = (id: string) => {
        setHeaders(headers.filter((h) => h.id !== id))
    }

    const handleHeaderChange = (id: string, field: 'key' | 'value', val: string) => {
        setHeaders(headers.map((h) => (h.id === id ? { ...h, [field]: val } : h)))
    }

    const copiedTimerRef = useRef<number | null>(null)
    useEffect(() => () => {
        if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
    }, [])

    const copyToClipboard = async (text: string, field: string) => {
        if (!(await copyText(text))) {
            toast.error(t('log_detail.copy_failed'))
            return
        }
        setCopiedField(field)
        if (copiedTimerRef.current) window.clearTimeout(copiedTimerRef.current)
        copiedTimerRef.current = window.setTimeout(() => setCopiedField(null), 2000)
    }

    const formatRequestBody = () => {
        const text = body.trim()
        if (!text) return
        try {
            setBody(JSON.stringify(JSON.parse(text), null, 2))
            setRequestBodyViewMode('raw')
        } catch {
            setRequestBodyViewMode('raw')
        }
    }

    const handleSend = useCallback(async () => {
        if (!method) return
        if (targetMode === 'upstream' && !upstream) return
        if (targetMode === 'url' && !targetUrl.trim()) return

        setError(null)
        setResponse(null)
        setResponseViewMode('pretty')
        setSending(true)

        const headerMap: Record<string, string> = {}
        headers.forEach((h) => {
            if (h.key.trim()) headerMap[h.key.trim()] = h.value
        })

        const startTime = performance.now()
        try {
            const resp = await sendReplay({
                ...(targetMode === 'upstream'
                    ? { upstream, path }
                    : { target_url: targetUrl.trim() }),
                method,
                headers: headerMap,
                body,
            })
            setElapsed(Math.round(performance.now() - startTime))
            setResponse(resp)
        } catch (err: unknown) {
            setElapsed(Math.round(performance.now() - startTime))
            setError(getErrorMessage(err, '请求失败'))
        } finally {
            setSending(false)
        }
    }, [targetMode, upstream, targetUrl, method, path, headers, body])

    // Handle Ctrl+Enter to send
    useEffect(() => {
        const handler = (e: KeyboardEvent) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault()
                handleSend()
            }
        }
        window.addEventListener('keydown', handler)
        return () => window.removeEventListener('keydown', handler)
    }, [handleSend])

    return (
        <div className="mx-auto w-full max-w-7xl space-y-5 animate-fade-in">

            {/* Unified Address Bar:同一条工具栏上所有控件统一 h-8,与全站工具条一致 */}
            <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-muted/40 p-2">
                {/* Method Selector */}
                <Select value={method} onValueChange={setMethod}>
                    <SelectTrigger
                        aria-label={t('log_table.method')}
                        className="min-w-[92px] shrink-0 justify-between gap-1 border-border bg-background font-mono text-xs font-semibold"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {HTTP_METHODS.map((m) => (
                            <SelectItem key={m} value={m} className="font-mono text-xs font-medium">
                                {m}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <div className="flex shrink-0 items-center gap-1 rounded-md border border-border bg-background p-1">
                    {([
                        { value: 'upstream', label: t('playground.target_upstream') },
                        { value: 'url', label: t('playground.target_url') },
                    ] as const).map((option) => (
                        <button
                            key={option.value}
                            type="button"
                            onClick={() => setTargetMode(option.value)}
                            className={cn(
                                'h-6 rounded-sm px-2.5 text-xs font-medium transition-colors',
                                targetMode === option.value
                                    ? 'bg-secondary text-foreground'
                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                            )}
                        >
                            {option.label}
                        </button>
                    ))}
                </div>

                {targetMode === 'upstream' ? (
                    <>
                        {/* Upstream Selector */}
                        <Select value={upstream} onValueChange={setUpstream}>
                            <SelectTrigger
                                aria-label={t('playground.select_upstream')}
                                className={cn(
                                    'min-w-[120px] max-w-[220px] shrink-0 justify-between gap-1 border-input bg-background text-xs',
                                    selectedUpstreamMissing && 'border-warning/50 text-warning'
                                )}
                            >
                                <SelectValue placeholder={t('playground.select_upstream')}>
                                    {upstream || t('playground.select_upstream')}
                                </SelectValue>
                            </SelectTrigger>
                            <SelectContent>
                                {upstreams.map((u) => (
                                    <SelectItem key={u.name} value={u.name} className="text-xs">
                                        <span className="font-semibold">{u.name}</span>
                                        <span className="truncate text-muted-foreground">{u.target}</span>
                                    </SelectItem>
                                ))}
                                {upstreams.length === 0 && (
                                    <div className="px-2 py-1.5 text-xs italic text-muted-foreground">
                                        {t('playground.no_upstreams')}
                                    </div>
                                )}
                            </SelectContent>
                        </Select>

                        {/* Path Input */}
                        <input
                            type="text"
                            value={path}
                            onChange={(e) => setPath(e.target.value)}
                            placeholder="/v1/chat/completions"
                            className={cn('h-8 min-w-[220px] flex-1 rounded-md border border-input bg-background px-3 text-sm font-mono placeholder:text-muted-foreground/50 transition-all', FOCUS_RING)}
                        />
                    </>
                ) : (
                    <input
                        type="url"
                        value={targetUrl}
                        onChange={(e) => setTargetUrl(e.target.value)}
                        placeholder={t('playground.custom_url_placeholder')}
                        className={cn('h-8 min-w-[260px] flex-1 rounded-md border border-input bg-background px-3 text-sm font-mono placeholder:text-muted-foreground/50 transition-all', FOCUS_RING)}
                    />
                )}

                {/* Send Button */}
                <Button
                    onClick={handleSend}
                    disabled={
                        sending ||
                        (targetMode === 'upstream' ? !upstream || selectedUpstreamMissing : !targetUrl.trim())
                    }
                    className="shrink-0 gap-2 px-4"
                >
                    {sending ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                        <Send className="h-4 w-4" />
                    )}
                    <span className="hidden sm:inline">{t('playground.send')}</span>
                </Button>
            </div>

            {/* Request Config Tabs:ui/tabs 的 line 变体,与 Traces 页同一形态,不再手搓 border-b-2 页签 */}
            <Tabs value={activeTab} onValueChange={(value) => setActiveTab(value as RequestTab)} className="gap-0">
                <TabsList variant="line" className="w-full justify-start rounded-none border-b border-border pb-1">
                    <TabsTrigger value="body" className="flex-none px-3">
                        {t('playground.body')}
                    </TabsTrigger>
                    <TabsTrigger value="headers" className="flex-none gap-1.5 px-3">
                        {t('playground.headers')}
                        {headers.length > 0 && <Badge variant="secondary">{headers.length}</Badge>}
                    </TabsTrigger>
                </TabsList>

                <TabsContent value="body" className="space-y-2 pt-3">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                            <div className="flex items-center gap-2">
                                {body.trim() && (
                                    <Badge
                                        variant="outline"
                                        className={cn(
                                            'h-6 border-none px-2 text-xs font-medium',
                                            // JSON 非法会禁用发送,是错误不是警告
                                            requestBodyJsonError
                                                ? 'bg-danger/10 text-danger'
                                                : 'bg-success/10 text-success'
                                        )}
                                    >
                                        {requestBodyJsonError
                                            ? t('playground.json_invalid')
                                            : t('playground.json_valid')}
                                    </Badge>
                                )}
                                <Button
                                    type="button"
                                    variant="outline"
                                    size="sm"
                                    onClick={formatRequestBody}
                                    disabled={!body.trim() || !!requestBodyJsonError}
                                    className="h-7 gap-1.5 px-2.5 text-xs font-medium"
                                >
                                    <Braces className="h-3.5 w-3.5" />
                                    {t('playground.format_json')}
                                </Button>
                            </div>
                            <div className="flex items-center gap-1 rounded-md border border-border bg-background p-1">
                                {([
                                    { value: 'raw', label: t('log_detail.view_raw') },
                                    { value: 'pretty', label: t('log_detail.view_pretty') },
                                ] as const).map((option) => (
                                    <Button
                                        key={option.value}
                                        type="button"
                                        variant={requestBodyViewMode === option.value ? 'secondary' : 'ghost'}
                                        size="sm"
                                        onClick={() => setRequestBodyViewMode(option.value)}
                                        className="h-6 px-2 text-xs font-medium"
                                    >
                                        {option.label}
                                    </Button>
                                ))}
                            </div>
                        </div>
                        {requestBodyViewMode === 'raw' ? (
                            <textarea
                                value={body}
                                onChange={(e) => setBody(e.target.value)}
                                placeholder='{ "model": "gpt-4", "messages": [...] }'
                                className={cn(
                                    'w-full h-[260px] px-4 py-3 rounded-md bg-background border text-xs font-mono leading-relaxed placeholder:text-muted-foreground/50 resize-none custom-scrollbar transition-all',
                                    requestBodyJsonError ? 'border-danger' : 'border-input',
                                    requestBodyJsonError ? FOCUS_RING_DANGER : FOCUS_RING,
                                )}
                                spellCheck={false}
                                aria-invalid={requestBodyJsonError ? true : undefined}
                                aria-describedby={requestBodyJsonError ? jsonErrorId : undefined}
                            />
                        ) : (
                            <div className="h-[260px] overflow-auto rounded-md border border-input bg-background px-4 py-3 custom-scrollbar">
                                {body.trim() ? (
                                    <JsonViewer data={parsedRequestBody ?? body} />
                                ) : (
                                    <div className="flex h-full items-center justify-center text-xs italic text-muted-foreground/50">
                                        {t('playground.empty_state')}
                                    </div>
                                )}
                            </div>
                        )}

                        {/* 行内错误提示。原来解析错误只挂在 Badge 的原生 title 上,
                            要悬停约一秒才出现,等于没提示 —— 而它会禁用发送按钮 */}
                        {/* 不加 role="alert":那是 assertive 活动区域,每敲一个字符都会重新播报。
                            textarea 已用 aria-describedby 关联,聚焦时读屏器就会读到 */}
                        {requestBodyJsonError && (
                            <p id={jsonErrorId} className="flex items-start gap-1.5 text-xs text-danger">
                                <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                                <span className="min-w-0 break-words font-mono">{requestBodyJsonError}</span>
                            </p>
                        )}
                </TabsContent>

                <TabsContent value="headers" className="space-y-2 pt-3">
                        <div className="space-y-1.5 max-h-[240px] overflow-y-auto custom-scrollbar">
                            {headers.map((h) => (
                                <div key={h.id} className="flex items-center gap-2 group">
                                    <input
                                        type="text"
                                        value={h.key}
                                        onChange={(e) => handleHeaderChange(h.id, 'key', e.target.value)}
                                        placeholder="Header Name"
                                        className={cn('h-8 w-[35%] rounded-md border border-input bg-background px-3 text-xs font-mono font-medium placeholder:text-muted-foreground/50 transition-all sm:w-[30%]', FOCUS_RING)}
                                    />
                                    <input
                                        type="text"
                                        value={h.value}
                                        onChange={(e) => handleHeaderChange(h.id, 'value', e.target.value)}
                                        placeholder="Value"
                                        className={cn('h-8 flex-1 rounded-md border border-input bg-background px-3 text-xs font-mono placeholder:text-muted-foreground/50 transition-all', FOCUS_RING)}
                                    />
                                    <button
                                        onClick={() => handleRemoveHeader(h.id)}
                                        className="rounded-md p-1.5 text-muted-foreground/50 opacity-0 transition-all hover:bg-danger/10 hover:text-danger group-hover:opacity-100"
                                    >
                                        <Trash2 className="h-3.5 w-3.5" />
                                    </button>
                                </div>
                            ))}
                        </div>
                        <Button
                            variant="ghost"
                            size="sm"
                            onClick={handleAddHeader}
                            className="h-8 px-3 text-xs font-medium gap-1.5 text-muted-foreground/50 hover:text-foreground"
                        >
                            <Plus className="h-3 w-3" />
                            {t('playground.add_header')}
                        </Button>
                </TabsContent>
            </Tabs>

            {/* Response */}
            {(response || error || sending) && (
                <div className="rounded-lg border border-border bg-card overflow-hidden">
                    {/* Response Header */}
                    <div className="px-4 py-3 border-b border-border flex flex-wrap items-center gap-3">
                        <span className="text-xs font-semibold text-muted-foreground/70">
                            {t('playground.response')}
                        </span>
                        {sending && (
                            <div className="flex items-center gap-2 text-xs font-semibold text-primary">
                                <Loader2 className="h-3 w-3 animate-spin" />
                                {t('common.loading')}
                            </div>
                        )}
                        {response && (
                            <>
                                <Badge
                                    variant="outline"
                                    className={cn(
                                        'font-semibold text-xs border-none',
                                        getStatusColor(response.status_code)
                                    )}
                                >
                                    {response.status_code}
                                </Badge>
                                {elapsed !== null && (
                                    <span className="text-xs font-mono text-muted-foreground/50">
                                        {elapsed}ms
                                    </span>
                                )}
                                {response.body && (
                                    <span className="text-xs font-mono text-muted-foreground/50">
                                        {formatSize(response.body.length)}
                                        {response.truncated && (
                                            <span className="ml-1 text-warning font-semibold">
                                                (TRUNCATED)
                                            </span>
                                        )}
                                    </span>
                                )}
                                {response.body_decoded && (
                                    <Badge
                                        variant="outline"
                                        className="h-5 border-info/30 bg-info/5 px-1.5 text-xs font-medium text-info"
                                    >
                                        {t('playground.body_decoded', {
                                            encoding: (response.body_decoded_from || 'gzip').toUpperCase(),
                                        })}
                                    </Badge>
                                )}
                                {response.body && (
                                    <ViewToggle value={responseViewMode} onChange={setResponseViewMode} />
                                )}
                                <div className="ml-auto">
                                    <Button
                                        variant="ghost"
                                        size="icon"
                                        className="h-7 w-7"
                                        onClick={() => copyToClipboard(response.body, 'resp')}
                                    >
                                        {copiedField === 'resp' ? (
                                            <Check className="h-3.5 w-3.5 text-success" />
                                        ) : (
                                            <Copy className="h-3.5 w-3.5 text-muted-foreground/50" />
                                        )}
                                    </Button>
                                </div>
                            </>
                        )}
                    </div>

                    {/* Error */}
                    {error && (
                        <div className="p-4 bg-danger/5 border-b border-danger/10">
                            <pre className="text-xs text-danger font-mono whitespace-pre-wrap">{error}</pre>
                        </div>
                    )}

                    {/* Response Headers */}
                    {response?.headers && Object.keys(response.headers).length > 0 && (
                        <details className="group">
                            <summary className="px-4 py-2 cursor-pointer text-xs font-semibold text-muted-foreground/70 hover:text-muted-foreground transition-colors select-none">
                                {t('playground.response_headers')} ({Object.keys(response.headers).length})
                            </summary>
                            <div className="px-4 pb-3 space-y-1 font-mono text-xs">
                                {Object.entries(response.headers).map(([k, vv]) => (
                                    <div key={k} className="flex flex-col sm:flex-row sm:gap-2">
                                        <span className="text-success/70 shrink-0 font-semibold">{k}:</span>
                                        <div className="flex flex-col">
                                            {vv.map((v, i) => (
                                                <span key={i} className="text-foreground/70 break-all">{v}{i < vv.length - 1 ? ';' : ''}</span>
                                            ))}
                                        </div>
                                    </div>
                                ))}
                            </div>
                            <Separator className="bg-border/20" />
                        </details>
                    )}

                    {/* Response Body */}
                    {response?.body && (
                        <div className="p-4 max-h-[600px] overflow-auto custom-scrollbar">
                            {responseViewMode === 'raw' ? (
                                <RawBodyViewer text={response.body} />
                            ) : (
                                <JsonViewer data={parsedResponseBody ?? response.body} />
                            )}
                        </div>
                    )}

                    {/* Empty response */}
                    {response && !response.body && !error && (
                        <div className="p-8 text-center text-xs text-muted-foreground/50 italic">
                            {t('playground.empty_response')}
                        </div>
                    )}
                </div>
            )}


        </div>
    )
}

