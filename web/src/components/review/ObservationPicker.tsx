import { useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Pencil, Plus, Trash2 } from 'lucide-react'
import type { ReviewConfig, ReviewPoint } from '@/lib/review'
import { cn } from '@/lib/utils'
import { TaskActionBar, TaskIconButton } from '@/components/TaskHover'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

export default function ObservationPicker({ value, onChange, disabled = false, actions, suggestedIds = [] }: { value: ReviewConfig; onChange: (value: ReviewConfig) => void; disabled?: boolean; actions?: ReactNode; suggestedIds?: string[] }) {
  const { t } = useTranslation()
  const id = useId()
  const [name, setName] = useState('')
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<string | null>(null)
  const point = value.points.find(point => point.id === editing)
  const change = (id: string, patch: Partial<ReviewPoint>) => onChange({ ...value, points: value.points.map(p => p.id === id ? { ...p, ...patch } : p) })
  const add = () => {
    if (!name.trim() || value.points.length >= 30 || value.selected.length >= 20) return
    const point = { id: crypto.randomUUID(), name: name.trim(), instructions: '' }
    onChange({ ...value, points: [...value.points, point], selected: [...value.selected, point.id] })
    setName(''); setAdding(false); setEditing(point.id)
  }
  return <div className="space-y-3">
    <div className="flex flex-wrap items-center gap-2">
      {actions}
      <Button type="button" variant="ghost" size="sm" disabled={disabled || value.points.length >= 30} aria-expanded={adding} aria-controls={`${id}-add`} onClick={() => setAdding(!adding)}><Plus className="size-3.5" />{t('review.custom_action')}</Button>
    </div>
    {adding && <div id={`${id}-add`} className="flex gap-2">
      <Input autoFocus aria-label={t('review.custom_point')} placeholder={t('review.custom_point')} value={name} maxLength={100} disabled={disabled} onChange={event => setName(event.target.value)} onKeyDown={event => { if (event.key === 'Enter' && !event.nativeEvent.isComposing) { event.preventDefault(); add() } }} />
      <Button type="button" variant="outline" disabled={disabled || !name.trim() || value.selected.length >= 20} onClick={add}>{t('review.add')}</Button>
    </div>}
    <div className="flex flex-wrap gap-2">{value.points.map(point => {
      const checked = value.selected.includes(point.id)
      return <div key={point.id} className={cn('task-item inline-flex max-w-full items-center gap-1 rounded-md border py-1 pl-2.5 pr-1', checked ? 'border-primary/15 bg-primary/5' : 'border-border bg-background')}>
          <label className="review-point-label flex min-h-7 min-w-0 cursor-pointer items-center gap-2 text-sm" title={point.instructions || undefined}>
            <input type="checkbox" className="accent-primary" checked={checked} disabled={disabled || (!checked && value.selected.length >= 20)} onChange={e => onChange({ ...value, selected: e.target.checked ? [...value.selected, point.id] : value.selected.filter(id => id !== point.id) })} />
            <span className="min-w-0 break-words">{point.name}</span>
            {suggestedIds.includes(point.id) && <span className="shrink-0 text-xs text-primary">{t('review.ai_candidate')}</span>}
          </label>
          <TaskActionBar>
            <TaskIconButton size="icon-xs" className="task-direct-action" label={t('review.edit_point', { name: point.name })} disabled={disabled} onClick={() => setEditing(editing === point.id ? null : point.id)}><Pencil className="size-3" /></TaskIconButton>
            {!point.metric && <TaskIconButton size="icon-xs" className="task-direct-action" label={t('review.remove_point', { name: point.name })} disabled={disabled} onClick={() => { onChange({ ...value, points: value.points.filter(value => value.id !== point.id), selected: value.selected.filter(id => id !== point.id) }); if (editing === point.id) setEditing(null) }}><Trash2 className="size-3" /></TaskIconButton>}
          </TaskActionBar>
      </div>
    })}</div>
    {point && <div className="space-y-3 rounded-md border border-border p-3">
      <Input aria-label={t('review.point_name')} maxLength={100} value={point.name} disabled={disabled} onChange={event => change(point.id, { name: event.target.value })} />
      <Textarea aria-label={t('review.point_instructions')} placeholder={t('review.point_instructions')} rows={2} maxLength={650} value={point.instructions} disabled={disabled} onChange={event => change(point.id, { instructions: event.target.value })} />
      <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={() => setEditing(null)}><Check className="size-3.5" />{t('review.finish_point')}</Button>
    </div>}
    <p className="text-xs text-muted-foreground">{t('review.selection_count', { count: value.selected.length })}</p>
  </div>
}
