import { useTranslation } from 'react-i18next'
import { Check, ChevronDown, ChevronUp, ChevronsDownUp, ChevronsUpDown, Copy, Search, X } from 'lucide-react'
import type { ComponentType, ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { HighlightText, type JsonExpandMode } from '../JsonViewer'
import { useLogDetailUI } from './context'
import type { LogDetailSection } from './helpers'

export function CopyButton({ text, field, className }: { text: string; field: string; className?: string }) {
    const { t } = useTranslation()
    const { copiedField, copyToClipboard } = useLogDetailUI()
    const label = copiedField === field ? t('common.copied') : t('common.copy')
    // 不挂 tooltip:复制图标本身就是共识,气泡只是把"复制"两个字弹在
    // 你正要点的位置上挡手。aria-label 留着给读屏和键盘用户
    return (
        <Button
            variant="ghost"
            size="icon"
            onClick={(e) => {
                e.stopPropagation()
                copyToClipboard(text, field)
            }}
            className={cn("h-7 w-7 rounded-md hover:bg-primary/10 hover:text-primary transition-all", className)}
            aria-label={label}
        >
            {copiedField === field ? (
                <Check className="h-3.5 w-3.5 text-success" />
            ) : (
                <Copy className="h-3.5 w-3.5 text-muted-foreground" />
            )}
        </Button>
    )
}

export function SearchToggle({ active, onClick }: { active: boolean; onClick: () => void }) {
    const { t } = useTranslation()
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={onClick}
                    className={cn(
                        "h-7 w-7 rounded-md transition-all",
                        active ? "bg-primary/10 text-primary hover:bg-primary/15" : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    )}
                    aria-label={t('body_search.search')}
                >
                    <Search className="h-3.5 w-3.5" />
                </Button>
            </TooltipTrigger>
            <TooltipContent side="top" sideOffset={4}>{t('body_search.search')}</TooltipContent>
        </Tooltip>
    )
}

export function ViewToggle({
    value,
    options,
    onChange,
}: {
    value: string
    options: Array<{ value: string; label: string }>
    onChange: (value: string) => void
}) {
    return (
        <div className="flex items-center gap-1 rounded-lg bg-muted p-1">
            {options.map((option) => (
                <Button
                    key={option.value}
                    type="button"
                    variant={value === option.value ? 'secondary' : 'ghost'}
                    size="sm"
                    onClick={() => onChange(option.value)}
                    className={cn(
                        "h-6 rounded-md px-2 text-xs font-medium transition-all",
                        value === option.value
                            ? "border border-border bg-background text-foreground hover:bg-background"
                            : "text-muted-foreground hover:bg-background/70 hover:text-foreground"
                    )}
                >
                    {option.label}
                </Button>
            ))}
        </div>
    )
}

export function ExpandToggle({
    mode,
    onChange,
}: {
    mode: JsonExpandMode
    onChange: (mode: JsonExpandMode) => void
}) {
    const { t } = useTranslation()
    const willExpand = mode !== 'all'
    const label = willExpand ? t('log_detail.expand_all') : t('log_detail.collapse_all')
    return (
        <Tooltip>
            <TooltipTrigger asChild>
                <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    onClick={() => onChange(willExpand ? 'all' : 'none')}
                    className="h-7 w-7 rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
                    aria-label={label}
                >
                    {willExpand ? <ChevronsUpDown className="h-3.5 w-3.5" /> : <ChevronsDownUp className="h-3.5 w-3.5" />}
                </Button>
            </TooltipTrigger>
            <TooltipContent side="top" sideOffset={4}>{label}</TooltipContent>
        </Tooltip>
    )
}

