import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { uploadBackup, confirmBackupImport, cancelBackupImport, MAX_BACKUP_IMPORT_BYTES, type BackupImportJob } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const knownErrors = new Set(['backup_busy', 'backup_import_layout', 'backup_import_state', 'backup_import_size', 'backup_import_expired', 'backup_import_incompatible', 'backup_import_invalid', 'backup_import_interrupted', 'backup_import_origin', 'backup_import_type', 'backup_import_confirmation', 'backup_space', 'backup_status_save_failed'])

type Props = {
  job?: BackupImportJob | null
  supported: boolean
  errorCode?: string
  open: boolean
  onOpenChange: (open: boolean) => void
  onChanged: () => Promise<void>
}

export function BackupImport({ job, supported, errorCode, open, onOpenChange, onChanged }: Props) {
  const { t, i18n } = useTranslation()
  const [file, setFile] = useState<File | null>(null)
  const [uploading, setUploading] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])
  const checking = job?.state === 'checking' || job?.state === 'uploading'
  const pending = job?.state === 'pending' || job?.state === 'applying'
  const message = (code?: string) => knownErrors.has(code ?? '') ? t(`backups.import_errors.${code}`) : t('backups.import_failed')

  async function upload() {
    if (!file || uploading || busy || !supported) return
    if (file.size === 0 || file.size > MAX_BACKUP_IMPORT_BYTES) { setError(message('backup_import_size')); return }
    const request = new AbortController()
    controller.current = request
    setUploading(true)
    setError('')
    try {
      await uploadBackup(file, request.signal)
      setFile(null)
      await onChanged()
    } catch (error) {
      if (!request.signal.aborted) setError(message(error instanceof Error ? error.message : ''))
    } finally {
      if (controller.current === request) controller.current = null
      setUploading(false)
    }
  }

  async function confirm() {
    if (!job || busy) return
    setBusy(true)
    setError('')
    try { await confirmBackupImport(job.id); await onChanged() }
    catch (error) { setError(message(error instanceof Error ? error.message : '')); await onChanged() }
    finally { setBusy(false) }
  }

  async function clear() {
    if (!job || busy) return
    setBusy(true)
    setError('')
    try { await cancelBackupImport(job.id); setFile(null); await onChanged() }
    catch (error) { setError(message(error instanceof Error ? error.message : '')); await onChanged() }
    finally { setBusy(false) }
  }

  function changeOpen(next: boolean) {
    if (busy) return
    if (!next && uploading) controller.current?.abort()
    if (next) setError('')
    onOpenChange(next)
  }

  return <>
    {job && <div className="flex flex-wrap items-center justify-between gap-3 rounded-md bg-muted/30 px-3 py-2 text-sm">
      <p role="status" className={job.state === 'failed' ? 'text-danger' : 'text-muted-foreground'}>{t(`backups.import_states.${job.state}`)}</p>
      <Button variant="ghost" size="sm" onClick={() => changeOpen(true)}>{t('backups.import_view')}</Button>
    </div>}
    {errorCode && <p role="alert" className="text-xs text-danger">{message(errorCode)}</p>}
    <Dialog open={open} onOpenChange={changeOpen}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{t('backups.import_button')}</DialogTitle>
          <DialogDescription>{t('backups.import_description')}</DialogDescription>
        </DialogHeader>
        {!job && !uploading && <div className="space-y-3">
          {!supported ? <p className="text-sm text-muted-foreground">{message('backup_import_layout')}</p> : <>
            <Label htmlFor="backup-import-file">{t('backups.import_file')}</Label>
            <Input id="backup-import-file" type="file" accept=".zip,application/zip" onChange={event => { setFile(event.target.files?.[0] ?? null); setError('') }} />
            <p className="text-xs text-muted-foreground">{t('backups.import_limit')}</p>
          </>}
        </div>}
        {(uploading || checking) && <p role="status" className="flex items-center gap-2 py-4 text-sm text-muted-foreground"><Loader2 className="size-4 shrink-0 animate-spin" />{t(uploading ? 'backups.import_uploading' : 'backups.import_checking')}</p>}
        {job && (job.state === 'ready' || pending || job.state === 'completed') && <div className="space-y-4">
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
            <div><dt className="text-xs text-muted-foreground">{t('backups.import_source')}</dt><dd className="mt-1 break-all">{job.source_version}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('backups.import_target')}</dt><dd className="mt-1 break-all">{job.target_version}</dd></div>
            <div className="col-span-2"><dt className="text-xs text-muted-foreground">{t('backups.import_created')}</dt><dd className="mt-1">{new Date(job.backup_created_at).toLocaleString(i18n.language)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('backups.import_logs')}</dt><dd className="mt-1 tabular-nums">{(job.counts?.request_logs ?? 0).toLocaleString(i18n.language)}</dd></div>
            <div><dt className="text-xs text-muted-foreground">{t('backups.import_reports')}</dt><dd className="mt-1 tabular-nums">{(job.counts?.review_reports ?? 0).toLocaleString(i18n.language)}</dd></div>
          </dl>
          {job.upgrade_required && <p className="text-xs text-muted-foreground">{t('backups.import_upgrade')}</p>}
          <p className="rounded-md bg-muted/30 p-3 text-sm leading-relaxed">{t(job.state === 'completed' ? 'backups.import_completed_hint' : pending ? 'backups.import_pending_hint' : 'backups.import_replace_hint')}</p>
        </div>}
        {job?.state === 'failed' && <p role="alert" className="text-sm text-danger">{message(job.error)}</p>}
        {error && <p role="alert" className="text-sm text-danger">{error}</p>}
        <DialogFooter className="flex-wrap">
          <Button variant="outline" disabled={busy} onClick={() => changeOpen(false)}>{t('common.close')}</Button>
          {job && !checking && <Button variant="outline" disabled={busy || uploading} onClick={() => void clear()}>{t(pending ? 'backups.import_cancel_pending' : 'backups.import_clear')}</Button>}
          {!job && supported && <Button disabled={!file || busy || uploading} onClick={() => void upload()}>{uploading && <Loader2 className="size-4 animate-spin" />}{t('backups.import_upload')}</Button>}
          {job?.state === 'ready' && <Button variant="destructive" disabled={busy} onClick={() => void confirm()}>{busy && <Loader2 className="size-4 animate-spin" />}{t('backups.import_confirm')}</Button>}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </>
}
