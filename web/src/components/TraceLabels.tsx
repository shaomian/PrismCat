import { useId, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Popover } from 'radix-ui'
import { X } from 'lucide-react'
import { collectTraceLabels, maxTraceLabels } from '@/lib/trace-labels'
import { cn } from '@/lib/utils'
import { Input } from '@/components/ui/input'

export function TraceLabelEditor({ labels, input, onChange, onInputChange, disabled }: { labels: string[]; input: string; onChange: (labels: string[]) => void; onInputChange: (input: string) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  const id = useId()
  const composing = useRef(false)
  const tooMany = collectTraceLabels(labels, input).length > maxTraceLabels
  const commit = (text: string) => {
    const next = collectTraceLabels(labels, text)
    if (next.length > maxTraceLabels) { onInputChange(text); return }
    onChange(next)
    onInputChange('')
  }
  const type = (text: string) => {
    if (!composing.current && /[,，\r\n]/.test(text)) commit(text)
    else onInputChange(text)
  }
  return <div className="space-y-1.5 text-xs">
    <label htmlFor={id}>{t('trace_manager.labels')}</label>
    <div className={cn('flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1.5 focus-within:ring-2 focus-within:ring-ring/50', disabled && 'opacity-60')}>
      {labels.map(label => <span key={label} className="inline-flex max-w-full items-center gap-1 rounded bg-muted py-0.5 pl-2 pr-1 text-xs text-muted-foreground">
        <span className="min-w-0 break-all">{label}</span><button type="button" disabled={disabled} aria-label={t('trace_labels.remove', { label })} title={t('trace_labels.remove', { label })} className="inline-flex size-5 shrink-0 items-center justify-center rounded hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary" onClick={() => onChange(labels.filter(value => value !== label))}><X className="size-3" /></button>
      </span>)}
      <Input id={id} value={input} maxLength={4000} disabled={disabled} aria-describedby={`${id}-hint`} aria-invalid={tooMany} placeholder={t('trace_labels.input_placeholder')}
        className="h-7 min-w-28 flex-1 border-0 bg-transparent p-0 text-sm shadow-none focus-visible:ring-0" onChange={event => type(event.target.value)}
        onCompositionStart={() => { composing.current = true }} onCompositionEnd={event => { composing.current = false; type(event.currentTarget.value) }}
        onKeyDown={event => { if (event.key === 'Enter' && !composing.current && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229) { event.preventDefault(); commit(input) } }} />
    </div>
    <p id={`${id}-hint`} className={cn('text-xs', tooMany ? 'text-danger' : 'text-muted-foreground')} role={tooMany ? 'alert' : undefined}>{tooMany ? t('trace_labels.limit', { count: maxTraceLabels }) : t('trace_labels.input_hint')}</p>
  </div>
}

export function TraceLabels({ labels, limit, onSelect, activeLabel, className }: { labels: readonly string[]; limit?: number; onSelect?: (label: string) => void; activeLabel?: string; className?: string }) {
  const { t } = useTranslation()
  if (!labels.length) return null
  const ordered = activeLabel && labels.includes(activeLabel) ? [activeLabel, ...labels.filter(label => label !== activeLabel)] : labels
  const visible = limit === undefined ? ordered : ordered.slice(0, limit)
  const hidden = limit === undefined ? [] : ordered.slice(limit)
  const chip = (label: string, full = false) => {
    const classes = cn('inline-flex min-w-0 items-center rounded bg-muted px-1.5 py-0.5 text-xs font-normal text-muted-foreground', full ? 'max-w-full' : 'max-w-24', onSelect && 'relative z-10 hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary', activeLabel === label && 'bg-primary/10 text-primary')
    return onSelect ? <button key={label} type="button" className={classes} title={label} aria-pressed={activeLabel === label} aria-label={t(activeLabel === label ? 'trace_labels.clear_filter' : 'trace_labels.filter_by', { label })} onClick={event => { event.stopPropagation(); onSelect(label) }}><span className={full ? 'break-all text-left' : 'truncate'}>{label}</span></button> : <span key={label} className={classes} title={label}><span className={full ? 'break-all' : 'truncate'}>{label}</span></span>
  }
  return <div className={cn('flex min-w-0 flex-wrap items-center gap-1', className)}>
    {visible.map(label => chip(label, limit === undefined))}
    {hidden.length > 0 && <Popover.Root>
      <Popover.Trigger asChild><button type="button" className="relative z-10 shrink-0 rounded px-1 py-0.5 text-xs text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary" title={hidden.join(', ')} aria-label={t('trace_labels.more', { count: hidden.length })} onClick={event => event.stopPropagation()}>+{hidden.length}</button></Popover.Trigger>
      <Popover.Portal><Popover.Content aria-label={t('trace_manager.labels')} align="start" sideOffset={5} collisionPadding={12} className="z-50 max-h-64 w-64 max-w-[calc(100vw-2rem)] overflow-y-auto rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md" onClick={event => event.stopPropagation()}>
        <div className="flex flex-wrap gap-1.5">{labels.map(label => chip(label, true))}</div>
      </Popover.Content></Popover.Portal>
    </Popover.Root>}
  </div>
}
