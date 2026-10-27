import { CaptureRecordingDialog, TraceCapturePanel } from '@/components/TraceCapturePanel'
import { useState, useEffect } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Network, AlertCircle, Copy, Search, X, ChevronLeft, ChevronRight, Plus, Loader2 } from 'lucide-react'
import { copyText } from '@/lib/clipboard'
import { fetchTraces, fetchUpstreams, type TraceSummary, type Upstream } from '@/lib/api'
import { cn, formatLatency } from '@/lib/utils'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TracePinButton } from '@/components/TraceTaskManager'
import { TaskActions, TaskBatchActions, TaskSelectionToggle } from '@/components/TaskManagement'
import { TraceFilters } from '@/components/TraceFilters'
import { countTraceFilters, defaultTraceFilters, traceListQuery, type TraceListFilters } from '@/lib/trace-filters'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { correlationSettingsPath, useCorrelationStatus } from '@/lib/trace-capture'
import { toast } from 'sonner'

export function Traces() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const tab = searchParams.get('tab') === 'captures' ? 'captures' : 'traces'
  const { status, error: statusError, refresh: refreshStatus } = useCorrelationStatus()
  const [recordingOpen, setRecordingOpen] = useState(false)
  const [captureRevision, setCaptureRevision] = useState(0)
  const [captureCreation, setCaptureCreation] = useState(0)
  const activeCaptures = status?.active_captures.length ?? 0

  function selectTab(value: string) {
    exitSelection()
    setSearchParams(previous => {
      const next = new URLSearchParams(previous)
      if (value === 'captures') next.set('tab', 'captures')
      else next.delete('tab')
      return next
    }, { replace: true })
  }

  function captureChanged() {
    setCaptureRevision(previous => previous + 1)
    void refreshStatus()
  }

  const [page, setPage] = useState<{ scope: string; traces: TraceSummary[]; total: number; failed?: boolean } | null>(null)
  const [upstreams, setUpstreams] = useState<Upstream[]>([])
  const [filter, setFilter] = useState<TraceListFilters>(() => defaultTraceFilters(searchParams.get('trace_id') || ''))
  const [draftQuery, setDraftQuery] = useState(filter.query)
  const [composing, setComposing] = useState(false)
  const [reload, setReload] = useState(0)
  const [selection, setSelection] = useState<{ scope: string; ids: string[]; active: boolean }>({ scope: '', ids: [], active: false })
  const selectionScope = JSON.stringify({ filter, tab, reload })
  const currentPage = page?.scope === selectionScope ? page : null
  const traces = currentPage?.traces ?? []
  const total = currentPage?.total ?? 0
  const loading = tab === 'traces' && !currentPage
  const selecting = selection.scope === selectionScope && selection.active
  const selected = selecting ? selection.ids : []
  const selectedItems = traces.filter(trace => selected.includes(trace.trace_id)).map(trace => ({ id: trace.trace_id }))
  function exitSelection() { setSelection({ scope: '', ids: [], active: false }) }
  function managementChanged() { exitSelection(); setReload(previous => previous + 1) }
  function toggleSelected(id: string, checked: boolean) {
    setSelection({ scope: selectionScope, active: true, ids: checked ? [...selected.filter(value => value !== id), id] : selected.filter(value => value !== id) })
  }

  useEffect(() => {
    if (tab !== 'traces') return
    const controller = new AbortController()

    async function load() {
      try {
        const res = await fetchTraces(traceListQuery(filter), controller.signal)
        if (controller.signal.aborted) return
        if (!res.traces?.length && (filter.offset ?? 0) > 0) {
          setFilter(previous => ({ ...previous, offset: Math.max(0, Math.ceil(res.total / (previous.limit || 20)) - 1) * (previous.limit || 20) }))
          return
        }
        setPage({ scope: selectionScope, traces: res.traces || [], total: res.total })
      } catch {
        if (!controller.signal.aborted) {
          setPage({ scope: selectionScope, traces: [], total: 0, failed: true })
          toast.error(i18n.t('traces.load_failed'))
        }
      }
    }

    void load()

    return () => {
      controller.abort()
    }
  }, [filter, i18n, tab, reload, selectionScope])

  useEffect(() => {
    if (composing || draftQuery.trim() === filter.query) return
    const timer = window.setTimeout(() => setFilter(previous => ({ ...previous, query: draftQuery.trim(), offset: 0 })), 300)
    return () => window.clearTimeout(timer)
  }, [draftQuery, filter.query, composing])

  useEffect(() => {
    fetchUpstreams().then(setUpstreams).catch(() => {})
  }, [])

  const handleSearch = () => {
    exitSelection()
    if (draftQuery.trim() === filter.query) setReload(previous => previous + 1)
    else setFilter(previous => ({ ...previous, query: draftQuery.trim(), offset: 0 }))
  }

  const narrowed = !!filter.query || countTraceFilters({ ...filter, archived: false }) > 0

  const maxLatency = traces.reduce((max, t) => Math.max(max, t.total_latency_ms), 1)

  // 只给请求头,不给完整 curl:代理地址取决于上游名与端口,请求路径又因厂商而异
  // (/v1/messages 与 /v1/chat/completions),拼出来的完整命令大概率是错的。
  // 请求头是唯一在任何场景下都正确、也正是用户不知道的那部分。
  const traceExample = '-H "X-PrismCat-Trace-ID: my-task-001"'

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className={cn('inline-flex items-center gap-1.5 text-xs', status?.enabled ? 'text-success' : 'text-muted-foreground')}>
            <span className={cn('size-1.5 rounded-full', status?.enabled ? 'bg-success' : 'bg-muted-foreground/50')} />
            {t(status ? status.enabled ? 'correlation.status_enabled' : 'correlation.status_disabled' : statusError ? 'correlation.status_unavailable' : 'common.loading')}
          </span>
          <Link className="text-xs text-primary hover:underline" to={correlationSettingsPath}>{t('correlation.settings_link')}</Link>
        </div>
        <Button size="sm" onClick={() => setRecordingOpen(true)}><Plus className="size-4" />{t('correlation.new_capture')}</Button>
      </div>
      <Tabs value={tab} onValueChange={selectTab} className="gap-4">
        <TabsList variant="line" className="w-full justify-start rounded-none border-b border-border pb-1">
          <TabsTrigger value="traces" className="flex-none px-3">{t('nav.traces')}</TabsTrigger>
          <TabsTrigger value="captures" className="flex-none px-3">{t('correlation.capture_title')}{activeCaptures > 0 && <span className="ml-1 inline-flex items-center gap-1 text-xs text-danger"><span className="size-1.5 rounded-full bg-danger" />{t('correlation.active_captures', { count: activeCaptures })}</span>}</TabsTrigger>
        </TabsList>
        <TabsContent value="traces" className="space-y-4">
      {/* Filter bar */}
      <div className="space-y-2 rounded-lg border border-border bg-card p-2 md:p-3">
        <div className="flex items-center gap-2">
          <div className="relative min-w-0 flex-1">
            <Search aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              type="search"
              aria-label={t('trace_manager.search_hint')}
              placeholder={t('trace_manager.search_hint')}
              value={draftQuery}
              onChange={event => { setDraftQuery(event.target.value); exitSelection() }}
              onCompositionStart={() => setComposing(true)}
              onCompositionEnd={event => { setComposing(false); setDraftQuery(event.currentTarget.value) }}
              onKeyDown={event => { if (event.key === 'Enter' && !composing && !event.nativeEvent.isComposing) { event.preventDefault(); handleSearch() } }}
              className="h-9 pl-9 pr-8 text-sm [&::-webkit-search-cancel-button]:appearance-none"
            />
            {draftQuery && <Button type="button" size="icon-xs" variant="ghost" className="absolute right-1.5 top-1/2 -translate-y-1/2 text-muted-foreground" aria-label={t('trace_filters.clear_search')} onClick={() => { exitSelection(); setDraftQuery(''); setFilter(previous => ({ ...previous, query: '', offset: 0 })) }}><X className="size-3.5" /></Button>}
          </div>
          <TraceFilters value={filter} upstreams={upstreams} onChange={next => { exitSelection(); setFilter({ ...next, query: composing ? filter.query : draftQuery.trim(), offset: 0 }) }} />
        </div>
        {/* Pagination */}
        <div className="flex items-center justify-between text-xs text-muted-foreground px-1">
          <div className="flex flex-wrap items-center gap-3">
            <span>{t('traces.total_count', { count: total })}</span>
            {selecting && traces.length > 0 && <label className="inline-flex items-center gap-1.5"><input type="checkbox" checked={selectedItems.length === traces.length} onChange={event => setSelection({ scope: selectionScope, active: true, ids: event.target.checked ? traces.map(trace => trace.trace_id) : [] })} />{t('task_management.select_page')}</label>}
            {(traces.length > 0 || selecting) && <TaskSelectionToggle active={selecting} disabled={loading} onToggle={() => selecting ? exitSelection() : setSelection({ scope: selectionScope, ids: [], active: true })} />}
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost" size="sm" className="h-7 w-7 p-0"
              disabled={loading || !filter.offset}
              onClick={() => { exitSelection(); setFilter(prev => ({ ...prev, offset: Math.max(0, (prev.offset || 0) - (prev.limit || 20)) })) }}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
            <span>
              {Math.floor((filter.offset || 0) / (filter.limit || 20)) + 1} / {Math.max(1, Math.ceil(total / (filter.limit || 20)))}
            </span>
            <Button
              variant="ghost" size="sm" className="h-7 w-7 p-0"
              disabled={loading || (filter.offset || 0) + (filter.limit || 20) >= total}
              onClick={() => { exitSelection(); setFilter(prev => ({ ...prev, offset: (prev.offset || 0) + (prev.limit || 20) })) }}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      <TaskBatchActions kind="trace" items={selectedItems} archived={filter.archived} onChanged={managementChanged} />

      {/* Traces table */}
      {loading ? (
        <div role="status" className="flex items-center justify-center gap-2 py-20 text-sm text-muted-foreground"><Loader2 className="size-5 animate-spin" />{t('common.loading')}</div>
      ) : currentPage?.failed ? (
        <div role="alert" className="flex flex-col items-center justify-center gap-3 py-20 text-sm text-muted-foreground"><AlertCircle className="size-6" /><p>{t('traces.load_failed')}</p><Button variant="outline" size="sm" onClick={() => setReload(previous => previous + 1)}>{t('common.refresh')}</Button></div>
      ) : traces.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20">
          <Network className="mb-4 h-10 w-10 text-muted-foreground/40" />
          <p className="text-sm font-medium text-foreground">{t(narrowed ? 'trace_filters.no_matches' : filter.archived ? 'task_management.empty_archived' : 'traces.no_traces')}</p>
          {narrowed && <p className="mt-1 text-xs text-muted-foreground">{t('trace_filters.no_matches_hint')}</p>}
          {!narrowed && !filter.archived && <p className="mt-1 max-w-md text-center text-xs text-muted-foreground">
            {t('traces.no_traces_hint')}
          </p>}

          {/* 空状态负责教会用户怎么触发这个功能 —— 不知道要发这个请求头的人
              永远看不到这页有内容,会以为功能坏了 */}
          {!narrowed && !filter.archived && <div className="mt-6 w-full max-w-xl">
            <p className="mb-2 text-xs text-muted-foreground">{t('traces.no_traces_example')}</p>
            <div className="flex items-start gap-2 rounded-lg border border-border bg-muted p-3">
              <pre className="min-w-0 flex-1 overflow-x-auto font-mono text-xs leading-5 text-foreground">
                {traceExample}
              </pre>
              <Button
                size="icon"
                variant="outline"
                className="h-7 w-7 shrink-0 border border-input bg-background text-muted-foreground hover:bg-accent hover:text-foreground"
                aria-label={t('json_viewer.copy')}
                onClick={async () => {
                  if (await copyText(traceExample)) toast.success(t('log_detail.copy_success'))
                  else toast.error(t('log_detail.copy_failed'))
                }}
              >
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>}
        </div>
      ) : (
        <>
          {/* Desktop table */}
          <div className="hidden md:block rounded-lg overflow-hidden bg-card border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b bg-muted/30">
                  {selecting && <th className="w-10 px-3" aria-label={t('task_management.selection')} />}
                  <th className="px-4 py-3 text-left font-medium text-xs text-muted-foreground">{t('traces.trace_id')}</th>
                  <th className="px-3 py-3 text-center font-medium text-xs text-muted-foreground">{t('traces.requests')}</th>
                  <th className="px-3 py-3 text-left font-medium text-xs text-muted-foreground">{t('traces.upstreams')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.duration')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.tokens')}</th>
                  <th className="px-3 py-3 text-center font-medium text-xs text-muted-foreground">{t('traces.errors')}</th>
                  <th className="px-3 py-3 text-right font-medium text-xs text-muted-foreground">{t('traces.time')}</th>
                  {!selecting && <th className="w-24 px-2" aria-label={t('task_management.actions')} />}
                </tr>
              </thead>
              <tbody>
                {traces.map(trace => (
                  <tr
                    key={trace.trace_id}
                    onClick={() => selecting ? toggleSelected(trace.trace_id, !selected.includes(trace.trace_id)) : navigate(`/traces/${encodeURIComponent(trace.trace_id)}`)}
                    className={cn('group/trace border-b transition-colors hover:bg-muted/40 cursor-pointer', selecting && selected.includes(trace.trace_id) && 'bg-primary/5')}
                  >
                    {selecting && <td className="px-3" onClick={event => event.stopPropagation()}><input type="checkbox" aria-label={t('task_management.select_item', { name: trace.metadata?.name || trace.trace_id })} checked={selected.includes(trace.trace_id)} onChange={event => toggleSelected(trace.trace_id, event.target.checked)} /></td>}
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Network className="h-3.5 w-3.5 shrink-0 text-info" />
                        <div className="min-w-0">{selecting ? <p className="max-w-[240px] truncate font-medium">{trace.metadata?.name || t('trace_manager.unnamed')}</p> : <Link to={`/traces/${encodeURIComponent(trace.trace_id)}`} onClick={event => event.stopPropagation()} className="block max-w-[240px] truncate rounded-sm font-medium transition-colors group-hover/trace:text-primary focus-visible:outline-2 focus-visible:outline-primary">{trace.metadata?.name || t('trace_manager.unnamed')}</Link>}<span className="font-mono text-muted-foreground truncate block max-w-[200px]" title={trace.trace_id}>{trace.trace_id}</span></div>
                        <CopyTraceIdButton traceId={trace.trace_id} />
                        <TracePinButton trace={trace} onChange={metadata => setPage(previous => previous?.scope === selectionScope ? { ...previous, traces: previous.traces.map(item => item.trace_id === trace.trace_id ? { ...item, metadata } : item) } : previous)} />
 <span className="flex gap-1 text-xs text-muted-foreground">{trace.sources?.map(source => <span key={source} className="rounded border border-border px-1">{t(`correlation.sources.${source}`)}</span>)}</span>
                      </div>
                      {/* Latency bar */}
                      <div className="mt-1.5 h-1.5 w-full rounded-full bg-muted/50">
                        <div
                          className={cn(
                            'h-1.5 rounded-full transition-all duration-500',
                            trace.error_count > 0 ? 'bg-danger' :
                            trace.total_latency_ms > 5000 ? 'bg-warning' :
                            'bg-info'
                          )}
                          style={{ width: `${Math.max(4, (trace.total_latency_ms / maxLatency) * 100)}%` }}
                        />
                      </div>
                    </td>
                    <td className="px-3 py-3 text-center">
                      <span className="font-mono font-semibold text-foreground/80">{trace.request_count}</span>
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex flex-wrap gap-1">
                        {(trace.upstreams || []).map(u => (
                          <span key={u} className="inline-flex items-center h-[18px] px-1.5 rounded-md text-xs font-semibold tracking-tight bg-muted text-muted-foreground">
                            {u}
                          </span>
                        ))}
                      </div>
                    </td>
                    <td className="px-3 py-3 text-right font-mono text-muted-foreground">
                      {formatLatency(trace.total_latency_ms)}
                    </td>
                    <td className="px-3 py-3 text-right font-mono text-muted-foreground">
                      {typeof trace.usage_total_tokens === 'number' ? trace.usage_total_tokens.toLocaleString() : '-'}
                    </td>
                    <td className="px-3 py-3 text-center">
                      <span className={cn('font-mono font-semibold', trace.error_count > 0 ? 'text-danger' : 'text-muted-foreground/60')}>
                        {trace.error_count}
                      </span>
                    </td>
                    <td className="px-3 py-3 text-right text-muted-foreground">
                      {new Date(trace.last_time).toLocaleTimeString()}
                    </td>
                    {!selecting && <td className="px-2 text-right" onClick={event => event.stopPropagation()}><TaskActions kind="trace" items={[{ id: trace.trace_id }]} name={trace.metadata?.name || trace.trace_id} archived={!!trace.metadata?.archived} onChanged={managementChanged} /></td>}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Mobile cards */}
          <div className="md:hidden space-y-2">
            {traces.map(trace => (
              <div
                key={trace.trace_id}
                className={cn('relative w-full rounded-lg p-4 text-left transition-all active:scale-[0.99] bg-card hover:bg-card/40 border border-border', selecting && selected.includes(trace.trace_id) && 'border-primary/50 bg-primary/5')}
              >
                <div className="flex items-center gap-2 mb-2">
                  {selecting && <input type="checkbox" className="relative z-10" aria-label={t('task_management.select_item', { name: trace.metadata?.name || trace.trace_id })} checked={selected.includes(trace.trace_id)} onChange={event => toggleSelected(trace.trace_id, event.target.checked)} />}
                  <Network className="h-3.5 w-3.5 text-info shrink-0" />
                  {selecting ? <button type="button" aria-pressed={selected.includes(trace.trace_id)} onClick={() => toggleSelected(trace.trace_id, !selected.includes(trace.trace_id))} className="min-w-0 flex-1 truncate text-left text-xs text-foreground/90 after:absolute after:inset-0 after:rounded-lg">{trace.metadata?.name || trace.trace_id}</button> : <Link to={`/traces/${encodeURIComponent(trace.trace_id)}`} className="min-w-0 flex-1 truncate text-xs text-foreground/90 after:absolute after:inset-0 after:rounded-lg" title={trace.trace_id}>{trace.metadata?.name || trace.trace_id}</Link>}
                  <span className="relative z-10"><CopyTraceIdButton traceId={trace.trace_id} /></span>
                </div>
                <div className="flex items-center gap-2 mb-2 flex-wrap">
                  {(trace.upstreams || []).map(u => (
                    <span key={u} className="text-xs font-semibold tracking-tight bg-muted text-muted-foreground px-1.5 py-0.5 rounded-md">{u}</span>
                  ))}
                  <span className="text-xs font-medium bg-muted text-muted-foreground px-1.5 py-0.5 rounded">{trace.request_count} reqs</span>
                  {trace.error_count > 0 && (
                    <span className="text-xs font-medium bg-danger/10 text-danger px-1.5 py-0.5 rounded flex items-center gap-0.5">
                      <AlertCircle className="h-2.5 w-2.5" />{trace.error_count}
                    </span>
                  )}
                  <span className="text-xs font-mono text-muted-foreground ml-auto">{formatLatency(trace.total_latency_ms)}</span>
                </div>
                <div className="mb-2 flex min-h-8 items-center justify-between gap-2">
                  <span className="text-xs font-mono text-muted-foreground/70">{t('traces.tokens')}: {typeof trace.usage_total_tokens === 'number' ? trace.usage_total_tokens.toLocaleString() : '-'}</span>
                  {!selecting && <span className="relative z-10"><TaskActions kind="trace" items={[{ id: trace.trace_id }]} name={trace.metadata?.name || trace.trace_id} archived={!!trace.metadata?.archived} onChanged={managementChanged} /></span>}
                </div>
                <div className="h-1.5 w-full rounded-full bg-muted/50">
                  <div
                    className={cn(
                      'h-1.5 rounded-full',
                      trace.error_count > 0 ? 'bg-danger/60' : 'bg-info/60'
                    )}
                    style={{ width: `${Math.max(4, (trace.total_latency_ms / maxLatency) * 100)}%` }}
                  />
                </div>
                <div className="flex items-center justify-between mt-2 text-xs text-muted-foreground/60">
                  <span>{new Date(trace.first_time).toLocaleString()}</span>
                  <span>{new Date(trace.last_time).toLocaleTimeString()}</span>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
        </TabsContent>
        <TabsContent value="captures">
          <TraceCapturePanel key={captureCreation} revision={captureRevision} recording={activeCaptures > 0} onChanged={captureChanged} />
        </TabsContent>
      </Tabs>
      {recordingOpen && <CaptureRecordingDialog upstreams={upstreams} status={status} onClose={() => setRecordingOpen(false)} onStarted={() => { setCaptureCreation(previous => previous + 1); captureChanged(); selectTab('captures') }} />}
    </div>
  )
}
