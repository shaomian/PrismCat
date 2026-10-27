import { useId } from 'react'
import { useTranslation } from 'react-i18next'
import { Popover } from 'radix-ui'
import { ListFilter, X } from 'lucide-react'
import type { Upstream } from '@/lib/api'
import { countTraceFilters, defaultTraceFilters, tracePeriods, type TraceListFilters, type TracePeriod } from '@/lib/trace-filters'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

export function TraceFilters({ value, upstreams, onChange }: {
  value: TraceListFilters
  upstreams: Upstream[]
  onChange: (next: TraceListFilters) => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const count = countTraceFilters(value)
  const change = (patch: Partial<TraceListFilters>) => onChange({ ...value, ...patch, offset: 0 })
  return <Popover.Root>
    <Popover.Trigger asChild>
      <Button type="button" variant="outline" className={cn('h-9 shrink-0 gap-1.5 text-xs', count > 0 && 'border-primary/40 text-primary')} aria-label={t(count ? 'trace_filters.open_active' : 'trace_filters.open', { count })}>
        <ListFilter className="size-4" />{t('trace_filters.open')}
        {count > 0 && <span aria-hidden="true" className="min-w-5 rounded-full bg-primary/10 px-1.5 text-center text-xs tabular-nums">{count}</span>}
      </Button>
    </Popover.Trigger>
    <Popover.Portal>
      <Popover.Content align="end" sideOffset={8} collisionPadding={12} aria-labelledby={`${id}-title`} className="z-50 w-80 max-w-[calc(100vw-1.5rem)] rounded-lg border border-border bg-popover p-4 text-popover-foreground shadow-md outline-none">
        <div className="mb-4 flex items-center justify-between gap-3">
          <h3 id={`${id}-title`} className="text-sm font-medium">{t('trace_filters.title')}</h3>
          <Popover.Close asChild><Button type="button" size="icon-xs" variant="ghost" aria-label={t('common.close')}><X className="size-3.5" /></Button></Popover.Close>
        </div>
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <div className="min-w-0 space-y-2">
              <Label htmlFor={`${id}-period`} className="text-xs text-muted-foreground">{t('trace_filters.period')}</Label>
              <Select value={value.period} onValueChange={period => change({ period: period as TracePeriod })}>
                <SelectTrigger id={`${id}-period`} className="h-9 w-full min-w-0 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent>{tracePeriods.map(period => <SelectItem key={period} value={period}>{t(`trace_filters.periods.${period}`)}</SelectItem>)}</SelectContent>
              </Select>
            </div>
            <div className="min-w-0 space-y-2">
              <Label htmlFor={`${id}-archive`} className="text-xs text-muted-foreground">{t('task_management.archive_filter')}</Label>
              <Select value={String(value.archived)} onValueChange={archived => change({ archived: archived === 'true' })}>
                <SelectTrigger id={`${id}-archive`} className="h-9 w-full min-w-0 text-xs"><SelectValue /></SelectTrigger>
                <SelectContent><SelectItem value="false">{t('task_management.unarchived')}</SelectItem><SelectItem value="true">{t('task_management.archived')}</SelectItem></SelectContent>
              </Select>
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor={`${id}-entry`} className="text-xs text-muted-foreground">{t('trace_filters.entry')}</Label>
            <Select value={value.upstream || '_all'} onValueChange={upstream => change({ upstream: upstream === '_all' ? '' : upstream })}>
              <SelectTrigger id={`${id}-entry`} className="h-9 w-full min-w-0 text-xs"><SelectValue /></SelectTrigger>
              <SelectContent><SelectItem value="_all">{t('trace_filters.all_entries')}</SelectItem>{upstreams.map(upstream => <SelectItem key={upstream.name} value={upstream.name}>{upstream.name}</SelectItem>)}</SelectContent>
            </Select>
          </div>
          <fieldset className="space-y-2">
            <legend className="text-xs text-muted-foreground">{t('trace_filters.conditions')}</legend>
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {([['hasError', 'errors'], ['pinned', 'pinned'], ['todo', 'todo']] as const).map(([field, label]) => <label key={field} className="flex min-h-8 cursor-pointer items-center gap-1.5 text-xs"><input type="checkbox" className="accent-primary" checked={value[field]} onChange={event => change({ [field]: event.target.checked })} />{t(`trace_manager.views.${label}`)}</label>)}
            </div>
          </fieldset>
        </div>
        <div className="mt-4 border-t border-border pt-3">
          <Button type="button" variant="ghost" size="sm" className="h-8 px-2 text-xs text-muted-foreground" disabled={!count} onClick={() => onChange(defaultTraceFilters(value.query))}>{t('trace_filters.clear')}</Button>
        </div>
      </Popover.Content>
    </Popover.Portal>
  </Popover.Root>
}
