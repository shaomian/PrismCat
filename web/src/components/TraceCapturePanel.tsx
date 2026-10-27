import { lazy, Suspense, useCallback, useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronLeft, ChevronRight, Loader2, Radio, RefreshCw, Square } from 'lucide-react'
import { toast } from 'sonner'
import { fetchCaptureRuns, fetchLog, fetchLogs, startCaptureRun, stopCaptureRun, type CaptureRun, type CorrelationStatus, type RequestLog, type Upstream } from '@/lib/api'
import { captureLogsPageSize, captureNameBytes, capturePageSize, captureTokens, correlationSettingsPath, defaultCaptureName } from '@/lib/trace-capture'
import { useVisiblePolling } from '@/lib/use-visible-polling'
import { cn, formatLatency } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TaskActions, TaskBatchActions, TaskSelectionToggle } from '@/components/TaskManagement'

const LogDetail = lazy(async () => { const module = await import('@/components/LogDetail'); return { default: module.LogDetail } })

export function CaptureRecordingDialog({ upstreams, status, onClose, onStarted }: {
  upstreams: Upstream[]
  status: CorrelationStatus | null
  onClose: () => void
  onStarted: () => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [upstream, setUpstream] = useState('')
  const [single, setSingle] = useState(false)
  const [busy, setBusy] = useState(false)
  const [openedAt] = useState(() => new Date())
  const entries = upstreams.filter(entry => entry.logging_enabled)
  const isRecording = (entry: string) => status?.active_captures.some(run => run.upstream === entry) ?? false
  const selectedEntry = upstream || entries.find(entry => !isRecording(entry.name))?.name || ''
  const suggestedName = defaultCaptureName(selectedEntry, openedAt)
  const nameInvalid = captureNameBytes(name) > 200
  const available = !!status && !!selectedEntry && !isRecording(selectedEntry)

  async function start(event: FormEvent) {
    event.preventDefault()
    if (busy || !available || nameInvalid) return
    setBusy(true)
    try {
      await startCaptureRun(name.trim() || suggestedName, selectedEntry, single && !!status?.enabled)
      toast.success(t('correlation.started_notice'))
      onStarted()
      onClose()
    } catch (error) { toast.error((error as Error).message) }
    finally { setBusy(false) }
  }

  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-[440px]" onEscapeKeyDown={event => { if (busy) event.preventDefault() }} onPointerDownOutside={event => { if (busy) event.preventDefault() }}>
      <DialogHeader className="text-left">
        <DialogTitle>{t('correlation.new_capture')}</DialogTitle>
        <DialogDescription>{t('correlation.new_capture_hint')}</DialogDescription>
      </DialogHeader>
      <form onSubmit={event => void start(event)} className="space-y-5">
        <div className="space-y-2">
          <Label htmlFor="capture-upstream">{t('correlation.entry_label')}</Label>
          <select id="capture-upstream" className="h-10 w-full rounded-md border border-input bg-background px-3 text-sm" value={selectedEntry} disabled={busy || !status} onChange={event => setUpstream(event.target.value)}>
            {!selectedEntry && <option value="">{t(entries.length ? 'correlation.no_available_entry' : 'correlation.no_entries')}</option>}
            {entries.map(entry => <option key={entry.name} value={entry.name} disabled={isRecording(entry.name)}>{entry.name}{isRecording(entry.name) ? ` · ${t('correlation.recording')}` : ''}</option>)}
          </select>
          {entries.length === 0 && <p className="text-xs text-muted-foreground">{t('correlation.no_entries_hint')} <Link className="text-primary hover:underline" to="/settings/routing" onClick={onClose}>{t('correlation.configure_entries')}</Link></p>}
          {entries.length > 0 && !available && <p className="text-xs text-muted-foreground">{t(status ? 'correlation.entry_busy_hint' : 'common.loading')}</p>}
        </div>
        <div className="space-y-2">
          <Label htmlFor="capture-name">{t('correlation.name')}<span className="font-normal text-muted-foreground">{t('correlation.optional')}</span></Label>
          <Input id="capture-name" value={name} maxLength={200} disabled={busy} aria-invalid={nameInvalid} aria-describedby="capture-name-hint" placeholder={t('correlation.auto_name_placeholder')} onChange={event => setName(event.target.value)} />
          <p id="capture-name-hint" className={cn('break-words text-xs', nameInvalid ? 'text-destructive' : 'text-muted-foreground')}>
            {nameInvalid ? t('correlation.name_too_long') : t('correlation.auto_name_hint', { name: suggestedName })}
          </p>
        </div>
        <details className="group border-t border-border pt-3 text-xs">
          <summary className="flex w-fit cursor-pointer list-none items-center gap-1 text-muted-foreground hover:text-foreground [&::-webkit-details-marker]:hidden"><ChevronDown className="size-3.5 transition-transform group-open:rotate-180" />{t('correlation.more_options')}</summary>
          <div className="mt-4 space-y-2">
            <label className="flex items-start gap-2"><input type="checkbox" className="mt-0.5 accent-primary" checked={single && !!status?.enabled} disabled={busy || !status?.enabled} onChange={event => setSingle(event.target.checked)} /><span>{t('correlation.single_task')}</span></label>
            <p className="pl-5 text-muted-foreground">{t('correlation.single_task_hint')}</p>
            {!status?.enabled && <Link className="ml-5 inline-block text-primary hover:underline" to={correlationSettingsPath} onClick={onClose}>{t('correlation.single_task_settings')}</Link>}
          </div>
        </details>
        <DialogFooter className="pt-1">
          <Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>
          <Button type="submit" disabled={busy || !available || nameInvalid}>{busy ? <Loader2 className="size-4 animate-spin" /> : <Radio className="size-4" />}{t('correlation.start')}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

export function TraceCapturePanel({ revision, recording, onChanged }: { revision: number; recording: boolean; onChanged: () => void }) {
  const { t, i18n } = useTranslation()
  const [offset, setOffset] = useState(0)
  const [reload, setReload] = useState(0)
  const [page, setPage] = useState<{ key: string; runs: CaptureRun[]; total: number } | null>(null)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [selected, setSelected] = useState<CaptureRun | null>(null)
  const [archived, setArchived] = useState(false)
  const [selection, setSelection] = useState<{ key: string; ids: string[]; active: boolean }>({ key: '', ids: [], active: false })
  const key = `${offset}:${revision}:${reload}:${archived}`
  const load = useCallback(async (signal: AbortSignal) => {
    setLoading(true)
    try {
      const result = await fetchCaptureRuns({ limit: capturePageSize, offset, archived }, signal)
      if (!signal.aborted) {
        const total = result.total ?? result.runs?.length ?? 0
        if (offset > 0 && offset >= total) { setOffset(Math.max(0, Math.ceil(total / capturePageSize) - 1) * capturePageSize); return }
        setPage({ key, runs: result.runs ?? [], total })
        setError('')
      }
    } catch (error) { if (!signal.aborted) setError((error as Error).message) }
    finally { if (!signal.aborted) setLoading(false) }
  }, [key, offset, archived])
  useVisiblePolling(load, true, recording && !archived ? 5000 : 0)

  async function stop(id: string) {
    setBusy(id)
    try {
      await stopCaptureRun(id)
      const stopped_at = new Date().toISOString()
      setPage(previous => previous && { ...previous, runs: previous.runs.map(run => run.id === id ? { ...run, stopped_at } : run) })
      setSelected(previous => previous?.id === id ? { ...previous, stopped_at } : previous)
      toast.success(t('correlation.stopped_notice'))
      onChanged()
    } catch (error) { toast.error((error as Error).message) }
    finally { setBusy('') }
  }

  const runs = page?.key === key ? page.runs : []
  const manageableRuns = runs.filter(run => !!run.stopped_at)
  const selecting = selection.key === key && selection.active
  const selectedIds = selecting ? selection.ids : []
  const selectedItems = manageableRuns.filter(run => selectedIds.includes(run.id)).map(run => ({ id: run.id }))
  function exitSelection() { setSelection({ key: '', ids: [], active: false }) }
  function toggleSelected(id: string, checked: boolean) { setSelection({ key, active: true, ids: checked ? [...selectedIds.filter(value => value !== id), id] : selectedIds.filter(value => value !== id) }) }
  function managementChanged() { setSelected(null); exitSelection(); setReload(value => value + 1); onChanged() }
  const total = page?.total ?? 0
  const selectedRun = page?.runs.find(run => run.id === selected?.id) ?? selected
  return <div className="space-y-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <p className="max-w-2xl text-xs leading-relaxed text-muted-foreground">{t('correlation.capture_hint')}</p>
      <div className="flex shrink-0 items-center gap-2">
        {(manageableRuns.length > 0 || selecting) && <TaskSelectionToggle active={selecting} disabled={loading || !!busy} onToggle={() => selecting ? exitSelection() : setSelection({ key, ids: [], active: true })} />}
        <select aria-label={t('task_management.archive_filter')} className="h-8 rounded-md border border-input bg-background px-2 text-xs" value={String(archived)} onChange={event => { setArchived(event.target.value === 'true'); setOffset(0); setSelected(null); exitSelection() }}><option value="false">{t('task_management.unarchived')}</option><option value="true">{t('trace_manager.archived')}</option></select><Button variant="ghost" size="icon-sm" disabled={loading} onClick={() => { exitSelection(); setReload(value => value + 1) }} aria-label={t('common.refresh')}><RefreshCw className={cn('size-4', loading && 'animate-spin')} /></Button></div>
    </div>
    {selecting && runs.length > 0 && <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <label className="inline-flex items-center gap-1.5"><input type="checkbox" disabled={!manageableRuns.length} checked={manageableRuns.length > 0 && selectedItems.length === manageableRuns.length} onChange={event => setSelection({ key, active: true, ids: event.target.checked ? manageableRuns.map(run => run.id) : [] })} />{t('task_management.select_page')}</label>
      <TaskBatchActions kind="capture" items={selectedItems} archived={archived} disabled={!!busy} onChanged={managementChanged} />
    </div>}
    {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    {runs.length === 0 ? <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border py-16 text-center">
      {loading ? <Loader2 className="size-6 animate-spin text-muted-foreground" /> : <Radio className="size-7 text-muted-foreground/60" />}
      <p className="text-sm text-muted-foreground">{t(loading ? 'common.loading' : error ? 'correlation.failed' : 'correlation.no_captures')}</p>
    </div> : <div className="divide-y divide-border rounded-lg border border-border bg-card px-4 sm:px-5">
      {runs.map(run => <div key={run.id} className={cn('flex flex-col gap-3 py-4 transition-colors sm:flex-row sm:items-center sm:justify-between', selecting && selectedIds.includes(run.id) && 'bg-primary/5')}>
        <div className="flex min-w-0 flex-1 items-start gap-3">
        {selecting && <input type="checkbox" className="mt-1 shrink-0" aria-label={t('task_management.select_item', { name: run.name })} disabled={!run.stopped_at} checked={selectedIds.includes(run.id)} onChange={event => toggleSelected(run.id, event.target.checked)} />}
        <button type="button" className="group min-w-0 flex-1 space-y-2 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-60" aria-pressed={selecting ? selectedIds.includes(run.id) : undefined} disabled={selecting && !run.stopped_at} onClick={() => selecting ? toggleSelected(run.id, !selectedIds.includes(run.id)) : setSelected(run)}>
          <div className="flex min-w-0 items-center gap-2.5">
            <span className="truncate text-sm font-medium transition-colors group-hover:text-primary group-focus-visible:text-primary" title={run.name}>{run.name}</span>
            <span className={cn('inline-flex shrink-0 items-center gap-1.5 text-xs', run.stopped_at ? 'text-muted-foreground' : 'text-danger')}>
              {!run.stopped_at && <span className="size-1.5 rounded-full bg-danger" />}{t(run.stopped_at ? 'correlation.stopped' : 'correlation.recording')}
            </span>
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs tabular-nums text-muted-foreground">
            <span className="max-w-48 truncate" title={run.upstream}>{run.upstream}</span>
            <span>{t('correlation.requests_count', { count: run.request_count })}</span>
            <span>{formatLatency(run.wall_time_ms)}</span>
            <span>{captureTokens(run)?.toLocaleString(i18n.language) ?? '—'} Token</span>
            <time dateTime={run.started_at}>{new Date(run.started_at).toLocaleString(i18n.language, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}</time>
          </div>
        </button>
        </div>
        {(!selecting || !run.stopped_at) && <div className="flex shrink-0 items-center justify-end gap-1.5">
          {!run.stopped_at && <Button variant="outline" size="sm" className="text-danger" disabled={!!busy} onClick={() => void stop(run.id)}>{busy === run.id ? <Loader2 className="size-3.5 animate-spin" /> : <Square className="size-3" />}{t('correlation.stop')}</Button>}
          {!selecting && run.stopped_at && <TaskActions kind="capture" items={[{ id: run.id }]} name={run.name} archived={!!run.archived} disabled={!!busy} onChanged={managementChanged} />}
        </div>}
      </div>)}
    </div>}
    {total > 0 && <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
      <span>{t('correlation.total_captures', { count: total })}</span>
      <div className="flex items-center gap-2">
        <Button size="icon-sm" variant="ghost" aria-label={t('correlation.previous')} disabled={loading || offset === 0} onClick={() => { exitSelection(); setOffset(value => Math.max(0, value - capturePageSize)) }}><ChevronLeft className="size-4" /></Button>
        <span>{Math.floor(offset / capturePageSize) + 1} / {Math.max(1, Math.ceil(total / capturePageSize))}</span>
        <Button size="icon-sm" variant="ghost" aria-label={t('correlation.next')} disabled={loading || offset + capturePageSize >= total} onClick={() => { exitSelection(); setOffset(value => value + capturePageSize) }}><ChevronRight className="size-4" /></Button>
      </div>
    </div>}
    {selectedRun && <CaptureRunDetail key={selectedRun.id} run={selectedRun} onClose={() => setSelected(null)} />}
  </div>
}

function CaptureRunDetail({ run, onClose }: { run: CaptureRun; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const [offset, setOffset] = useState(0)
  const [reload, setReload] = useState(0)
  const [page, setPage] = useState<{ key: string; logs: RequestLog[]; total: number } | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [log, setLog] = useState<RequestLog | null>(null)
  const [openingLog, setOpeningLog] = useState('')
  const logRequest = useRef<AbortController | null>(null)
  const id = run.id
  const key = `${offset}:${reload}`
  const load = useCallback(async (signal: AbortSignal) => {
    setLoading(true)
    try {
      const result = await fetchLogs({ capture_run_id: id, offset, limit: captureLogsPageSize }, signal)
      if (!signal.aborted) { setPage({ key, logs: result.logs ?? [], total: result.total }); setError('') }
    } catch (error) { if (!signal.aborted) setError((error as Error).message) }
    finally { if (!signal.aborted) setLoading(false) }
  }, [id, key, offset])
  useVisiblePolling(load, true, run.stopped_at ? 0 : 5000)
  useEffect(() => () => logRequest.current?.abort(), [])

  async function openLog(entry: RequestLog) {
    logRequest.current?.abort()
    const controller = new AbortController()
    logRequest.current = controller
    setOpeningLog(entry.id)
    try {
      const next = await fetchLog(entry.id, controller.signal)
      if (!controller.signal.aborted) setLog(next)
    } catch (error) { if (!controller.signal.aborted) toast.error((error as Error).message) }
    finally { if (!controller.signal.aborted) setOpeningLog('') }
  }

  const logs = page?.key === key ? page.logs : []
  const total = page?.total ?? 0
  return <>
    <Sheet open onOpenChange={open => { if (!open) onClose() }}>
      <SheetContent className="w-full gap-0 sm:max-w-xl">
        <SheetHeader className="border-b border-border px-5 py-5 pr-12">
          <SheetTitle className="break-words leading-relaxed">{run.name}</SheetTitle>
          <SheetDescription className="break-words">{run.upstream} · {t(run.stopped_at ? 'correlation.stopped' : 'correlation.recording')}{run.single_task ? ` · ${t('correlation.single_task_short')}` : ''}</SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto px-5 py-5">
          <dl className="grid grid-cols-3 gap-3 border-b border-border pb-5 text-sm tabular-nums">
            <div><dt className="text-xs text-muted-foreground">{t('traces.requests')}</dt><dd className="mt-1">{run.request_count.toLocaleString(i18n.language)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('traces.duration')}</dt><dd className="mt-1">{formatLatency(run.wall_time_ms)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">Token</dt><dd className="mt-1">{captureTokens(run)?.toLocaleString(i18n.language) ?? '—'}</dd></div>
          </dl>
          <div className="space-y-2 text-xs text-muted-foreground">
            <p>{t('correlation.started_at', { time: new Date(run.started_at).toLocaleString(i18n.language) })}</p>
            {run.stopped_at && <p>{t('correlation.stopped_at', { time: new Date(run.stopped_at).toLocaleString(i18n.language) })}</p>}
            <p>{t('correlation.usage', { input: run.usage_input_tokens?.toLocaleString(i18n.language) ?? '—', output: run.usage_output_tokens?.toLocaleString(i18n.language) ?? '—', missing: run.usage_missing_count })}</p>
            <p>{t('correlation.stop_hint')}</p>
          </div>
          <div>
            <div className="mb-2 flex items-center justify-between gap-2"><h3 className="text-sm font-medium">{t('correlation.request_records')}</h3><Button size="icon-sm" variant="ghost" aria-label={t('common.refresh')} disabled={loading} onClick={() => setReload(value => value + 1)}><RefreshCw className={cn('size-4', loading && 'animate-spin')} /></Button></div>
            {error && <p role="alert" className="py-2 text-xs text-destructive">{error}</p>}
            {logs.length === 0 && <p className="py-10 text-center text-sm text-muted-foreground">{t(loading ? 'common.loading' : error ? 'correlation.failed' : 'correlation.no_requests')}</p>}
            <div className="divide-y divide-border">
              {logs.map(entry => <div key={entry.id} className="space-y-2 py-3 text-xs">
                <button type="button" className="flex w-full min-w-0 items-start justify-between gap-3 text-left hover:text-primary" disabled={openingLog === entry.id} onClick={() => void openLog(entry)}>
                  <span className="min-w-0 truncate font-mono" title={`${entry.method} ${entry.path}`}>{entry.method} {entry.path}</span>
                  {openingLog === entry.id ? <Loader2 className="size-3.5 shrink-0 animate-spin" /> : <span className={cn('shrink-0 tabular-nums', entry.error || entry.status_code >= 400 ? 'text-danger' : 'text-muted-foreground')}>{entry.status_code || '…'}</span>}
                </button>
                <div className="flex min-w-0 items-center gap-2 text-muted-foreground">
                  <time className="shrink-0" dateTime={entry.created_at}>{new Date(entry.created_at).toLocaleTimeString(i18n.language)}</time>
                  {entry.trace_id ? <span className="flex min-w-0 items-center gap-1"><Link className="truncate font-mono text-primary hover:underline" to={`/traces/${encodeURIComponent(entry.trace_id)}`}>{entry.trace_id}</Link><CopyTraceIdButton traceId={entry.trace_id} /></span> : <span>{t('correlation.unassigned')}</span>}
                </div>
              </div>)}
            </div>
          </div>
        </div>
        <div className="flex items-center justify-between gap-2 border-t border-border px-5 py-3 text-xs text-muted-foreground">
          <span>{t('correlation.requests_count', { count: total })}</span>
          <div className="flex items-center gap-2">
            <Button size="icon-sm" variant="ghost" aria-label={t('correlation.previous')} disabled={loading || offset === 0} onClick={() => setOffset(value => Math.max(0, value - captureLogsPageSize))}><ChevronLeft className="size-4" /></Button>
            <span>{Math.floor(offset / captureLogsPageSize) + 1} / {Math.max(1, Math.ceil(total / captureLogsPageSize))}</span>
            <Button size="icon-sm" variant="ghost" aria-label={t('correlation.next')} disabled={loading || offset + captureLogsPageSize >= total} onClick={() => setOffset(value => value + captureLogsPageSize)}><ChevronRight className="size-4" /></Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
    <Suspense fallback={null}>{log && <LogDetail log={log} onClose={() => setLog(null)} onLogChange={next => {
      setLog(next)
      setPage(previous => previous && { ...previous, logs: previous.logs.map(entry => entry.id === next.id ? next : entry) })
    }} />}</Suspense>
  </>
}
