import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Download, Loader2, RefreshCw, Trash2, Upload } from 'lucide-react'
import { toast } from 'sonner'
import { createBackup, deleteBackup, backupDownloadUrl, type BackupJob } from '@/lib/api'
import type { SystemBackupStatus } from '@/lib/use-system-backups'
import { SettingSection } from '@/pages/settings/shared'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { BackupImport } from '@/components/BackupImport'

function bytes(value: number) {
  if (value < 1024) return `${value} B`
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), 4)
  return `${(value / 1024 ** index).toFixed(1)} ${['B', 'KB', 'MB', 'GB', 'TB'][index]}`
}

type Action = { kind: 'create' } | { kind: 'delete'; job: BackupJob }

export function SystemBackups({ status }: { status: SystemBackupStatus }) {
  const { t, i18n } = useTranslation()
  const { jobs, error, running, refresh } = status
  const [busy, setBusy] = useState(false)
  const [action, setAction] = useState<Action | null>(null)
  const [importOpen, setImportOpen] = useState(false)

  async function apply() {
    if (!action || busy) return
    setBusy(true)
    try {
      if (action.kind === 'create') {
        const job = await createBackup()
        status.created(job)
      } else if (action.kind === 'delete') {
        await deleteBackup(action.job.id)
      }
      setAction(null)
      await refresh()
    } catch {
      toast.error(t('backups.action_failed'))
    } finally { setBusy(false) }
  }

  const failures = new Set(['backup_interrupted', 'backup_body_missing', 'backup_body_corrupt', 'backup_space', 'backup_status_save_failed'])
  return (
    <SettingSection title={t('backups.title')} description={t('backups.section_description')} action={
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="icon-sm" onClick={() => { void refresh() }} aria-label={t('backups.refresh')}><RefreshCw className="size-4" /></Button>
          <Button variant="outline" size="sm" disabled={busy || running && !status.importJob} onClick={() => setImportOpen(true)}><Upload className="size-4" />{t('backups.import_button')}</Button>
          <Button variant="outline" size="sm" disabled={busy || running} onClick={() => setAction({ kind: 'create' })}>
            {running && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}{t('backups.create')}
          </Button>
        </div>
    }>
      <div className="space-y-4">
      <p className="text-xs leading-relaxed text-muted-foreground">{t('backups.description')}</p>
      <BackupImport job={status.importJob} supported={status.importSupported} errorCode={status.importError} open={importOpen} onOpenChange={setImportOpen} onChanged={refresh} />
      {error && <p role="alert" className="text-sm text-danger">{error}</p>}
      {!error && jobs.length === 0 && <p className="py-3 text-sm text-muted-foreground">{t('backups.empty')}</p>}
      <div className="divide-y divide-border">
        {jobs.map(job => <div key={job.id} className="flex flex-wrap items-center justify-between gap-3 py-3">
          <div className="min-w-0 space-y-1 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <span>{new Date(job.created_at).toLocaleString(i18n.language)}</span>
              <span className={job.state === 'failed' ? 'text-danger' : 'text-muted-foreground'}>{t(`backups.states.${job.state}`)}</span>
              {job.state === 'completed' && <span className="text-muted-foreground">{bytes(job.size)}</span>}
            </div>
            {job.state === 'running' && <p className="text-xs text-muted-foreground">{t(`backups.phases.${job.progress.phase}`)} · {t('backups.progress', { count: job.progress.files, size: bytes(job.progress.bytes) })}</p>}
            {job.state === 'failed' && <p className="text-xs text-danger">{t(`backups.failures.${failures.has(job.error ?? '') ? job.error : 'backup_failed'}`)}</p>}
          </div>
          <div className="flex gap-2">
            {job.state === 'completed' && <Button asChild variant="outline" size="sm"><a href={backupDownloadUrl(job.id)} download><Download className="mr-2 h-4 w-4" />{t('backups.download')}</a></Button>}
            <Button variant="ghost" size="sm" disabled={busy || job.state === 'running'} onClick={() => setAction({ kind: 'delete', job })} aria-label={t('backups.delete')}><Trash2 className="h-4 w-4" /></Button>
          </div>
        </div>)}
      </div>
      <details className="text-sm">
        <summary className="cursor-pointer text-muted-foreground">{t('backups.restore_title')}</summary>
        <div className="mt-3 space-y-3 text-xs leading-relaxed text-muted-foreground">
          <p>{t('backups.scope')}</p>
          <p>{t('backups.web_restore_description')}</p>
          <p>{t('backups.compatibility_description')}</p>
          <p>{t('backups.web_restore_replace')}</p>
          <details>
            <summary className="cursor-pointer text-sm">{t('backups.cli_restore_title')}</summary>
            <p className="my-3">{t('backups.restore_description')}</p>
            <pre className="overflow-x-auto rounded-md bg-muted p-3"><code>{'prismcat backup create --config data/config.yaml --file backup.zip\nprismcat backup inspect --file backup.zip\nprismcat backup restore --file backup.zip --target data --dry-run\nprismcat backup restore --file backup.zip --target data\nprismcat -config data/config.yaml'}</code></pre>
            <p className="mt-2">{t('backups.restore_replace')}</p>
          </details>
        </div>
      </details>
      <Dialog open={action !== null} onOpenChange={open => { if (!open && !busy) setAction(null) }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t(`backups.confirm.${action?.kind ?? 'create'}_title`)}</DialogTitle>
            <DialogDescription>{t(`backups.confirm.${action?.kind ?? 'create'}_description`)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => setAction(null)}>{t('backups.cancel')}</Button>
            <Button disabled={busy} onClick={() => { void apply() }}>{busy && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}{t('backups.confirm_action')}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      </div>
    </SettingSection>
  )
}
