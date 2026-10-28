import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Popover } from 'radix-ui'
import { Scissors } from 'lucide-react'
import type { TraceOrigin } from '@/lib/experiments'
import { formatDate } from '@/lib/utils'

export function TraceSourceBadges({ sources, origin }: { sources?: string[]; origin?: TraceOrigin }) {
  const { t } = useTranslation()
  return <div className="flex min-w-0 flex-wrap items-center gap-1 text-xs text-muted-foreground">
    {origin?.kind === 'manual_split' ? <TraceOriginBadge origin={origin} /> : sources?.map(source => <span key={source} className="inline-flex h-[18px] items-center whitespace-nowrap rounded border border-border px-1">{t(`correlation.sources.${source}`)}</span>)}
  </div>
}

// One lightweight system badge, with the same preview for mouse, keyboard and
// touch. Its history is already in the page response; opening it never fetches.
export function TraceOriginBadge({ origin }: { origin: TraceOrigin }) {
  const { t, i18n } = useTranslation()
  const id = useId()
  const [open, setOpen] = useState(false)
  const button = useRef<HTMLButtonElement>(null)
  const content = useRef<HTMLDivElement>(null)
  const timer = useRef<number | null>(null)
  const cancelClose = () => { if (timer.current !== null) window.clearTimeout(timer.current); timer.current = null }
  useEffect(() => () => { if (timer.current !== null) window.clearTimeout(timer.current) }, [])
  const closeSoon = () => {
    cancelClose()
    if (document.activeElement === button.current || content.current?.contains(document.activeElement)) return
    timer.current = window.setTimeout(() => setOpen(false), 150)
  }
  return <Popover.Root open={open} onOpenChange={setOpen}>
    <Popover.Anchor asChild><button ref={button} type="button" aria-label={t('trace_origin.label')} aria-expanded={open} aria-haspopup="dialog" aria-controls={open ? id : undefined}
      className="inline-flex h-[18px] shrink-0 items-center gap-1 whitespace-nowrap rounded border border-border px-1 text-xs text-muted-foreground outline-none transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-primary [@media(pointer:coarse)]:min-h-8"
      onPointerEnter={event => { if (event.pointerType === 'mouse') { cancelClose(); setOpen(true) } }} onPointerLeave={closeSoon}
      onFocus={() => { cancelClose(); setOpen(true) }} onBlur={event => { if (!content.current?.contains(event.relatedTarget)) { cancelClose(); setOpen(false) } }}
      onClick={event => { event.stopPropagation(); cancelClose(); setOpen(true) }}>
      <Scissors className="size-3" aria-hidden="true" />{t('trace_origin.label')}
    </button></Popover.Anchor>
    <Popover.Portal><Popover.Content ref={content} id={id} aria-label={t('trace_origin.details')} align="start" side="bottom" sideOffset={6} collisionPadding={12}
      className="z-50 w-80 max-w-[calc(100vw-2rem)] space-y-3 rounded-md border border-border bg-popover p-3 text-xs text-popover-foreground shadow-md"
      onOpenAutoFocus={event => event.preventDefault()} onCloseAutoFocus={event => event.preventDefault()}
      onInteractOutside={event => { if (button.current?.contains(event.target as Node)) event.preventDefault() }}
      onPointerEnter={cancelClose} onPointerLeave={closeSoon} onClick={event => event.stopPropagation()}>
      <p className="font-medium">{t('trace_origin.details')}</p>
      <dl className="space-y-2">
        <div className="space-y-1"><dt className="text-muted-foreground">{t('trace_origin.source')}</dt><dd className="space-y-1">{origin.source_trace_ids.map(source => <code key={source} className="block break-all">{source}</code>)}</dd></div>
        <div className="flex flex-wrap justify-between gap-2"><dt className="text-muted-foreground">{t('trace_origin.initial_count')}</dt><dd className="tabular-nums">{t('correlation.requests_count', { count: origin.request_count })}</dd></div>
        <div className="flex flex-wrap justify-between gap-2"><dt className="text-muted-foreground">{t('trace_origin.time')}</dt><dd>{formatDate(new Date(origin.created_at).toISOString(), i18n.language)}</dd></div>
      </dl>
      <p className="leading-relaxed text-muted-foreground">{t('trace_origin.hint')}</p>
    </Popover.Content></Popover.Portal>
  </Popover.Root>
}
