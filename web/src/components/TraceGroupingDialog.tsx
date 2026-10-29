import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { applyTraceGrouping, previewTraceGrouping, type GroupingAction, type GroupingPreview, type GroupingRequest, type GroupingResult } from '@/lib/trace-grouping'
import { TraceSearch } from '@/components/TraceTaskManager'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export function TraceGroupingDialog({ action, traceIds, logIds, onClose, onApplied }: { action: GroupingAction; traceIds: string[]; logIds?: string[]; onClose: () => void; onApplied: (result: GroupingResult) => void }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [target, setTarget] = useState(action === 'merge' && traceIds.length > 1 ? traceIds[0] : '')
  const [page, setPage] = useState<{ key: string; value?: GroupingPreview; error?: string } | null>(null)
  const [busy, setBusy] = useState(false)
  const [revision, setRevision] = useState(0)
  const request: GroupingRequest = { action, trace_ids: traceIds, log_ids: logIds, target_trace_id: action === 'split' ? undefined : target.trim() }
  const key = JSON.stringify(request)
  const ready = action === 'split' || !!target.trim()
  const current = page?.key === `${key}:${revision}` ? page : null
  const preview = current?.value
  useEffect(() => {
    if (!ready) return
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      void previewTraceGrouping(JSON.parse(key) as GroupingRequest, controller.signal).then(value => {
        if (!controller.signal.aborted) setPage({ key: `${key}:${revision}`, value })
      }).catch(() => { if (!controller.signal.aborted) setPage({ key: `${key}:${revision}`, error: t('trace_grouping.failed') }) })
    }, 250)
    return () => { window.clearTimeout(timer); controller.abort() }
  }, [key, ready, revision, t])
  const apply = async () => {
    if (!preview?.can_apply || busy) return
    setBusy(true)
    try {
      const result = await applyTraceGrouping({ ...request, version: preview.version })
      toast.success(t('trace_grouping.done', { count: result.log_count }), { action: { label: t('trace_grouping.view_target'), onClick: () => navigate(`/traces/${encodeURIComponent(result.target_trace_id)}`) } })
      onApplied(result)
      onClose()
    } catch (error) {
      toast.error(t(error instanceof ApiError && error.status === 409 ? 'trace_grouping.changed' : 'trace_grouping.failed'))
      setRevision(value => value + 1)
    } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-lg" onInteractOutside={event => event.preventDefault()} onEscapeKeyDown={event => { if (busy) event.preventDefault() }}>
      <DialogHeader><DialogTitle>{t(`trace_grouping.${action}`)}</DialogTitle><DialogDescription>{t(`trace_grouping.${action}_hint`)}</DialogDescription></DialogHeader>
      <div className="space-y-4">
        <p className="text-sm">{action === 'merge' ? t('trace_grouping.source_traces', { count: traceIds.length }) : t('trace_grouping.selected_requests', { count: logIds?.length ?? 0 })}</p>
        {action !== 'split' && <div className="space-y-1.5"><p className="text-xs font-medium">{t('trace_grouping.target')}</p><TraceSearch value={target} onInput={setTarget} onSelect={setTarget} disabled={busy} excludeId={traceIds.length === 1 ? traceIds[0] : undefined} allowExplicit={false} /></div>}
        {ready && !current && <p role="status" className="flex items-center gap-2 text-xs text-muted-foreground"><Loader2 className="size-3.5 animate-spin" />{t('common.loading')}</p>}
        {current?.error && <div role="alert" className="space-y-2 text-xs text-danger"><p>{current.error}</p><Button size="xs" variant="outline" onClick={() => setRevision(value => value + 1)}>{t('common.refresh')}</Button></div>}
        {preview && <div className="space-y-2 rounded-md bg-muted px-3 py-3 text-sm">
          <p className="break-words">{t('trace_grouping.preview', { count: preview.log_count, target: action === 'split' ? t('trace_grouping.new_trace') : preview.target_name || preview.target_trace_id })}</p>
          {preview.reason && <p role="alert" className="text-xs text-danger">{t(`trace_grouping.reasons.${preview.reason}`)}</p>}
        </div>}
        <p className="text-xs leading-relaxed text-muted-foreground">{t('trace_grouping.preserve_hint')}</p>
      </div>
      <DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button><Button type="button" disabled={busy || !preview?.can_apply} onClick={() => void apply()}>{busy && <Loader2 className="size-4 animate-spin" />}{t(`trace_grouping.confirm_${action}`)}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}
