import { createContext, useCallback, useContext, useEffect, useId, useMemo, useRef, useState, type ComponentProps, type ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { Popover, Slot } from 'radix-ui'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

interface PreviewContent { name: string; summary?: string; labels?: string[]; details?: string; facts?: { label: string; value: string }[] }
interface Preview extends PreviewContent { anchor: HTMLElement; route: string; trigger: string }
interface PreviewContext { schedule: (anchor: HTMLElement, content: PreviewContent, trigger: string) => void; cancel: () => void; activeTrigger?: string; id: string }
const Context = createContext<PreviewContext | null>(null)
const excerpt = (value = '', limit = 220) => { const text = value.trim().replace(/\s+/g, ' '); return text.length > limit ? `${text.slice(0, limit)}…` : text }

export function TaskPreviewProvider({ children }: { children: ReactNode }) {
  const { key: route } = useLocation()
  const id = useId()
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const [preview, setPreview] = useState<Preview | null>(null)
  const cancel = useCallback(() => {
    if (timer.current) clearTimeout(timer.current)
    timer.current = null
    setPreview(null)
  }, [])
  const schedule = useCallback((anchor: HTMLElement, content: PreviewContent, trigger: string) => {
    cancel()
    if (!window.matchMedia('(hover: hover) and (pointer: fine)').matches) return
    timer.current = setTimeout(() => {
      timer.current = null
      if (anchor.isConnected) setPreview({ ...content, name: excerpt(content.name, 300), summary: excerpt(content.summary), details: excerpt(content.details, 180), labels: content.labels?.slice(0, 5).map(label => excerpt(label, 40)), anchor, route, trigger })
    }, 550)
  }, [cancel, route])
  useEffect(() => {
    window.addEventListener('scroll', cancel, true)
    window.addEventListener('resize', cancel)
    return () => { window.removeEventListener('scroll', cancel, true); window.removeEventListener('resize', cancel); cancel() }
  }, [cancel, route])
  const active = preview?.route === route && preview.anchor.isConnected ? preview : null
  const context = useMemo(() => ({ schedule, cancel, activeTrigger: active?.trigger, id }), [schedule, cancel, active?.trigger, id])
  return <Context.Provider value={context}>
    <div className="contents" onPointerDownCapture={cancel}>{children}</div>
    {active && <Popover.Root open onOpenChange={open => { if (!open) cancel() }}>
      <Popover.Anchor virtualRef={{ current: active.anchor }} />
      <Popover.Portal><Popover.Content id={id} role="tooltip" align="start" side="bottom" sideOffset={8} collisionPadding={14} onOpenAutoFocus={event => event.preventDefault()} onCloseAutoFocus={event => event.preventDefault()} onEscapeKeyDown={cancel} className="pointer-events-none z-40 w-80 max-w-[calc(100vw-2rem)] space-y-2 rounded-lg border border-border bg-popover p-3.5 text-popover-foreground shadow-md">
        <p className="break-words text-sm font-medium">{active.name}</p>
        {active.summary && <p className="line-clamp-3 break-words text-xs leading-relaxed text-muted-foreground">{active.summary}</p>}
        {!!active.labels?.length && <div className="flex flex-wrap gap-1.5">{active.labels.map((label, index) => <span key={`${label}-${index}`} className="max-w-full truncate rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">{label}</span>)}</div>}
        {active.details && <p className="break-words text-xs text-muted-foreground">{active.details}</p>}
        {!!active.facts?.length && <dl className="space-y-1.5 text-xs">
          {active.facts.map(fact => <div key={fact.label} className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3">
            <dt className="text-muted-foreground">{fact.label}</dt>
            <dd className="min-w-0 whitespace-pre-line break-words [overflow-wrap:anywhere]">{fact.value}</dd>
          </div>)}
        </dl>}
      </Popover.Content></Popover.Portal>
    </Popover.Root>}
  </Context.Provider>
}

export function TaskPreviewTrigger({ children, disabled, ...content }: PreviewContent & { children: ReactNode; disabled?: boolean }) {
  const context = useContext(Context)
  const trigger = useId()
  const cancel = context?.cancel
  useEffect(() => () => cancel?.(), [cancel])
  if (disabled || !context) return children
  return <Slot.Root onPointerEnter={event => { if (event.pointerType === 'mouse') context.schedule(event.currentTarget, content, trigger) }} onPointerLeave={context.cancel} onFocus={event => context.schedule(event.currentTarget, content, trigger)} onBlur={context.cancel} onKeyDown={event => { if (event.key === 'Escape') context.cancel() }} aria-describedby={context.activeTrigger === trigger ? context.id : undefined}>{children}</Slot.Root>
}

export function TaskActionBar({ children, className }: { children: ReactNode; className?: string }) {
  const context = useContext(Context)
  return <div data-task-actions className={cn('task-actions inline-flex shrink-0 items-center gap-0.5', className)} onPointerEnter={context?.cancel} onFocusCapture={context?.cancel} onClick={event => event.stopPropagation()}>{children}</div>
}

export function TaskIconButton({ label, className, ...props }: ComponentProps<typeof Button> & { label: string }) {
  return <Button {...props} type={props.type ?? 'button'} variant={props.variant ?? 'ghost'} size={props.size ?? 'icon-sm'} aria-label={label} title={label} className={cn('text-muted-foreground hover:text-foreground', className)} />
}
