import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Code2, FilePlus2, FlaskConical, Pencil, Plus, Shield } from 'lucide-react'
import { toast } from 'sonner'
import { fetchTemplate, fetchTemplates, presetTemplates, reviewError, saveTemplate, type ReviewConfig, type ReviewTemplate, type TemplateSummary } from '@/lib/review'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { TaskActions, TaskBatchActions, TaskSelectionToggle } from '@/components/TaskManagement'
import ObservationPicker from './ObservationPicker'

type Props = { onChoose: (template: ReviewTemplate) => void; disabled?: boolean; manage?: boolean }
export default function ReviewTemplates({ onChoose, disabled, manage = false }: Props) {
  const { t } = useTranslation()
  const [page, setPage] = useState<{ key: string; items: TemplateSummary[]; total: number } | null>(null)
  const [offset, setOffset] = useState(0)
  const [refresh, setRefresh] = useState(0)
  const [editing, setEditing] = useState<ReviewTemplate | null>(null)
  const [loading, setLoading] = useState('')
  const [error, setError] = useState('')
  const [archived, setArchived] = useState(false)
  const [selection, setSelection] = useState<{ key: string; ids: string[]; active: boolean }>({ key: '', ids: [], active: false })
  const showArchived = manage && archived
  const pageKey = `${offset}:${refresh}:${showArchived}`
  const selecting = manage && selection.key === pageKey && selection.active
  const selected = selecting ? selection.ids : []
  const items = page?.key === pageKey ? page.items : []
  const total = page?.total ?? 0
  const pending = page?.key !== pageKey && !error
  const selectedItems = items.filter(item => selected.includes(item.id)).map(item => ({ id: item.id, revision: item.revision }))
  const exitSelection = () => setSelection({ key: '', ids: [], active: false })
  const toggleSelected = (id: string, checked: boolean) => setSelection({ key: pageKey, active: true, ids: checked ? [...selected.filter(value => value !== id), id] : selected.filter(value => value !== id) })
  const changed = () => { exitSelection(); setRefresh(value => value + 1) }
  useEffect(() => {
    let canceled = false
    void fetchTemplates(offset, showArchived).then(data => {
      if (!canceled) {
        if (offset > 0 && offset >= data.total) { setOffset(Math.max(0, Math.ceil(data.total / 20) - 1) * 20); return }
        setPage({ key: pageKey, items: data.templates, total: data.total }); setError('')
      }
    }).catch(e => { if (!canceled) setError(reviewError(e, t)) })
    return () => { canceled = true }
  }, [offset, showArchived, pageKey, t])
  const choose = async (id: string, edit = false) => {
    exitSelection()
    setLoading(id)
    try { const value = await fetchTemplate(id); if (edit) setEditing(value); else onChoose(value) }
    catch (e) { toast.error(reviewError(e, t)) }
    finally { setLoading('') }
  }
  const icons = [Shield, Code2, FlaskConical, FilePlus2]
  return <div className="space-y-4">
    <p className="text-sm text-muted-foreground">{t('review.template_intro')}</p>
    {!showArchived && <div className="grid gap-3 sm:grid-cols-2">
      {presetTemplates(t).map((template, i) => {
        const Icon = icons[i]
        return <button key={template.id} disabled={disabled || !!loading} className="space-y-2 rounded-xl border border-border bg-card p-4 text-left transition-colors hover:border-primary/50 hover:bg-primary/5 focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50" onClick={() => onChoose(template)}>
          <div className="flex items-center gap-2 font-medium"><Icon className="size-5 text-primary" />{template.name}</div>
          <p className="text-xs leading-relaxed text-muted-foreground">{template.description}</p>
          <p className="text-xs text-primary">{t('review.use_template')}</p>
        </button>
      })}
    </div>}
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className="text-sm font-medium">{t('review.my_templates')}</h3>
      {manage && <div className="flex flex-wrap items-center gap-2">
        {(items.length > 0 || selecting) && <TaskSelectionToggle active={selecting} disabled={pending || disabled || !!loading} onToggle={() => selecting ? exitSelection() : setSelection({ key: pageKey, ids: [], active: true })} />}
        <select aria-label={t('task_management.archive_filter')} className="h-9 rounded-md border border-input bg-background px-2 text-sm" value={String(archived)} onChange={event => { setArchived(event.target.value === 'true'); setOffset(0); exitSelection() }}>
        <option value="false">{t('task_management.unarchived')}</option><option value="true">{t('task_management.archived')}</option>
      </select><Button size="sm" variant="outline" disabled={disabled} onClick={() => {
        exitSelection()
        const base = presetTemplates(t)[3]
        setEditing({ ...base, id: '', name: '', description: '', config: { ...base.config, template_id: '', template_name: '' } })
      }}><Plus className="size-4" />{t('review.create_template')}</Button></div>}
    </div>
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    {selecting && items.length > 0 && <div className="flex flex-wrap items-center justify-between gap-3">
      <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={items.every(item => selected.includes(item.id))} onChange={event => setSelection({ key: pageKey, active: true, ids: event.target.checked ? items.map(item => item.id) : [] })} />{t('task_management.select_page')}</label>
      {selectedItems.length > 0 && <TaskBatchActions kind="template" items={selectedItems} archived={showArchived} disabled={disabled || !!loading} onChanged={changed} />}
    </div>}
    {!items.length && !error && <p className="text-xs text-muted-foreground">{t(pending ? 'common.loading' : showArchived ? 'task_management.empty_archived' : 'review.no_templates')}</p>}
    <div className="grid gap-3 sm:grid-cols-2">{items.map(item => <div key={item.id} className={cn('rounded-xl border border-border bg-card p-4 transition-colors', selecting && selected.includes(item.id) && 'border-primary/50 bg-primary/5')}>
      <div className="flex items-start gap-3">
        {selecting && <input className="mt-1" type="checkbox" aria-label={t('task_management.select_item', { name: item.name })} checked={selected.includes(item.id)} onChange={event => toggleSelected(item.id, event.target.checked)} />}
        {showArchived && !selecting ? <div className="min-w-0 flex-1 space-y-2"><p className="break-words font-medium">{item.name}</p><p className="line-clamp-2 text-xs text-muted-foreground">{item.description || t('review.point_count', { count: item.point_count })}</p><p className="text-xs text-muted-foreground">{t('task_management.archived')}</p></div> : <button type="button" disabled={disabled || !!loading} aria-pressed={selecting ? selected.includes(item.id) : undefined} onClick={() => selecting ? toggleSelected(item.id, !selected.includes(item.id)) : void choose(item.id)} className="block min-w-0 flex-1 space-y-2 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-primary disabled:opacity-50">
          <p className="break-words font-medium">{item.name}</p><p className="line-clamp-2 text-xs text-muted-foreground">{item.description || t('review.point_count', { count: item.point_count })}</p>
          {!selecting && <p className="text-xs text-primary">{loading === item.id ? t('common.loading') : t('review.use_template')}</p>}
        </button>}
      </div>
      {manage && !selecting && <div className="mt-3 flex flex-wrap items-center justify-between gap-2">
        <Button size="xs" variant="ghost" disabled={disabled || !!loading} onClick={() => void choose(item.id, true)}><Pencil className="size-3" />{t('review.edit_template')}</Button>
        <TaskActions kind="template" items={[{ id: item.id, revision: item.revision }]} name={item.name} archived={showArchived} disabled={disabled || !!loading} onChanged={changed} />
      </div>}
    </div>)}</div>
    {total > 20 && <div className="flex items-center gap-2 text-xs"><Button size="xs" variant="outline" disabled={pending || !offset} onClick={() => { setOffset(Math.max(0, offset - 20)); exitSelection() }}>{t('correlation.previous')}</Button><span>{offset / 20 + 1} / {Math.ceil(total / 20)}</span><Button size="xs" variant="outline" disabled={pending || offset + 20 >= total} onClick={() => { setOffset(offset + 20); exitSelection() }}>{t('correlation.next')}</Button></div>}
    {editing && <TemplateEditor value={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); changed() }} />}
  </div>
}

