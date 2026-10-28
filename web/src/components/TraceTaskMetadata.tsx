import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { GitMerge, ListChecks, Loader2, Pencil, Pin, PinOff, Star } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { emptyTraceMetadata, fetchTraceTaskMetadata, saveTraceTaskMetadata, type TraceTaskMetadata } from '@/lib/experiments'
import type { TaskAction } from '@/lib/task-management'
import { TaskActions, TaskMenuItem } from '@/components/TaskManagement'
import { TaskActionBar, TaskIconButton } from '@/components/TaskHover'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TraceOriginBadge } from '@/components/TraceOriginBadge'
import { TraceLabelEditor, TraceLabels } from '@/components/TraceLabels'
import { collectTraceLabels, maxTraceLabels } from '@/lib/trace-labels'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export function TraceTaskControls({ traceId, metadata, onChange, onManaged, onMerge, onOrganize, disabled }: { traceId: string; metadata?: TraceTaskMetadata | null; onChange: (value: TraceTaskMetadata) => void; onManaged: (action: TaskAction) => void; onMerge?: () => void; onOrganize?: () => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState(false)
  const update = async (patch: Partial<TraceTaskMetadata>) => {
    setBusy(true)
    try { const value = metadata ?? emptyTraceMetadata(traceId); onChange(await saveTraceTaskMetadata({ ...value, ...patch })) }
    catch (error) {
      toast.error(t(error instanceof ApiError && error.status === 409 ? 'task_management.blocked_notice' : 'experiments.failed'))
      if (error instanceof ApiError && error.status === 409) void fetchTraceTaskMetadata(traceId).then(onChange).catch(() => {})
    } finally { setBusy(false) }
  }
  return <>
    <TaskActionBar className="relative z-10">
      <span aria-busy={busy}><TaskActions kind="trace" name={metadata?.name || traceId} items={[{ id: traceId }]} archived={metadata?.archived} disabled={disabled || busy} onEdit={() => setEditing(true)} editLabel={t('trace_manager.edit_info')} extraItems={<>
        {onMerge && <TaskMenuItem onSelect={onMerge}><GitMerge className="size-4" />{t('trace_grouping.merge_to')}</TaskMenuItem>}
        {onOrganize && <TaskMenuItem onSelect={onOrganize}><ListChecks className="size-4" />{t('trace_grouping.organize')}</TaskMenuItem>}
        {!metadata?.archived && <TaskMenuItem onSelect={() => void update({ top_at: metadata?.top_at ? 0 : 1 })}>{metadata?.top_at ? <PinOff className="size-4" /> : <Pin className="size-4" />}{t(metadata?.top_at ? 'trace_manager.remove_top' : 'trace_manager.move_top')}</TaskMenuItem>}
        <TaskMenuItem onSelect={() => void update({ pinned: !metadata?.pinned })}><Star className="size-4" />{t(metadata?.pinned ? 'trace_manager.unpin' : 'trace_manager.pin')}</TaskMenuItem>
      </>} onChanged={onManaged} /></span>
    </TaskActionBar>
    {editing && <TraceMetadataDialog traceId={traceId} onClose={() => setEditing(false)} onSaved={onChange} />}
  </>
}

function TraceMetadataDialog({ traceId, onClose, onSaved }: { traceId: string; onClose: () => void; onSaved: (value: TraceTaskMetadata) => void }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<TraceTaskMetadata | null>(null)
  const [labelInput, setLabelInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [conflict, setConflict] = useState(false)
  const [reload, setReload] = useState(0)
  useEffect(() => {
    let cancelled = false
    void fetchTraceTaskMetadata(traceId).then(value => { if (!cancelled) { setDraft(value); setLabelInput('') } }).catch(() => { if (!cancelled) setError(t('trace_manager.load_failed')) })
    return () => { cancelled = true }
  }, [traceId, reload, t])
  const change = (patch: Partial<TraceTaskMetadata>) => setDraft(previous => previous ? { ...previous, ...patch } : previous)
  const nextLabels = collectTraceLabels(draft?.labels ?? [], labelInput)
  const tooManyLabels = nextLabels.length > maxTraceLabels
  const save = async () => {
    if (!draft || busy || conflict || tooManyLabels) return
    setBusy(true); setError('')
    try {
      const value = await saveTraceTaskMetadata({ ...draft, name: draft.name.trim(), labels: nextLabels })
      onSaved(value); toast.success(t('trace_manager.saved')); onClose()
    } catch (reason) {
      const changed = reason instanceof ApiError && reason.status === 409
      setConflict(changed); setError(changed ? t('trace_manager.conflict') : (reason as Error).message)
    } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}><DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-md" onInteractOutside={event => event.preventDefault()} onEscapeKeyDown={event => { if (busy) event.preventDefault() }}>
    <DialogHeader><DialogTitle>{t('trace_manager.edit_info')}</DialogTitle><DialogDescription>{t('trace_manager.edit_hint')}</DialogDescription></DialogHeader>
    {!draft ? <div className="flex items-center gap-2 text-sm text-muted-foreground">{!error && <Loader2 className="size-4 animate-spin" />}{t(error ? 'trace_manager.load_failed' : 'common.loading')}</div> : <div className="space-y-4">
      <label className="block space-y-1.5 text-xs">{t('trace_manager.name')}<Input autoFocus disabled={busy} value={draft.name} maxLength={200} onChange={event => change({ name: event.target.value })} /></label>
      <TraceLabelEditor labels={draft.labels ?? []} input={labelInput} disabled={busy} onChange={labels => change({ labels })} onInputChange={setLabelInput} />
      <label className="block space-y-1.5 text-xs">{t('trace_manager.notes')}<Textarea disabled={busy} value={draft.notes} rows={3} maxLength={16000} onChange={event => change({ notes: event.target.value })} /></label>
    </div>}
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    <DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>{!draft && error && <Button type="button" variant="outline" onClick={() => { setError(''); setReload(value => value + 1) }}>{t('common.refresh')}</Button>}<Button type="button" disabled={busy || !draft || conflict || tooManyLabels} onClick={() => void save()}>{busy && <Loader2 className="size-4 animate-spin" />}{t('common.save')}</Button></DialogFooter>
  </DialogContent></Dialog>
}

export function TraceTaskManager({ traceId }: { traceId: string }) { return <TraceTaskHeader key={traceId} traceId={traceId} /> }
function TraceTaskHeader({ traceId }: { traceId: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [metadata, setMetadata] = useState<TraceTaskMetadata | null>(null)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  useEffect(() => {
    let cancelled = false
    void fetchTraceTaskMetadata(traceId).then(value => { if (!cancelled) setMetadata(value) }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [traceId])
  const loadLatest = async () => { const value = await fetchTraceTaskMetadata(traceId); setMetadata(value); setError('') }
  return <section className="trace-copy-scope space-y-2">
    <div className="task-item flex items-start justify-between gap-3">
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <h2 className="min-w-0 break-words text-lg font-semibold">{metadata?.name || t('trace_manager.unnamed')}</h2>
        {!!metadata?.top_at && <Pin className="size-3.5 shrink-0 text-primary" aria-label={t('trace_manager.top')} />}
        {metadata?.pinned && <Star className="size-3.5 shrink-0 fill-warning text-warning" aria-label={t('trace_manager.pinned')} />}
      <TaskActionBar><TaskIconButton label={t('trace_manager.edit_info')} disabled={!metadata} onClick={() => setEditing(true)}><Pencil className="size-3.5" /></TaskIconButton></TaskActionBar>
        {metadata?.archived && <span className="text-xs text-muted-foreground">{t('trace_manager.archived')}</span>}
      </div>
    </div>
    <div className="flex min-w-0 flex-wrap items-center gap-1.5"><code className="min-w-0 break-all text-xs text-muted-foreground">{traceId}</code><CopyTraceIdButton traceId={traceId} contextual />{metadata?.origin && <TraceOriginBadge origin={metadata.origin} />}</div>
    <TraceLabels labels={metadata?.labels ?? []} onSelect={label => navigate(`/traces?label=${encodeURIComponent(label)}`)} />
    {metadata?.notes && <p className="line-clamp-3 whitespace-pre-wrap break-words text-sm text-muted-foreground">{metadata.notes}</p>}
    {error && <div className="flex flex-wrap items-center gap-2"><p role="alert" className="text-xs text-danger">{error}</p>{!metadata && <Button size="xs" variant="outline" onClick={() => void loadLatest().catch(reason => setError((reason as Error).message))}>{t('common.refresh')}</Button>}</div>}
    {editing && <TraceMetadataDialog traceId={traceId} onClose={() => setEditing(false)} onSaved={setMetadata} />}
  </section>
}
