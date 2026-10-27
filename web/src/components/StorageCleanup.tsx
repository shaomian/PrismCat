import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { Loader2, Trash2 } from 'lucide-react'
import { toast } from 'sonner'
import { apiRequest, jsonInit } from '@/lib/api'
import { useVisiblePolling } from '@/lib/use-visible-polling'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface CleanupPreview {
  token: string
  enabled: boolean
  retention_days: number
  before: string
  counts: { expired_logs: number; eligible_logs: number; eligible_traces: number; retained_logs: number }
}
interface CleanupJob {
  id: string
  state: 'idle' | 'running' | 'completed' | 'failed'
  deleted_logs: number
  error?: string
}
const errorCodes = new Set(['cleanup_busy', 'cleanup_unavailable', 'cleanup_preview_failed', 'cleanup_preview_required', 'cleanup_preview_expired', 'cleanup_disabled', 'cleanup_config_changed', 'cleanup_failed', 'cleanup_invalid_retention'])

export function StorageCleanup({ onCompleted }: { onCompleted?: () => void }) {
  const { t, i18n } = useTranslation()
  const [open, setOpen] = useState(false)
  const [preview, setPreview] = useState<CleanupPreview | null>(null)
  const [busy, setBusy] = useState(false)
  const [starting, setStarting] = useState(false)
  const [error, setError] = useState('')
  const [job, setJob] = useState<CleanupJob>({ id: '', state: 'idle', deleted_logs: 0 })
  const previousJob = useRef<CleanupJob | null>(null)
  const previewRequest = useRef<AbortController | null>(null)
  const statusRequest = useRef(0)
  const errorText = (error: unknown) => {
    const code = error instanceof Error ? error.message : ''
    return errorCodes.has(code) ? t(`task_cleanup.errors.${code}`) : t('task_cleanup.failed')
  }
  const loadStatus = useCallback(async (signal: AbortSignal) => {
    const request = ++statusRequest.current
    const response = await apiRequest('/api/system/cleanup', { signal }, 'task_cleanup.failed')
    const value = await response.json() as CleanupJob
    if (!signal.aborted && request === statusRequest.current) setJob(value)
  }, [])
  useVisiblePolling(loadStatus, true, job.state === 'running' ? 2000 : 0)
  useEffect(() => () => previewRequest.current?.abort(), [])
  useEffect(() => {
    if (previousJob.current?.state === 'running' && previousJob.current.id === job.id && job.state !== 'running') {
      onCompleted?.()
      if (job.state === 'completed') toast.success(t('task_cleanup.completed', { count: job.deleted_logs }))
      else toast.error(t('task_cleanup.failed'))
    }
    previousJob.current = job
  }, [job, onCompleted, t])

  async function showPreview() {
    previewRequest.current?.abort()
    const controller = new AbortController()
    previewRequest.current = controller
    setOpen(true)
    setPreview(null)
    setError('')
    setBusy(true)
    try {
      const response = await apiRequest('/api/system/cleanup/preview', { signal: controller.signal }, 'task_cleanup.failed')
      const value = await response.json() as CleanupPreview
      if (!controller.signal.aborted) setPreview(value)
    } catch (error) { if (!controller.signal.aborted) setError(errorText(error)) }
    finally { if (!controller.signal.aborted) setBusy(false) }
  }
  async function start() {
    if (!preview?.enabled || !preview.token || busy) return
    setBusy(true)
    setStarting(true)
    setError('')
    try {
      const response = await apiRequest('/api/system/cleanup', jsonInit('POST', { token: preview.token }), 'task_cleanup.failed')
      statusRequest.current++
      setJob(await response.json() as CleanupJob)
      setOpen(false)
      setPreview(null)
    } catch (error) { setError(errorText(error)) }
    finally { setBusy(false); setStarting(false) }
  }

  function closePreview() {
    if (starting) return
    previewRequest.current?.abort()
    setBusy(false)
    setOpen(false)
  }

  return <div className="mt-5 space-y-3 border-t border-input pt-5">
    <div className="flex flex-wrap items-center justify-between gap-4">
      <div className="max-w-2xl space-y-1">
        <h3 className="text-sm font-medium">{t('task_cleanup.title')}</h3>
        <p className="text-xs leading-relaxed text-muted-foreground">{t('task_cleanup.hint')}</p>
      </div>
      <Button variant="outline" size="sm" disabled={busy || job.state === 'running'} onClick={() => void showPreview()}>
        {job.state === 'running' ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
        {t(job.state === 'running' ? 'task_cleanup.running' : 'task_cleanup.preview')}
      </Button>
    </div>
    {job.state === 'running' && <p role="status" className="text-xs text-muted-foreground">{t('task_cleanup.running_hint')}</p>}
    {job.state === 'completed' && <p role="status" className="text-xs text-muted-foreground">{t('task_cleanup.completed', { count: job.deleted_logs })} {t('task_cleanup.space_hint')}</p>}
    {job.state === 'failed' && <p role="alert" className="text-xs text-destructive">{t(job.deleted_logs > 0 ? 'task_cleanup.partial_failed' : 'task_cleanup.failed', { count: job.deleted_logs })}</p>}
    <Dialog open={open} onOpenChange={value => { if (!value) closePreview() }}>
      <DialogContent className="sm:max-w-[460px]">
        <DialogHeader className="text-left">
          <DialogTitle>{t('task_cleanup.confirm_title')}</DialogTitle>
          <DialogDescription>{t('task_cleanup.confirm_hint')}</DialogDescription>
        </DialogHeader>
        {busy && !preview && <p role="status" className="flex items-center gap-2 py-4 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t('task_cleanup.calculating')}</p>}
        {preview && !preview.enabled && <div className="space-y-2 text-sm">
          <p>{t('task_cleanup.disabled')}</p>
          <Link className="text-primary hover:underline" to="/settings/logging/general" onClick={() => setOpen(false)}>{t('task_cleanup.open_settings')}</Link>
        </div>}
        {preview?.enabled && <div className="space-y-4">
          <p className="text-sm">{t('task_cleanup.rule', { days: preview.retention_days, time: new Date(preview.before).toLocaleString(i18n.language) })}</p>
          <dl className="grid grid-cols-3 gap-3 rounded-lg bg-muted/40 p-3 text-sm tabular-nums">
            <div><dt className="text-xs text-muted-foreground">{t('task_cleanup.eligible_logs')}</dt><dd className="mt-1 font-medium">{preview.counts.eligible_logs.toLocaleString(i18n.language)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('task_cleanup.eligible_traces')}</dt><dd className="mt-1 font-medium">{preview.counts.eligible_traces.toLocaleString(i18n.language)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('task_cleanup.retained_logs')}</dt><dd className="mt-1 font-medium">{preview.counts.retained_logs.toLocaleString(i18n.language)}</dd></div>
          </dl>
          <p className="text-xs leading-relaxed text-muted-foreground">{t('task_cleanup.protection_hint')}</p>
          {!preview.counts.eligible_logs && <p className="text-sm">{t('task_cleanup.nothing_to_clean')}</p>}
        </div>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="outline" disabled={starting} onClick={closePreview}>{t('common.cancel')}</Button>
          {error && <Button variant="outline" disabled={busy} onClick={() => void showPreview()}>{t('common.refresh')}</Button>}
          <Button variant="destructive" disabled={busy || !preview?.enabled || !preview.counts.eligible_logs || !!error} onClick={() => void start()}>
            {busy && preview && <Loader2 className="size-4 animate-spin" />}{t('task_cleanup.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
}