function TemplateEditor({ value, onClose, onSaved }: { value: ReviewTemplate; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => structuredClone(value))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const save = async () => {
    setBusy(true)
    try { await saveTemplate(draft); toast.success(t('review.template_saved')); onSaved() }
    catch (e) { setError(reviewError(e, t)) }
    finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl"><DialogHeader>
    <DialogTitle>{t(value.id ? 'review.edit_template' : 'review.create_template')}</DialogTitle><DialogDescription>{t('review.template_snapshot_hint')}</DialogDescription>
  </DialogHeader>
    <label className="space-y-1 text-xs">{t('review.template_name')}<Input value={draft.name} maxLength={100} disabled={busy} onChange={e => setDraft(v => ({ ...v, name: e.target.value }))} /></label>
    <label className="space-y-1 text-xs">{t('review.template_description')}<Input value={draft.description} maxLength={400} disabled={busy} onChange={e => setDraft(v => ({ ...v, description: e.target.value }))} /></label>
    <ObservationPicker value={draft.config} disabled={busy} onChange={config => setDraft(v => ({ ...v, config }))} />
    <label className="space-y-1 text-xs">{t('review.instructions')}<Textarea value={draft.config.instructions} rows={2} maxLength={2600} disabled={busy} onChange={e => setDraft(v => ({ ...v, config: { ...v.config, instructions: e.target.value } }))} /></label>
    {error && <p role="alert" className="text-xs text-danger">{error}</p>}
    <div className="flex flex-wrap gap-2"><Button disabled={busy || !draft.name.trim() || !draft.config.selected.length} onClick={() => void save()}>{t('common.save')}</Button><Button variant="outline" disabled={busy} onClick={onClose}>{t('common.cancel')}</Button></div>
  </DialogContent></Dialog>
}

export function SaveTemplateDialog({ config, onClose }: { config: ReviewConfig; onClose: () => void }) {
  return <TemplateEditor value={{ id: '', name: config.template_name, description: '', config: structuredClone(config), revision: 0, archived: false }} onClose={onClose} onSaved={onClose} />
}
