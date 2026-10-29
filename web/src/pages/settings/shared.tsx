import { useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, CircleHelp } from 'lucide-react'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { customProxyPlaceholder, outboundProxyMode, type OutboundProxyMode } from './helpers'

type FieldBlockProps = {
    label: string
    hint?: string
    htmlFor?: string
    unit?: string
    children: ReactNode
}

type ToggleSettingProps = {
    label: string
    description: string
    checked: boolean
    onCheckedChange: (checked: boolean) => void
}

export function OutboundProxyControl({
    value,
    onChange,
    t,
    className,
}: {
    value: string
    onChange: (value: string) => void
    t: (key: string) => string
    className?: string
}) {
    const mode = outboundProxyMode(value)
    const [customValue, setCustomValue] = useState(
        mode === 'custom' ? value : customProxyPlaceholder,
    )

    return (
        <div
            className={cn(
                "relative flex h-9 overflow-hidden rounded-md border border-input bg-background transition-shadow focus-within:ring-2 focus-within:ring-ring/50",
                className,
            )}
        >
            {mode === 'custom' && (
                <Input
                    value={value}
                    onChange={e => {
                        setCustomValue(e.target.value)
                        onChange(e.target.value)
                    }}
                    placeholder={customProxyPlaceholder}
                    aria-label={t('upstream_manager.outbound_proxy_custom_address')}
                    className="h-full min-w-0 flex-1 rounded-none border-0 bg-transparent px-3 pr-11 font-mono text-sm shadow-none focus-visible:ring-0 focus-visible:ring-offset-0"
                />
            )}
            <Select
                value={mode}
                onValueChange={(nextMode: OutboundProxyMode) => {
                    if (nextMode === 'custom') {
                        onChange(mode === 'custom' ? value : customValue)
                        return
                    }
                    onChange(nextMode)
                }}
            >
                <SelectTrigger
                    className={cn(
                        "h-full rounded-none border-0 bg-transparent text-sm shadow-none",
                        mode === 'custom'
                            ? "absolute inset-y-0 right-0 z-10 w-10 justify-center px-0 hover:bg-muted/40"
                            : "w-full",
                    )}
                >
                    {mode === 'custom' ? (
                        <span className="sr-only">
                            <SelectValue />
                        </span>
                    ) : (
                        <SelectValue />
                    )}
                </SelectTrigger>
                <SelectContent>
                    <SelectItem value="env">{t('upstream_manager.outbound_proxy_env')}</SelectItem>
                    <SelectItem value="direct">{t('upstream_manager.outbound_proxy_direct')}</SelectItem>
                    <SelectItem value="custom">{t('upstream_manager.outbound_proxy_custom')}</SelectItem>
                </SelectContent>
            </Select>
        </div>
    )
}

export function InfoTooltip({ content }: { content: string }) {
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <button
                    type="button"
                    onClick={event => event.stopPropagation()}
                    className="inline-flex h-4 w-4 items-center justify-center rounded-md text-muted-foreground/85 transition-colors hover:text-foreground"
                    aria-label="More info"
                >
                    <CircleHelp className="h-3.5 w-3.5" />
                </button>
            </TooltipTrigger>
            <TooltipContent sideOffset={6} className="max-w-xs px-3 py-2 text-xs leading-6">
                {content}
            </TooltipContent>
        </Tooltip>
    )
}

export function FieldBlock({ label, hint, htmlFor, unit, children }: FieldBlockProps) {
    return (
        <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2.5">
                <Label
                    htmlFor={htmlFor}
                    className="cursor-pointer select-none text-sm font-medium text-foreground hover:text-foreground/90 transition-colors"
                >
                    {label}
                </Label>
                {hint && <InfoTooltip content={hint} />}
                {unit && (
                    <span className="rounded-md bg-muted/60 px-2 py-0.5 text-xs font-medium text-muted-foreground">
                        {unit}
                    </span>
                )}
            </div>
            {children}
        </div>
    )
}

export function AdvancedSettings({
    children,
    open: controlledOpen,
    onOpenChange,
    defaultOpen = false,
}: {
    children: ReactNode
    open?: boolean
    onOpenChange?: (open: boolean) => void
    defaultOpen?: boolean
}) {
    const { t } = useTranslation()
    const [internalOpen, setInternalOpen] = useState(defaultOpen)
    const open = controlledOpen ?? internalOpen
    const setOpen = (nextOpen: boolean) => {
        if (controlledOpen === undefined) setInternalOpen(nextOpen)
        onOpenChange?.(nextOpen)
    }
    return (
        <section className="rounded-md border border-input bg-muted/20">
            <button
                type="button"
                aria-expanded={open}
                onClick={() => setOpen(!open)}
                className="flex w-full cursor-pointer items-center justify-between gap-4 px-4 py-3 text-left"
            >
                <div className="flex min-w-0 items-center gap-2">
                    <div className="text-sm font-medium text-foreground">
                        {t('upstream_manager.advanced_settings')}
                    </div>
                    <InfoTooltip content={t('upstream_manager.advanced_settings_hint')} />
                </div>
                <ChevronDown className={cn(
                    "h-4 w-4 shrink-0 text-muted-foreground transition-transform",
                    open && "rotate-180",
                )} />
            </button>
            {open && (
                <div className="space-y-6 border-t border-input px-4 py-5">
                    {children}
                </div>
            )}
        </section>
    )
}

export function AdvancedSettingsGroup({
    title,
    description,
    children,
    card = false,
    divider = false,
}: {
    title: string
    description?: string
    children: ReactNode
    /** 独立成卡片。嵌在别的面板里时别用 —— 会叠出第三层边框 */
    card?: boolean
    /** 用一条分隔线起新段,替代再套一层边框 */
    divider?: boolean
}) {
    return (
        <div className={cn(
            "space-y-4",
            card && "rounded-md border border-input bg-background p-4",
            divider && "border-t border-input pt-6",
        )}>
            <div>
                <div className="text-xs font-semibold text-foreground/65">{title}</div>
                {description && <p className="mt-1.5 text-xs leading-5 text-muted-foreground">{description}</p>}
            </div>
            {children}
        </div>
    )
}

export function ToggleSetting({
    label,
    description,
    checked,
    onCheckedChange,
}: ToggleSettingProps) {
    const id = useId()
    return (
        <div className="flex items-center gap-3">
            <Switch
                id={id}
                checked={checked}
                onCheckedChange={onCheckedChange}
                className="shrink-0 data-[state=unchecked]:bg-border/60"
            />
            <div className="flex items-center gap-1.5">
                <Label
                    htmlFor={id}
                    className="cursor-pointer select-none text-sm font-medium text-foreground hover:text-foreground transition-colors"
                >
                    {label}
                </Label>
                <InfoTooltip content={description} />
            </div>
        </div>
    )
}

export function SettingSection({
    title,
    description,
    action,
    children,
}: {
    title: string
    description?: string
    action?: ReactNode
    children: ReactNode
}) {
    return (
        <section className="overflow-hidden rounded-lg border border-input bg-card">
            <div className="flex items-start justify-between gap-4 border-b border-input px-6 py-4">
                <div className="min-w-0">
                    <h3 className="text-sm font-semibold text-foreground">{title}</h3>
                    {description && (
                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">{description}</p>
                    )}
                </div>
                {action && <div className="shrink-0">{action}</div>}
            </div>
            <div className="px-6 py-6">{children}</div>
        </section>
    )
}
