import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { DropdownMenu } from 'radix-ui'
import { Archive, ArchiveRestore, ChevronDown, ListChecks, Loader2, Trash2, X } from 'lucide-react'
import { toast } from 'sonner'
import { ApiError } from '@/lib/api'
import { applyTaskChange, previewTaskChange, type TaskAction, type TaskChange, type TaskItem, type TaskKind, type TaskPreview } from '@/lib/task-management'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'

interface ManagementProps {
  kind: TaskKind
  items: TaskItem[]
  archived?: boolean
  experimentId?: string
  disabled?: boolean
  onChanged: (action: TaskAction) => void
}

export function TaskSelectionToggle({ active, onToggle, disabled = false }: { active: boolean; onToggle: () => void; disabled?: boolean }) {
  const { t } = useTranslation()
  return <Button type="button" variant="ghost" size="sm" className="h-7 gap-1.5 px-2 text-xs text-muted-foreground" aria-pressed={active} disabled={disabled} onClick={onToggle}>
    {active ? <X className="size-3.5" /> : <ListChecks className="size-3.5" />}{t(active ? 'task_management.exit_selection' : 'task_management.bulk_manage')}
  </Button>
}

export function TaskActions({ name, ...props }: ManagementProps & { name: string }) {
  const { t } = useTranslation()
  const [deleting, setDeleting] = useState(false)
  const [busy, setBusy] = useState(false)
  const change = async (action: TaskAction) => {
    setBusy(true)
    try {
      await applyTaskChange({ kind: props.kind, action, items: props.items.map(item => ({ id: item.id })), experiment_id: props.experimentId })
      toast.success(t(`task_management.done.${action}`))
      props.onChanged(action)
    } catch (error) {
      toast.error(t(error instanceof ApiError && error.status === 409 ? 'task_management.blocked_notice' : 'task_management.failed'))
    } finally { setBusy(false) }
  }
  const menuItem = 'flex cursor-default select-none items-center gap-2 rounded px-3 py-2 text-sm outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground data-[disabled]:opacity-50'
  return <span className="relative inline-flex shrink-0" onClick={event => event.stopPropagation()}>
    <DropdownMenu.Root>
      <DropdownMenu.Trigger asChild><Button type="button" variant="ghost" size="sm" className="group h-8 gap-1 px-2 text-xs font-normal text-muted-foreground hover:text-foreground data-[state=open]:bg-accent data-[state=open]:text-accent-foreground pointer-coarse:min-h-11" aria-label={t('task_management.actions_for', { name })} disabled={props.disabled || busy || !props.items.length}>
        {t('task_management.manage')}{busy ? <Loader2 className="size-3.5 animate-spin" /> : <ChevronDown aria-hidden="true" className="size-3.5 transition-transform group-data-[state=open]:rotate-180 motion-reduce:transition-none" />}
      </Button></DropdownMenu.Trigger>
      <DropdownMenu.Portal>
        <DropdownMenu.Content align="end" sideOffset={5} className="z-50 min-w-40 rounded-md border border-border bg-popover p-1 text-popover-foreground shadow-md">
          {props.kind !== 'run' && <DropdownMenu.Item className={menuItem} onSelect={() => void change(props.archived ? 'restore' : 'archive')}>
            {props.archived ? <ArchiveRestore className="size-4" /> : <Archive className="size-4" />}{t(props.archived ? 'task_management.restore' : 'task_management.archive')}
          </DropdownMenu.Item>}
          {props.kind !== 'run' && <DropdownMenu.Separator className="my-1 h-px bg-border" />}
          <DropdownMenu.Item className={`${menuItem} text-destructive data-[highlighted]:text-destructive`} onSelect={() => setDeleting(true)}><Trash2 className="size-4" />{t(`task_management.delete_label.${props.kind}`)}</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Portal>
    </DropdownMenu.Root>
    {deleting && <TaskDeleteDialog {...props} name={name} onClose={() => setDeleting(false)} />}
  </span>
}