export function SectionHeader({
    title,
    section,
    icon: Icon,
    extra,
}: {
    title: string
    section: LogDetailSection
    icon: ComponentType<{ className?: string }>
    extra?: ReactNode
}) {
    const { expandedSections, toggleSection } = useLogDetailUI()
    return (
        <div className="flex items-center justify-between gap-3 py-2.5">
            <button
                type="button"
                onClick={() => toggleSection(section)}
                className="group -mx-1 flex min-w-0 flex-1 items-center gap-2 rounded-lg px-1 py-0.5 text-left transition-colors"
            >
                <div className={cn(
                    "rounded-md p-1.5 transition-colors",
                    expandedSections[section]
                        ? "bg-primary/10 text-primary ring-1 ring-primary/20"
                        : "bg-muted text-muted-foreground group-hover:bg-secondary"
                )}>
                    <Icon className="h-3.5 w-3.5" />
                </div>
                <span className="text-xs font-medium text-foreground group-hover:text-primary transition-colors">
                    {title}
                </span>
                {expandedSections[section] ? (
                    <ChevronUp className="h-3.5 w-3.5 text-muted-foreground" />
                ) : (
                    <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                )}
            </button>
            {extra ? <div className="shrink-0">{extra}</div> : null}
        </div>
    )
}

export function RawBodyViewer({ text, searchTerm, maxMatches }: { text: string; searchTerm?: string; maxMatches: number }) {
    return (
        <pre className="whitespace-pre-wrap break-all text-xs font-mono leading-relaxed text-foreground select-text">
            {searchTerm ? <HighlightText text={text} searchTerm={searchTerm} maxMatches={maxMatches} /> : text}
        </pre>
    );
}

/** 概览卡的标签列 */
export function OverviewLabel({ children }: { children: ReactNode }) {
    return <dt className="whitespace-nowrap font-medium text-muted-foreground">{children}</dt>
}

export function UsageMetric({ label, value }: { label: string; value?: number }) {
    return (
        <span className="whitespace-nowrap">
            <span className="text-muted-foreground">{label}</span>{' '}
            <span className="font-mono font-medium text-foreground">
                {typeof value === 'number' ? value.toLocaleString() : '-'}
            </span>
        </span>
    )
}

export function BodySearchBar({
    searchTerm,
    onSearchTermChange,
    matchCount,
    truncated,
    pending,
    onNavigate,
    onClose,
}: {
    searchTerm: string;
    onSearchTermChange: (term: string) => void;
    matchCount: number;
    truncated: boolean;
    pending: boolean;
    onNavigate: (dir: 'prev' | 'next') => void;
    onClose: () => void;
}) {
    const { t } = useTranslation();
    const matchLabel = pending
        ? '…'
        : truncated
            ? t('body_search.match_count_truncated', { count: matchCount, defaultValue: '{{count}}+ matches' })
            : matchCount > 0
                ? t('body_search.match_count', { count: matchCount })
                : t('body_search.no_matches')
    return (
        <div className="flex items-center gap-2 border-b border-border px-3 py-1.5 bg-muted/20">
            <Search className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            <input
                autoFocus
                value={searchTerm}
                onChange={(e) => onSearchTermChange(e.target.value)}
                onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                        e.preventDefault()
                        if (!pending && matchCount > 0) onNavigate(e.shiftKey ? 'prev' : 'next')
                    }
                    if (e.key === 'Escape') { e.preventDefault(); onClose(); }
                }}
                placeholder={t('body_search.placeholder')}
                className="flex-1 min-w-0 bg-transparent text-xs outline-none placeholder:text-muted-foreground/50"
            />
            {searchTerm && (
                <span className="text-xs font-medium text-muted-foreground whitespace-nowrap">
                    {matchLabel}
                </span>
            )}
            <div className="flex items-center">
                <button
                    type="button"
                    onClick={() => onNavigate('prev')}
                    disabled={!searchTerm || pending || matchCount === 0}
                    className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                    <ChevronUp className="h-3 w-3" />
                </button>
                <button
                    type="button"
                    onClick={() => onNavigate('next')}
                    disabled={!searchTerm || pending || matchCount === 0}
                    className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-30 disabled:pointer-events-none"
                >
                    <ChevronDown className="h-3 w-3" />
                </button>
            </div>
            <button
                type="button"
                onClick={onClose}
                className="h-6 w-6 inline-flex items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
            >
                <X className="h-3 w-3" />
            </button>
        </div>
    );
}
