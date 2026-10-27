import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, Pencil, Plus, Trash2 } from 'lucide-react'
import type { ReviewConfig, ReviewPoint } from '@/lib/review'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

export default function ObservationPicker({ value, onChange, disabled = false }: { value: ReviewConfig; onChange: (value: ReviewConfig) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [editing, setEditing] = useState<string | null>(null)
  const change = (id: string, patch: Partial<ReviewPoint>) => onChange({ ...value, points: value.points.map(p => p.id === id ? { ...p, ...patch } : p) })
  const add = () => {
    if (!name.trim() || value.points.length >= 30 || value.selected.length >= 20) return
    const point = { id: crypto.randomUUID(), name: name.trim(), instructions: '' }
    onChange({ ...value, points: [...value.points, point], selected: [...value.selected, point.id] })
    setName(''); setEditing(point.id)
  }
  return <div className="space-y-3">
    <div className="grid gap-2 sm:grid-cols-2">{value.points.map(point => {
      const checked = value.selected.includes(point.id)
      return <div key={point.id} className={`rounded-lg border p-3 ${checked ? 'border-primary/50 bg-primary/5' : 'border-border bg-background'}`}>
        <div className="flex items-center gap-2">
          <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 text-sm">
            <input type="checkbox" checked={checked} disabled={disabled || (!checked && value.selected.length >= 20)} onChange={e => onChange({ ...value, selected: e.target.checked ? [...value.selected, point.id] : value.selected.filter(id => id !== point.id) })} />
            <span className="break-words">{point.name}</span>
          </label>
          <Button type="button" variant="ghost" size="icon-xs" aria-label={t('review.edit_point', { name: point.name })} disabled={disabled} onClick={() => setEditing(editing === point.id ? null : point.id)}>{editing === point.id ? <Check className="size-3" /> : <Pencil className="size-3" />}</Button>
          {!point.metric && <Button type="button" variant="ghost" size="icon-xs" aria-label={t('review.remove_point', { name: point.name })} disabled={disabled} onClick={() => onChange({ ...value, points: value.points.filter(p => p.id !== point.id), selected: value.selected.filter(id => id !== point.id) })}><Trash2 className="size-3" /></Button>}
        </div>
        {editing === point.id ? <div className="mt-2 space-y-2">
          <Input aria-label={t('review.point_name')} maxLength={100} value={point.name} disabled={disabled} onChange={e => change(point.id, { name: e.target.value })} />
          <Textarea aria-label={t('review.point_instructions')} placeholder={t('review.point_instructions')} rows={2} maxLength={650} value={point.instructions} disabled={disabled} onChange={e => change(point.id, { instructions: e.target.value })} />
        </div> : point.instructions && <p className="mt-1 pl-5 text-xs leading-relaxed text-muted-foreground">{point.instructions}</p>}
      </div>
    })}</div>
    <div className="flex gap-2">
      <Input aria-label={t('review.custom_point')} placeholder={t('review.custom_point')} value={name} maxLength={100} disabled={disabled || value.points.length >= 30} onChange={e => setName(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') { e.preventDefault(); add() } }} />
      <Button type="button" variant="outline" disabled={disabled || !name.trim() || value.points.length >= 30 || value.selected.length >= 20} onClick={add}><Plus className="size-4" />{t('review.add')}</Button>
    </div>
    <p className="text-xs text-muted-foreground">{t('review.selection_count', { count: value.selected.length })}</p>
  </div>
}