export function TaskBatchActions(props: ManagementProps) {
  const { t } = useTranslation()
  const [deleting, setDeleting] = useState(false)
  const [busy, setBusy] = useState(false)
  const change = async () => {
    const action = props.archived ? 'restore' : 'archive'
    setBusy(true)
    try {
      await applyTaskChange({ kind: props.kind, action, items: props.items.map(item => ({ id: item.id })), experiment_id: props.experimentId })
      toast.success(t(`task_management.done.${action}`))
      props.onChanged(action)
    } catch (error) { toast.error(t(error instanceof ApiError && error.status === 409 ? 'task_management.blocked_notice' : 'task_management.failed')) }
    finally { setBusy(false) }
  }
  if (!props.items.length) return null
  return <div className="flex flex-wrap items-center gap-2 text-xs" onClick={event => event.stopPropagation()}>
    <span className="text-muted-foreground">{t('task_management.selected_count', { count: props.items.length })}</span>
    {props.kind !== 'run' && <Button type="button" size="sm" variant="outline" disabled={props.disabled || busy} onClick={() => void change()}>{busy ? <Loader2 className="size-3.5 animate-spin" /> : props.archived ? <ArchiveRestore className="size-3.5" /> : <Archive className="size-3.5" />}{t(props.archived ? 'task_management.restore' : 'task_management.archive')}</Button>}
    <Button type="button" size="sm" variant="ghost" className="text-destructive" disabled={props.disabled || busy} onClick={() => setDeleting(true)}><Trash2 className="size-3.5" />{t('task_management.delete_selected')}</Button>
    {deleting && <TaskDeleteDialog {...props} onClose={() => setDeleting(false)} />}
  </div>
}

function TaskDeleteDialog({ name, kind, items, experimentId, onClose, onChanged }: ManagementProps & { name?: string; onClose: () => void }) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<TaskPreview | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const requestKey = JSON.stringify({ kind, action: 'delete', items: items.map(item => ({ id: item.id })), experiment_id: experimentId })
  const request = useMemo(() => JSON.parse(requestKey) as TaskChange, [requestKey])
  const load = useCallback(async (signal?: AbortSignal) => {
    try {
      const next = await previewTaskChange(request, signal)
      if (!signal?.aborted) setPreview(next)
    } catch { if (!signal?.aborted) setError(t('task_management.preview_failed')) }
  }, [request, t])
  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])
  async function confirm() {
    if (!preview?.can_apply || busy) return
    setBusy(true)
    setError('')
    try {
      await applyTaskChange({ ...request, items: preview.items.map(item => ({ id: item.id, revision: item.revision })) })
      toast.success(t('task_management.done.delete'))
      onClose()
      onChanged('delete')
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        setError(t('task_management.preview_changed'))
        setPreview(null)
        await load()
      } else setError(t('task_management.failed'))
    } finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}>
    <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-lg">
      <DialogHeader className="text-left">
        <DialogTitle>{t(`task_management.delete_label.${kind}`)}</DialogTitle>
        <DialogDescription>{t(`task_management.delete_hint.${kind}`)}</DialogDescription>
      </DialogHeader>
      {name && <p className="break-words text-sm font-medium">{name}</p>}
      {!preview ? <div className="flex items-center gap-2 py-3 text-sm text-muted-foreground">{!error && <Loader2 className="size-4 animate-spin" />}{t(error ? 'task_management.preview_failed' : 'task_management.preview_loading')}</div> : <div className="space-y-3">
        <div className="flex flex-wrap gap-x-5 gap-y-2 rounded-md bg-muted px-3 py-3 text-xs tabular-nums">
          <span>{t('task_management.selected_count', { count: preview.total_items })}</span>
          {kind === 'trace' && <span>{t('task_management.deleted_logs', { count: preview.log_count })}</span>}
          {kind === 'capture' && <span>{t('task_management.kept_logs', { count: preview.log_count })}</span>}
          {kind === 'experiment' && <><span>{t('task_management.deleted_runs', { count: preview.run_count })}</span><span>{t('task_management.deleted_reports', { count: preview.report_count })}</span></>}
        </div>
        {preview.items.some(item => item.blocked_reason) && <div className="space-y-3 rounded-md border border-border p-3">
          {preview.items.filter(item => item.blocked_reason).map(item => <div key={item.id} className="space-y-1 text-xs">
            <p className="break-words font-medium">{item.name || item.id}</p>
            <p className="text-destructive">{t(`task_management.reasons.${item.blocked_reason}`)}</p>
            {!!item.referenced_by?.length && <div className="flex flex-wrap gap-x-3 gap-y-1">{item.referenced_by.slice(0, 5).map(reference => <Link key={reference.id} className="break-words text-primary hover:underline" to={`/experiments/${encodeURIComponent(reference.id)}`} onClick={onClose}>{reference.name || reference.id}</Link>)}</div>}
          </div>)}
        </div>}
      </div>}
      {error && <p role="alert" className="text-xs text-destructive">{error}</p>}
      <DialogFooter>
        <Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button>
        {!preview && error && <Button type="button" variant="outline" disabled={busy} onClick={() => { setError(''); void load() }}>{t('common.refresh')}</Button>}
        <Button type="button" variant="destructive" disabled={busy || !preview?.can_apply} onClick={() => void confirm()}>{busy && <Loader2 className="size-4 animate-spin" />}{t(`task_management.delete_label.${kind}`)}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
}
