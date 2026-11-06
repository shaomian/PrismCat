import { useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Loader2 } from 'lucide-react'
import { updateConfig } from '@/lib/api'
import { MAX_RETENTION_DAYS, parseStoragePolicy, storagePolicyChanged, storagePolicyDraft, type StoragePolicy } from '@/lib/storage-policy'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { StorageCleanup } from '@/components/StorageCleanup'

type Props = {
    policy: StoragePolicy
    onSaved: (policy: StoragePolicy) => void
    cleanupPaused: boolean
    onResumeCleanup: () => Promise<void>
    onCleanupCompleted: () => void
}

export function StoragePolicySetting({ policy, onSaved, cleanupPaused, onResumeCleanup, onCleanupCompleted }: Props) {
    const { t } = useTranslation()
    const [draft, setDraft] = useState(() => storagePolicyDraft(policy))
    const [saving, setSaving] = useState(false)
    const [resumeOpen, setResumeOpen] = useState(false)
    const [resuming, setResuming] = useState(false)
    const [saved, setSaved] = useState(false)
    const parsed = parseStoragePolicy(draft)
    const dirty = storagePolicyChanged(draft, policy)

    async function save(event: FormEvent<HTMLFormElement>) {
        event.preventDefault()
        if (!parsed || !dirty || saving) return
        setSaving(true)
        try {
            await updateConfig({ storage: parsed })
            onSaved(parsed)
            setDraft(storagePolicyDraft(parsed))
            setSaved(true)
            toast.success(t('settings.storage_policy_saved'))
        } catch (error) {
            toast.error(error instanceof Error ? error.message : t('common.error'))
        } finally { setSaving(false) }
    }

    async function resume() {
        if (resuming || saving || dirty) return
        setResuming(true)
        try {
            await onResumeCleanup()
            setResumeOpen(false)
            toast.success(t('backups.cleanup_resumed'))
        } catch {
            toast.error(t('backups.action_failed'))
        } finally { setResuming(false) }
    }

    return <>
        {cleanupPaused && <div className="mt-5 flex flex-wrap items-center justify-between gap-3 rounded-md bg-warning/10 p-3 text-sm text-warning">
            <p className="max-w-3xl">{t('backups.cleanup_paused')}</p>
            <Button variant="outline" size="sm" disabled={resuming || saving || dirty} onClick={() => setResumeOpen(true)}>{t('backups.resume_cleanup')}</Button>
        </div>}
        <form id="storage-policy" onSubmit={event => void save(event)} className="mt-5 scroll-mt-20 space-y-4 border-t border-input pt-5">
            <div className="flex items-center justify-between gap-3">
                <h3 className="text-sm font-medium">{t('settings.storage_policy_title')}</h3>
                <span className="text-xs text-muted-foreground" role="status">{t(dirty ? 'settings.storage_policy_unsaved' : 'settings.storage_policy_up_to_date')}</span>
            </div>
            <div className="grid gap-5 sm:grid-cols-2">
                <div className="space-y-2">
                    <Label htmlFor="retention-days">{t('settings.retention_days')}</Label>
                    <div className="flex max-w-xs items-center gap-3">
                        <Input id="retention-days" type="number" min="0" max={MAX_RETENTION_DAYS} step="1" value={draft.days} disabled={saving}
                            aria-describedby="retention-days-hint" onChange={event => { setDraft({ ...draft, days: event.target.value }); setSaved(false) }} />
                        <span className="shrink-0 text-xs text-muted-foreground">{t('settings.days')}</span>
                    </div>
                    <p id="retention-days-hint" className="text-xs leading-relaxed text-muted-foreground">{t('settings.retention_days_hint')}</p>
                </div>
                <div className="space-y-2">
                    <Label htmlFor="max-storage">{t('settings.max_storage_bytes')}</Label>
                    <div className="flex max-w-xs items-center gap-3">
                        <Input id="max-storage" type="number" min="0" step="any" value={draft.limitGB} disabled={saving}
                            aria-describedby="max-storage-hint" onChange={event => { setDraft({ ...draft, limitGB: event.target.value }); setSaved(false) }} />
                        <span className="shrink-0 text-xs text-muted-foreground">GB</span>
                    </div>
                    <p id="max-storage-hint" className="text-xs leading-relaxed text-muted-foreground">{t('settings.max_storage_bytes_hint')}</p>
                </div>
            </div>
            <p className="rounded-md bg-muted/30 px-3 py-2 text-xs leading-relaxed text-muted-foreground">{t('settings.storage_policy_capacity_hint')}</p>
            {dirty && !parsed && <p role="alert" className="text-xs text-danger">{t('settings.storage_policy_invalid', { max: MAX_RETENTION_DAYS })}</p>}
            <div className="flex flex-wrap items-center justify-between gap-3">
                <p role="status" className="text-xs text-muted-foreground">{t(dirty ? 'settings.storage_policy_save_first' : saved ? 'settings.storage_policy_saved' : 'settings.storage_policy_schedule_hint')}</p>
                <div className="flex items-center gap-2">
                    {dirty && <Button type="button" variant="outline" size="sm" disabled={saving} onClick={() => { setDraft(storagePolicyDraft(policy)); setSaved(false) }}>{t('settings.storage_policy_revert')}</Button>}
                    <Button type="submit" size="sm" disabled={saving || !dirty || !parsed}>
                        {saving && <Loader2 className="size-4 animate-spin" />}{t('settings.storage_policy_save')}
                    </Button>
                </div>
            </div>
        </form>
        <StorageCleanup onCompleted={onCleanupCompleted} disabled={dirty || saving || resuming || cleanupPaused || policy.retention_days <= 0}
            disabledHint={!dirty && !saving && !cleanupPaused && policy.retention_days <= 0 ? t('task_cleanup.disabled') : undefined}
            retentionDays={policy.retention_days} />
        <Dialog open={resumeOpen} onOpenChange={open => { if (!resuming) setResumeOpen(open) }}>
            <DialogContent>
                <DialogHeader>
                    <DialogTitle>{t('backups.confirm.resume_title')}</DialogTitle>
                    <DialogDescription>{t('backups.confirm.resume_description')}</DialogDescription>
                </DialogHeader>
                <DialogFooter>
                    <Button variant="outline" disabled={resuming} onClick={() => setResumeOpen(false)}>{t('common.cancel')}</Button>
                    <Button disabled={resuming || saving || dirty} onClick={() => void resume()}>{resuming && <Loader2 className="size-4 animate-spin" />}{t('backups.confirm_action')}</Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    </>
}
