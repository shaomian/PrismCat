import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { fetchTraces, type TraceSummary } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

export function TraceSearch({ onSelect, value, onInput, disabled = false, excludeId, allowExplicit = true }: { onSelect: (id: string) => void; value?: string; onInput?: (value: string) => void; disabled?: boolean; excludeId?: string; allowExplicit?: boolean }) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<TraceSummary[]>([])
  const [error, setError] = useState('')
  const searchQuery = value ?? query
  useEffect(() => {
    let cancelled = false
    const timer = window.setTimeout(() => {
      void fetchTraces({ q: searchQuery, limit: 10, archived: false, mergeable: allowExplicit ? undefined : true }).then(response => {
        if (!cancelled) { setItems(response.traces ?? []); setError('') }
      }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    }, 250)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [searchQuery, allowExplicit])
  const choices = items.filter(item => item.trace_id !== excludeId && (allowExplicit || !item.sources?.includes('explicit')))
  return <div className="space-y-1">
    <Input aria-label={t('trace_manager.search_target')} placeholder={t('trace_manager.search_target')} value={searchQuery} disabled={disabled} onFocus={() => setOpen(true)} onChange={event => { setQuery(event.target.value); onInput?.(event.target.value); setOpen(true) }} />
    {open && <div className="max-h-56 overflow-auto rounded-md border border-border bg-card p-1 text-xs">
      <div className="flex items-center justify-between p-1 text-muted-foreground"><span>{error || t('trace_manager.choose_target')}</span><Button type="button" size="xs" variant="ghost" onClick={() => setOpen(false)}>{t('common.close')}</Button></div>
      {choices.map(item => <button type="button" key={item.trace_id} disabled={disabled} className="block w-full rounded p-2 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-primary" onClick={() => { onSelect(item.trace_id); setOpen(false); setQuery('') }}>
        <p className="truncate font-medium">{item.metadata?.name || t('trace_manager.unnamed')} · {item.request_count} · {new Date(item.last_time).toLocaleString()}</p>
        <p className="truncate font-mono text-muted-foreground">{item.trace_id} · {item.upstreams?.join(', ')}</p>
      </button>)}
      {!choices.length && !error && <p className="p-2 text-muted-foreground">{t('trace_manager.no_matches')}</p>}
    </div>}
  </div>
}
