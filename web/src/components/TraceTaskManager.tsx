import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Star } from 'lucide-react'
import { toast } from 'sonner'
import { fetchTraces, type TraceSummary } from '@/lib/api'
import { emptyTraceMetadata, fetchTraceTaskMetadata, saveTraceTaskMetadata, type TraceTaskMetadata } from '@/lib/experiments'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TaskActions } from '@/components/TaskManagement'

export function TraceSearch({ onSelect }: { onSelect: (id: string) => void }) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<TraceSummary[]>([])
  const [error, setError] = useState('')
  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      void fetchTraces({ q: query, limit: 10, archived: false }).then(response => {
        if (!cancelled) { setItems(response.traces ?? []); setError('') }
      }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    }, 250)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [query])
  return <div className="space-y-1">
    <Input aria-label={t('trace_manager.search_target')} placeholder={t('trace_manager.search_target')} value={query} onFocus={() => setOpen(true)} onChange={event => { setQuery(event.target.value); setOpen(true) }} />
    {open && <div className="max-h-56 overflow-auto rounded-md border border-border bg-card p-1 text-xs">
      <div className="flex items-center justify-between p-1 text-muted-foreground"><span>{error || t('trace_manager.choose_target')}</span><Button type="button" size="xs" variant="ghost" onClick={() => setOpen(false)}>{t('common.close')}</Button></div>
      {items.map(item => <button type="button" key={item.trace_id} className="block w-full rounded p-2 text-left hover:bg-accent" onClick={() => { onSelect(item.trace_id); setOpen(false); setQuery('') }}>
        <p className="truncate font-medium">{item.metadata?.name || t('trace_manager.unnamed')} · {item.request_count} · {new Date(item.last_time).toLocaleString()}</p>
        <p className="truncate font-mono text-muted-foreground">{item.trace_id} · {item.upstreams?.join(', ')}</p>
      </button>)}
      {!items.length && !error && <p className="p-2 text-muted-foreground">{t('trace_manager.no_matches')}</p>}
    </div>}
  </div>
}

export function TracePinButton({ trace, onChange }: { trace: TraceSummary; onChange: (m: TraceTaskMetadata) => void }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  return <Button type="button" size="icon-xs" variant="ghost" disabled={busy} title={t(trace.metadata?.pinned ? 'trace_manager.unpin' : 'trace_manager.pin')} aria-label={t(trace.metadata?.pinned ? 'trace_manager.unpin' : 'trace_manager.pin')} onClick={async event => {
    event.stopPropagation(); setBusy(true)
    try { const value = trace.metadata ?? emptyTraceMetadata(trace.trace_id); onChange(await saveTraceTaskMetadata({ ...value, pinned: !value.pinned })) } catch (error) { toast.error((error as Error).message) } finally { setBusy(false) }
  }}><Star className={trace.metadata?.pinned ? 'size-3.5 fill-warning text-warning' : 'size-3.5 text-muted-foreground'} /></Button>
}

export function TraceTaskManager({ traceId }: { traceId: string }) { return <TaskEditor key={traceId} traceId={traceId} /> }
function TaskEditor({ traceId }: { traceId: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [metadata, setMetadata] = useState<TraceTaskMetadata | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => {
    let cancelled = false
    void fetchTraceTaskMetadata(traceId).then(value => { if (!cancelled) setMetadata(value) }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [traceId])
  const change = (patch: Partial<TraceTaskMetadata>) => setMetadata(previous => previous ? { ...previous, ...patch } : previous)
  return <div className="flex items-start gap-2">
  <details className="min-w-0 flex-1 rounded-lg border border-border bg-card p-3 text-xs">
    <summary className="cursor-pointer font-medium">{metadata?.name || t('trace_manager.title')}{metadata?.pinned ? ` · ${t('trace_manager.pinned')}` : ''}{metadata?.archived ? ` · ${t('trace_manager.archived')}` : ''}</summary>
    {error && <p className="mt-3 text-danger">{error}</p>}
    {metadata && <div className="mt-3 grid gap-3 sm:grid-cols-2">
      <label className="space-y-1">{t('trace_manager.name')}<Input value={metadata.name} maxLength={200} onChange={event => change({ name: event.target.value })} /></label>
      <label className="space-y-1">{t('trace_manager.labels')}<Input value={metadata.labels.join(', ')} onChange={event => change({ labels: event.target.value.split(',').map(value => value.trim()) })} /></label>
      <label className="space-y-1 sm:col-span-2">{t('trace_manager.notes')}<Textarea value={metadata.notes} maxLength={16000} onChange={event => change({ notes: event.target.value })} /></label>
      <label>{t('trace_manager.status')}<select className="ml-2 rounded border border-input bg-background p-2" value={metadata.status} onChange={event => change({ status: event.target.value })}>{['none', 'todo', 'done'].map(status => <option key={status} value={status}>{t(`trace_manager.statuses.${status}`)}</option>)}</select></label>
      <div className="flex flex-wrap gap-4"><label className="flex items-center gap-2"><input type="checkbox" checked={metadata.pinned} onChange={event => change({ pinned: event.target.checked })} />{t('trace_manager.pin')}</label></div>
      <p className="text-muted-foreground sm:col-span-2">{t('trace_manager.retention_hint')}</p>
      <Button size="sm" disabled={busy} onClick={async () => { setBusy(true); try { setMetadata(await saveTraceTaskMetadata(metadata)); toast.success(t('trace_manager.saved')) } catch (reason) { toast.error((reason as Error).message) } finally { setBusy(false) } }}>{t('common.save')}</Button>
    </div>}
  </details>
  {metadata && <TaskActions kind="trace" name={metadata.name || traceId} items={[{ id: traceId }]} archived={metadata.archived} disabled={busy} onChanged={action => {
    if (action === 'delete') navigate('/traces')
    else void fetchTraceTaskMetadata(traceId).then(next => setMetadata(previous => previous ? { ...previous, archived: next.archived, revision: next.revision } : next)).catch(reason => setError((reason as Error).message))
  }} />}
  </div>
}
