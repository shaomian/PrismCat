import { useState, useEffect, useMemo, useCallback, useRef, startTransition } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import {
  Network, ArrowLeft, Clock, AlertCircle, Layers, Activity,
  ChevronRight, MessagesSquare, ListTree, GitBranch, FileDown, Sparkles,
} from 'lucide-react'
import { fetchTraceDetail, fetchTraceTimeline, fetchTraceTimelineEntry, fetchTraceDiagram, fetchLog, traceExportUrl, type TraceDetail as TraceDetailType, type TraceTimeline, type RequestLog, type TraceDiagram as TraceDiagramData } from '@/lib/api'
import { cn, formatLatency, formatDate, getStatusColor, METHOD_CLASS } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TraceTaskManager } from '@/components/TraceTaskManager'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from '@/components/ui/sheet'
import { LogDetail } from '@/components/LogDetail'
import { TraceTimelineView } from '@/components/TraceTimeline'
import { TraceDiagram } from '@/components/TraceDiagram'
import { AnalysisChat } from '@/components/AnalysisChat'
import { toast } from 'sonner'

export function TraceDetail() {
  const { t, i18n } = useTranslation()
  const { traceId } = useParams<{ traceId: string }>()
  const navigate = useNavigate()

  const [detail, setDetail] = useState<TraceDetailType | null>(null)
  const [timeline, setTimeline] = useState<TraceTimeline | null>(null)
  const [diagram, setDiagram] = useState<TraceDiagramData | null>(null)
  const [diagramView, setDiagramView] = useState<'sequence' | 'dependencies'>('sequence')
  const [loading, setLoading] = useState(true)
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null)
  const [analysisOpen, setAnalysisOpen] = useState(false)
  const [loadingFullLogIds, setLoadingFullLogIds] = useState<Set<string>>(new Set())

  const loadDetail = useCallback(async () => {
    if (!traceId) return
    setLoading(true)
    try {
      const res = await fetchTraceDetail(traceId)
      setDetail(res)
    } catch {
      toast.error(i18n.t('traces.load_detail_failed'))
    } finally {
      setLoading(false)
    }
  }, [traceId, i18n])

  const loadTimeline = useCallback(async () => {
    if (!traceId) return
    try {
      const res = await fetchTraceTimeline(traceId)
      setTimeline(res)
    } catch {
      toast.error(i18n.t('timeline.load_failed'))
    }
  }, [traceId, i18n])

  // 按需加载单个请求的完整（未截断）事件，原位替换列表中的对应条目
  const handleLoadFullEvents = useCallback(async (logId: string) => {
    if (!traceId || loadingFullLogIds.has(logId)) return
    setLoadingFullLogIds(prev => new Set(prev).add(logId))
    try {
      const entry = await fetchTraceTimelineEntry(traceId, logId)
      setTimeline(prev => prev ? {
        ...prev,
        requests: prev.requests.map(req => (req.log_id === logId ? entry : req)),
      } : prev)
    } catch {
      toast.error(i18n.t('timeline.full_load_failed'))
    } finally {
      setLoadingFullLogIds(prev => {
        const next = new Set(prev)
        next.delete(logId)
        return next
      })
    }
  }, [traceId, loadingFullLogIds, i18n])

  useEffect(() => {
    loadDetail()
    loadTimeline()
  }, [loadDetail, loadTimeline])

  useEffect(() => {
    if (!traceId) return
    let cancelled = false
    void fetchTraceDiagram(traceId, diagramView).then(value => {
      if (!cancelled) setDiagram(value)
    }).catch(() => {
      if (!cancelled) toast.error(i18n.t('timeline.diagram.load_failed'))
    })
    return () => { cancelled = true }
  }, [traceId, diagramView, i18n])

  const requests = useMemo(() => detail?.requests ?? [], [detail?.requests])
  const summary = detail?.summary

  const maxLatency = useMemo(
    () => requests.reduce((max, r) => Math.max(max, r.latency_ms), 1),
    [requests],
  )

  const selectedLogIndex = useMemo(() => {
    if (!selectedLog) return -1
    return requests.findIndex(req => req.id === selectedLog.id)
  }, [requests, selectedLog])

  // 列表行是瘦身数据（详情接口不再携带 body 全文，重链可达数十 MB）。
  // 打开/切换日志时先用元数据即时展示 Sheet，同时按需拉取该条完整日志。
  // 序号守卫：快速点开/切上一条下一条时丢弃过期响应。
  const selectSeq = useRef(0)
  const [selectedLogLoading, setSelectedLogLoading] = useState(false)
  const openLog = useCallback(async (log: RequestLog) => {
    const seq = ++selectSeq.current
    setSelectedLog(log)
    setSelectedLogLoading(false)
    // body 已就位（fetchLog 回填或注解更新写回的完整对象）则无需再拉
    if (log.request_body !== undefined || log.response_body !== undefined) return
    setSelectedLogLoading(true)
    try {
      const full = await fetchLog(log.id)
      if (selectSeq.current !== seq) return
      startTransition(() => {
        setSelectedLog(full)
      })
    } catch (err) {
      if (selectSeq.current !== seq) return
      // 保留元数据展示（头部/状态等仍可见），body 区块维持加载占位
      toast.error((err as Error).message)
    } finally {
      if (selectSeq.current === seq) {
        setSelectedLogLoading(false)
      }
    }
  }, [])

  const handleCloseLog = useCallback(() => {
    selectSeq.current++
    setSelectedLog(null)
    setSelectedLogLoading(false)
  }, [])

  const handleNavigateLog = useCallback((direction: 'previous' | 'next') => {
    if (selectedLogIndex < 0) return
    const nextIndex = direction === 'previous' ? selectedLogIndex - 1 : selectedLogIndex + 1
    const nextLog = requests[nextIndex]
    if (!nextLog) return
    void openLog(nextLog)
  }, [requests, selectedLogIndex, openLog])

  const traceSpanMs = useMemo(() => {
    if (!summary) return 1
    return Math.max(summary.last_time - summary.first_time, 1)
  }, [summary])

  const handleOpenLog = useCallback((logId: string) => {
    const log = requests.find(r => r.id === logId)
    if (log) void openLog(log)
  }, [requests, openLog])

  if (loading) {
    return (
      <div className="flex min-h-[40vh] flex-col items-center justify-center gap-4">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        <div className="text-sm font-medium text-muted-foreground">{t('common.loading')}</div>
      </div>
    )
  }

  if (!detail || !summary) {
    return (
      <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
        <Network className="h-12 w-12 mb-4 opacity-30" />
        <p className="text-sm font-medium">{t('traces.not_found')}</p>
        <Button variant="ghost" size="sm" className="mt-4 gap-1.5" onClick={() => navigate('/traces')}>
          <ArrowLeft className="h-3.5 w-3.5" />
          {t('traces.back_to_list')}
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Breadcrumb + back */}
      <div className="flex items-center gap-2 text-sm">
        <Button variant="ghost" size="sm" className="h-8 gap-1.5 text-muted-foreground" onClick={() => navigate('/traces')}>
          <ArrowLeft className="h-3.5 w-3.5" />
          {t('traces.title')}
        </Button>
        <ChevronRight className="h-3.5 w-3.5 text-muted-foreground/50" />
        <span className="font-mono text-xs text-foreground/80 truncate max-w-[300px]" title={traceId}>{traceId}</span>
        {traceId && <CopyTraceIdButton traceId={traceId} />}
      </div>

      {/* Summary strip */}
      {traceId && <TraceTaskManager traceId={traceId} />}
      <div className="grid grid-cols-2 md:grid-cols-5 gap-3">
        <SummaryCard
          icon={Layers}
          label={t('traces.requests')}
          value={String(summary.request_count)}
          accent="text-info"
        />
        <SummaryCard
          icon={Clock}
          label={t('traces.duration')}
          value={formatLatency(summary.total_latency_ms)}
          accent="text-info"
        />
        <SummaryCard
          icon={AlertCircle}
          label={t('traces.errors')}
          value={String(summary.error_count)}
          accent={summary.error_count > 0 ? 'text-danger' : 'text-muted-foreground'}
        />
        <SummaryCard
          icon={Activity}
          label={t('traces.upstreams')}
          value={String(summary.upstreams?.length ?? 0)}
          accent="text-info"
        />
        <SummaryCard
          icon={Activity}
          label={t('traces.tokens')}
          value={typeof summary.usage_total_tokens === 'number' ? summary.usage_total_tokens.toLocaleString() : '-'}
          accent="text-success"
        />
      </div>

      {/* Upstream badges + 导出 */}
      <div className="flex flex-wrap items-center gap-1.5">
        {summary.upstreams?.map(u => (
          <span key={u} className="inline-flex items-center h-[22px] px-2 rounded text-xs font-semibold tracking-tight bg-primary/10 text-primary">
            {u}
          </span>
        ))}
        {traceId && (
          <div className="ml-auto flex items-center gap-1.5">
            <Button variant="default" size="sm" className="gap-1.5 h-7 text-xs" onClick={() => setAnalysisOpen(true)}>
              <Sparkles className="h-3 w-3" />
              {t('analysis.open_button')}
            </Button>
            <a href={traceExportUrl(traceId, 'events')} download>
              <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs">
                <FileDown className="h-3 w-3" />
                {t('timeline.export.events')}
              </Button>
            </a>
            <a href={traceExportUrl(traceId, 'raw')} download>
              <Button variant="outline" size="sm" className="gap-1.5 h-7 text-xs">
                <FileDown className="h-3 w-3" />
                {t('timeline.export.raw')}
              </Button>
            </a>
          </div>
        )}
      </div>

      {/* 视图切换：对话时间线 / 请求列表 */}
      <Tabs defaultValue="conversation">
        <TabsList>
          <TabsTrigger value="conversation" className="gap-1.5">
            <MessagesSquare className="h-3.5 w-3.5" />
            {t('timeline.tab_conversation')}
          </TabsTrigger>
          <TabsTrigger value="diagram" className="gap-1.5">
            <GitBranch className="h-3.5 w-3.5" />
            {t('timeline.tab_diagram')}
          </TabsTrigger>
          <TabsTrigger value="requests" className="gap-1.5">
            <ListTree className="h-3.5 w-3.5" />
            {t('timeline.tab_requests')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="conversation" className="pt-4">
          {!timeline ? (
            <div className="flex min-h-[20vh] items-center justify-center">
              <div className="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            </div>
          ) : (
            <TraceTimelineView
              requests={timeline.requests}
              onOpenLog={handleOpenLog}
              onLoadFull={handleLoadFullEvents}
              loadingFullLogIds={loadingFullLogIds}
            />
          )}
        </TabsContent>

        <TabsContent value="diagram" className="pt-4">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            {(['sequence', 'dependencies'] as const).map(view => (
              <Button key={view} size="sm" variant={diagramView === view ? 'default' : 'outline'} onClick={() => { setDiagram(null); setDiagramView(view) }} disabled={diagramView === view}>
                {t(`correlation.diagram_${view}`)}
              </Button>
            ))}
          </div>
          {diagramView === 'dependencies' && <p className="mb-3 text-xs text-muted-foreground">{t('correlation.diagram_hint')}</p>}
          {!diagram ? (
            <div className="flex min-h-[20vh] items-center justify-center">
              <div className="h-5 w-5 animate-spin rounded-full border-2 border-primary border-t-transparent" />
            </div>
          ) : (
            <TraceDiagram code={diagram.mermaid} labels={diagram.labels} onOpenLog={handleOpenLog} />
          )}
        </TabsContent>

        <TabsContent value="requests" className="pt-4">
      {/* Timeline */}
      <div className="space-y-0">
        {requests.map((req, idx) => {
          const isLast = idx === requests.length - 1
          const offsetMs = new Date(req.created_at).getTime() - summary.first_time
          const offsetPct = Math.min(100, (offsetMs / traceSpanMs) * 100)

          return (
            <div key={req.id} className="flex gap-3 group">
              {/* Timeline connector */}
              <div className="flex flex-col items-center w-8 shrink-0">
                <div
                  className={cn(
                    'h-3 w-3 rounded-full border-2 mt-4 z-10 transition-colors',
                    req.error
                      ? 'border-danger bg-danger/20'
                      : req.status_code >= 400
                        ? 'border-warning bg-warning/20'
                        : 'border-info bg-info/20',
                    'group-hover:scale-125',
                  )}
                />
                {!isLast && (
                  <div className="w-0.5 flex-1 bg-border/60" />
                )}
              </div>

              {/* Request card */}
              <button
                type="button"
                onClick={() => void openLog(req)}
                className={cn(
                  'flex-1 rounded-md p-3 mb-1 text-left transition-all',
                  'bg-card hover:bg-card/50 border border-border hover:border-border',
                  'active:scale-[0.995]',
                  selectedLog?.id === req.id && 'ring-1 ring-info/40 bg-card',
                )}
              >
                <div className="flex items-center gap-2 flex-wrap">
                  {/* Sequence badge */}
                  <span className="text-xs font-mono font-medium text-info bg-info/10 px-1.5 py-0.5 rounded">
                    #{req.trace_seq ?? idx + 1}
                  </span>

                  {/* Method */}
                  <span className={cn(
                    'px-1.5 py-0.5 rounded text-xs font-medium',
                    METHOD_CLASS,
                  )}>
                    {req.method}
                  </span>

                  {/* Status */}
                  <span className={cn('font-mono text-xs font-medium', getStatusColor(req.status_code))}>
                    {req.status_code || '---'}
                  </span>

                  {/* Upstream */}
                  <span className="text-xs font-medium text-muted-foreground bg-muted/50 px-1.5 py-0.5 rounded">
                    {req.upstream}{req.upstream_target ? ` / ${req.upstream_target}` : ''}
                  </span>

                  {req.streaming && (
                    <span className="text-xs font-medium text-primary bg-primary/10 px-1 py-0.5 rounded">
                      {req.websocket ? 'WS' : 'SSE'}
                    </span>
                  )}

                  {req.error && (
                    <AlertCircle className="h-3 w-3 text-danger" />
                  )}

                  <span className="ml-auto text-xs font-mono text-muted-foreground">
                    {formatLatency(req.latency_ms)}
                  </span>
                </div>

                {/* Path */}
                <div className="mt-1.5 text-xs font-mono text-foreground/70 truncate">
                  {req.path}{req.query ? `?${req.query}` : ''}
                </div>

                {/* Latency bar */}
                <div className="mt-2 flex items-center gap-2">
                  <div className="flex-1 h-1.5 rounded-full bg-muted/50 overflow-hidden">
                    {/* Offset indicator */}
                    <div className="h-full flex">
                      <div style={{ width: `${offsetPct}%` }} />
                      <div
                        className={cn(
                          'h-full rounded-md transition-all duration-500',
                          req.error ? 'bg-danger/50' :
                          req.status_code >= 400 ? 'bg-warning/50' :
                          'bg-info/50',
                        )}
                        style={{ width: `${Math.max(3, (req.latency_ms / maxLatency) * (100 - offsetPct))}%` }}
                      />
                    </div>
                  </div>
                  <span className="text-xs text-muted-foreground/60 shrink-0">
                    {new Date(req.created_at).toLocaleTimeString()}
                  </span>
                </div>
              </button>
            </div>
          )
        })}
      </div>

      {/* Time range footer */}
      {requests.length > 0 && (
        <div className="flex items-center justify-between text-xs text-muted-foreground/60 px-1">
          <span>{formatDate(new Date(summary.first_time).toISOString(), i18n.language)}</span>
          <span className="font-mono">{formatLatency(summary.total_latency_ms)} {t('traces.total_span')}</span>
          <span>{formatDate(new Date(summary.last_time).toISOString(), i18n.language)}</span>
        </div>
      )}
        </TabsContent>
      </Tabs>

      {/* Log detail Sheet */}
      <LogDetail
        log={selectedLog}
        loading={selectedLogLoading}
        onClose={handleCloseLog}
        onNavigateLog={handleNavigateLog}
        canNavigatePreviousLog={selectedLogIndex > 0}
        canNavigateNextLog={selectedLogIndex >= 0 && selectedLogIndex < requests.length - 1}
        onLogChange={(updated) => {
          setSelectedLog(updated)
          setDetail(prev => {
            if (!prev) return prev
            return {
              ...prev,
              requests: prev.requests.map(r => r.id === updated.id ? updated : r),
            }
          })
        }}
      />

      {/* AI 分析面板：流式状态在模块级 store 中，关闭面板不中断输出 */}
      <Sheet open={analysisOpen} onOpenChange={setAnalysisOpen}>
        <SheetContent side="right" className="w-full sm:max-w-xl p-4 flex flex-col">
          <SheetHeader className="p-0 pb-2">
            <SheetTitle className="text-sm flex items-center gap-1.5">
              <Sparkles className="h-4 w-4 text-primary" />
              {t('analysis.sheet_title')}
            </SheetTitle>
            <SheetDescription className="text-xs truncate font-mono">{traceId}</SheetDescription>
          </SheetHeader>
          <div className="flex-1 min-h-0">
            <AnalysisChat traceId={traceId ?? ''} open={analysisOpen} />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}

function SummaryCard({
  icon: Icon,
  label,
  value,
  accent,
}: {
  icon: typeof Layers
  label: string
  value: string
  accent: string
}) {
  return (
    <div className="rounded-md border border-border bg-card p-3 space-y-1">
      <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
        <Icon className={cn('h-3 w-3', accent)} />
        {label}
      </div>
      <div className={cn('text-lg font-mono font-semibold', accent)}>{value}</div>
    </div>
  )
}
