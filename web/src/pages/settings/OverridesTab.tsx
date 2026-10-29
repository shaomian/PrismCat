import { useState, type Dispatch, type SetStateAction } from 'react'
import {
    AlertTriangle,
    ChevronDown,
    Copy,
    ExternalLink,
    Eye,
    EyeOff,
    FileCode,
    Plus,
    Trash2,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import type { Upstream } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
    getBindingRuleNames,
    getOverrideRuleEnabled,
    getOverrideRuleName,
    isSensitiveHeaderName,
    requestOverrideExample,
    usageExtractionExample,
    type BindingKind,
    type HeaderOp,
    type OverrideBinding,
    type OverrideRuleObject,
    type RuleTab,
    type TranslationFn,
} from './helpers'
import {
    FieldBlock,
    SettingSection,
    ToggleSetting,
} from './shared'

type RuleRuntimeStatus =
    | { kind: 'active'; enabledUpstreams: string[]; disabledUpstreams: string[]; inactiveTargets: string[] }
    | { kind: 'blocked'; reason: 'global' | 'rule' | 'bindings'; enabledUpstreams: string[]; disabledUpstreams: string[]; inactiveTargets: string[] }
    | { kind: 'inactive'; inactiveTargets: string[] }
    | { kind: 'unbound' }

function collectRuleBindingLocations(
    upstreams: Upstream[],
    legacyBindings: Record<string, OverrideBinding>,
    kind: BindingKind,
): { active: string[]; disabled: string[]; inactive: string[] } {
    const active: string[] = []
    const disabled: string[] = []
    const inactive: string[] = []
    for (const upstream of upstreams) {
        const targets = upstream.targets || {}
        if (Object.keys(targets).length === 0) {
            const binding = legacyBindings[upstream.name]
            if (!binding) continue
            const label = upstream.name
            if (binding.enabled) active.push(label)
            else disabled.push(label)
            continue
        }

        for (const [targetName, target] of Object.entries(targets)) {
            const binding = target[kind]
            if (!binding) continue
            const label = `${upstream.name}/${targetName}`
            if (targetName === upstream.active_target) {
                if (binding.enabled) active.push(label)
                else disabled.push(label)
            } else {
                inactive.push(label)
            }
        }
    }
    return { active, disabled, inactive }
}

function getRuleRuntimeStatus(
    rule: OverrideRuleObject,
    upstreams: Upstream[],
    bindings: Record<string, OverrideBinding>,
    globalEnabled: boolean,
): RuleRuntimeStatus {
    const ruleName = getOverrideRuleName(rule, '')
    if (!ruleName) return { kind: 'unbound' }

    const locations = collectRuleBindingLocations(upstreams, bindings, 'request_overrides')
    const enabledUpstreams = locations.active.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = targetName
            ? upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
            : bindings[upstreamName]
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })
    const disabledUpstreams = locations.disabled.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = targetName
            ? upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
            : bindings[upstreamName]
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })
    const inactiveTargets = locations.inactive.filter(name => {
        const [upstreamName, targetName] = name.split('/')
        const binding = upstreams.find(upstream => upstream.name === upstreamName)?.targets?.[targetName]?.request_overrides
        return Boolean(binding && getBindingRuleNames(binding).includes(ruleName))
    })

    if (enabledUpstreams.length + disabledUpstreams.length + inactiveTargets.length === 0) {
        return { kind: 'unbound' }
    }
    if (enabledUpstreams.length + disabledUpstreams.length === 0) {
        return { kind: 'inactive', inactiveTargets }
    }
    if (!globalEnabled) {
        return { kind: 'blocked', reason: 'global', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    if (!getOverrideRuleEnabled(rule)) {
        return { kind: 'blocked', reason: 'rule', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    if (enabledUpstreams.length === 0) {
        return { kind: 'blocked', reason: 'bindings', enabledUpstreams, disabledUpstreams, inactiveTargets }
    }
    return { kind: 'active', enabledUpstreams, disabledUpstreams, inactiveTargets }
}

function formatUpstreamList(
    enabled: string[],
    disabled: string[],
    t: (key: string, options?: Record<string, unknown>) => string,
): string {
    const summarize = (names: string[]) => {
        if (names.length <= 2) return names.join(', ')
        return `${names.slice(0, 2).join(', ')} +${names.length - 2}`
    }
    const parts: string[] = []
    if (enabled.length > 0) parts.push(`→ ${summarize(enabled)}`)
    if (disabled.length > 0) parts.push(t('settings.rule_status_disabled_upstreams', { list: summarize(disabled) }))
    return parts.join('  ·  ')
}

function formatInactiveTargets(
    targets: string[],
    t: (key: string, options?: Record<string, unknown>) => string,
): string {
    if (targets.length === 0) return ''
    const summarize = targets.length <= 2 ? targets.join(', ') : `${targets.slice(0, 2).join(', ')} +${targets.length - 2}`
    return t('settings.rule_status_inactive_targets', { list: summarize })
}

function HeaderValueInput({
    value,
    onChange,
    placeholder,
    disabled,
    sensitive,
    showLabel,
    hideLabel,
}: {
    value: string
    onChange: (value: string) => void
    placeholder: string
    disabled: boolean
    sensitive: boolean
    showLabel: string
    hideLabel: string
}) {
    const [revealed, setRevealed] = useState(false)
    const masked = sensitive && !revealed

    return (
        <div className="relative min-w-0 flex-[2]">
            <Input
                type={masked ? 'password' : 'text'}
                value={value}
                onChange={event => onChange(event.target.value)}
                placeholder={placeholder}
                disabled={disabled}
                autoComplete="off"
                className={cn(
                    "h-9 w-full rounded-lg border-input bg-background text-xs disabled:opacity-40",
                    sensitive && !disabled && "pr-9",
                )}
            />
            {sensitive && !disabled && (
                <button
                    type="button"
                    onClick={() => setRevealed(current => !current)}
                    className="absolute inset-y-0 right-0 flex w-9 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
                    aria-label={revealed ? hideLabel : showLabel}
                >
                    {revealed ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                </button>
            )}
        </div>
    )
}

type OverridesTabProps = {
    t: TranslationFn
    activeRuleTab: RuleTab
    setActiveRuleTab: Dispatch<SetStateAction<RuleTab>>
    requestOverridesEnabled: boolean
    setRequestOverridesEnabled: Dispatch<SetStateAction<boolean>>
    overrideMaxBodyKB: number
    setOverrideMaxBodyKB: Dispatch<SetStateAction<number>>
    overrideRulesText: string
    setOverrideRulesText: Dispatch<SetStateAction<string>>
    overrideRulesParse: { rules: OverrideRuleObject[]; error: string }
    overrideRuleObjects: OverrideRuleObject[]
    selectedOverrideRuleIndex: number
    selectedOverrideRuleName: string
    selectedOverrideRuleError: string
    selectedOverrideRuleNameError: string
    selectedOverrideMatchError: string
    selectedOverridePatchError: string
    selectedOverrideRulePreview: string
    selectedOverrideMatchText: string
    selectedOverridePatchText: string
    selectedOverrideRuleText: string
    selectedRuleAdvancedOpen: boolean
    setSelectedRuleAdvancedOpen: Dispatch<SetStateAction<boolean>>
    advancedRulesOpen: boolean
    setAdvancedRulesOpen: Dispatch<SetStateAction<boolean>>
    overrideBindings: Record<string, OverrideBinding>
    upstreams: Upstream[]
    sensitiveHeaders: string
    usageExtractionEnabled: boolean
    setUsageExtractionEnabled: Dispatch<SetStateAction<boolean>>
    usageRulesText: string
    setUsageRulesText: Dispatch<SetStateAction<string>>
    usageRulesParse: { rules: OverrideRuleObject[]; error: string }
    usageRuleObjects: OverrideRuleObject[]
    selectedUsageRuleIndex: number
    selectedUsageRuleText: string
    selectedUsageRuleError: string
    usageAdvancedRulesOpen: boolean
    setUsageAdvancedRulesOpen: Dispatch<SetStateAction<boolean>>
    usageBindings: Record<string, OverrideBinding>
    handleAddOverrideExample: () => void
    handleAddOverrideRule: () => void
    handleSelectOverrideRule: (index: number) => void
    handleDuplicateOverrideRule: (index: number) => void
    handleDeleteOverrideRule: (index: number) => void
    handleToggleOverrideRule: (index: number, enabled: boolean) => void
    handleOverrideRuleNameChange: (value: string) => void
    handleCommitOverrideRuleName: () => void
    handleOverrideMatchTextChange: (value: string) => void
    handleOverridePatchTextChange: (value: string) => void
    handleOverrideRuleTextChange: (value: string) => void
    handleAddHeaderOp: () => void
    handleUpdateHeaderOp: (hIndex: number, field: keyof HeaderOp, value: string) => void
    handleRemoveHeaderOp: (hIndex: number) => void
    getSelectedRuleHeaders: () => HeaderOp[]
    handleCopy: (value: string) => void
    handleMergeDefaultUsageRules: () => void
    handleAddUsageRule: () => void
    handleSelectUsageRule: (index: number) => void
    handleToggleUsageRule: (index: number, enabled: boolean) => void
    handleDuplicateUsageRule: (index: number) => void
    handleDeleteUsageRule: (index: number) => void
    handleUsageRuleTextChange: (value: string) => void
}

export function OverridesTab({
    t,
    activeRuleTab,
    setActiveRuleTab,
    requestOverridesEnabled,
    setRequestOverridesEnabled,
    overrideMaxBodyKB,
    setOverrideMaxBodyKB,
    overrideRulesText,
    setOverrideRulesText,
    overrideRulesParse,
    overrideRuleObjects,
    selectedOverrideRuleIndex,
    selectedOverrideRuleName,
    selectedOverrideRuleError,
    selectedOverrideRuleNameError,
    selectedOverrideMatchError,
    selectedOverridePatchError,
    selectedOverrideRulePreview,
    selectedOverrideMatchText,
    selectedOverridePatchText,
    selectedOverrideRuleText,
    selectedRuleAdvancedOpen,
    setSelectedRuleAdvancedOpen,
    advancedRulesOpen,
    setAdvancedRulesOpen,
    overrideBindings,
    upstreams,
    sensitiveHeaders,
    usageExtractionEnabled,
    setUsageExtractionEnabled,
    usageRulesText,
    setUsageRulesText,
    usageRulesParse,
    usageRuleObjects,
    selectedUsageRuleIndex,
    selectedUsageRuleText,
    selectedUsageRuleError,
    usageAdvancedRulesOpen,
    setUsageAdvancedRulesOpen,
    usageBindings,
    handleAddOverrideExample,
    handleAddOverrideRule,
    handleSelectOverrideRule,
    handleDuplicateOverrideRule,
    handleDeleteOverrideRule,
    handleToggleOverrideRule,
    handleOverrideRuleNameChange,
    handleCommitOverrideRuleName,
    handleOverrideMatchTextChange,
    handleOverridePatchTextChange,
    handleOverrideRuleTextChange,
    handleAddHeaderOp,
    handleUpdateHeaderOp,
    handleRemoveHeaderOp,
    getSelectedRuleHeaders,
    handleCopy,
    handleMergeDefaultUsageRules,
    handleAddUsageRule,
    handleSelectUsageRule,
    handleToggleUsageRule,
    handleDuplicateUsageRule,
    handleDeleteUsageRule,
    handleUsageRuleTextChange,
}: OverridesTabProps) {
    return (
        <div className="space-y-10 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
            <div className="mx-auto max-w-7xl space-y-6">
                <div className="inline-flex rounded-md border border-input bg-muted/30 p-1">
                    <button
                        type="button"
                        onClick={() => setActiveRuleTab('request_overrides')}
                        className={cn(
                            "rounded-lg px-4 py-2 text-sm font-semibold transition-colors",
                            activeRuleTab === 'request_overrides'
                                ? "bg-background text-foreground"
                                : "text-muted-foreground hover:text-foreground"
                        )}
                    >
                        {t('settings.rule_tab_request_overrides')}
                    </button>
                    <button
                        type="button"
                        onClick={() => setActiveRuleTab('usage_extraction')}
                        className={cn(
                            "rounded-lg px-4 py-2 text-sm font-semibold transition-colors",
                            activeRuleTab === 'usage_extraction'
                                ? "bg-background text-foreground"
                                : "text-muted-foreground hover:text-foreground"
                        )}
                    >
                        {t('settings.rule_tab_usage_extraction')}
                    </button>
                </div>

                {activeRuleTab === 'request_overrides' && (
                    <>
                <div className="flex items-start gap-3 rounded-md border border-warning/30 bg-warning/10 px-4 py-3 text-sm text-warning">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                    <div className="space-y-1">
                        <p className="font-semibold">{t('settings.request_overrides_warning_title')}</p>
                        <p className="text-xs leading-6 opacity-90">{t('settings.request_overrides_warning')}</p>
                    </div>
                </div>

                <SettingSection title={t('settings.request_overrides_config')}>
                    <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-4">
                        <div className="flex items-center sm:pt-6">
                            <ToggleSetting
                                label={t('settings.request_overrides_enable')}
                                description={t('settings.request_overrides_enable_hint')}
                                checked={requestOverridesEnabled}
                                onCheckedChange={setRequestOverridesEnabled}
                            />
                        </div>

                        <FieldBlock
                            label={t('settings.request_overrides_max_body')}
                            hint={t('settings.request_overrides_max_body_hint')}
                            htmlFor="override-max-body"
                            unit="KB"
                        >
                            <Input
                                id="override-max-body"
                                type="number"
                                min="1"
                                value={overrideMaxBodyKB}
                                onChange={e => setOverrideMaxBodyKB(Number(e.target.value))}
                                className="h-9 w-full rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                    </div>
                </SettingSection>

                <SettingSection
                    title={t('settings.request_overrides_rules')}
                    description={t('settings.request_overrides_rules_hint')}
                >
                    <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-input bg-muted/30 px-3 py-2">
                        <p className="text-xs leading-5 text-muted-foreground">
                            {t('settings.request_overrides_scope_hint')}
                        </p>
                        <div className="flex shrink-0 items-center gap-2">
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                asChild
                                className="h-8 text-xs font-semibold"
                            >
                                <a
                                    href={t('settings.request_overrides_docs_url')}
                                    target="_blank"
                                    rel="noreferrer noopener"
                                >
                                    <ExternalLink className="mr-1.5 h-3.5 w-3.5" />
                                    {t('settings.request_overrides_learn_more')}
                                </a>
                            </Button>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={handleAddOverrideExample}
                                disabled={Boolean(overrideRulesParse.error)}
                                className="h-8 text-xs font-semibold"
                            >
                                <Plus className="mr-1.5 h-3.5 w-3.5" />
                                {t('settings.request_overrides_insert_example')}
                            </Button>
                        </div>
                    </div>
                    <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                        <div className="rounded-md border border-input bg-background">
                            <div className="flex items-center justify-between gap-2 border-b border-input px-3 py-2">
                                <span className="text-xs font-semibold text-muted-foreground">
                                    {t('settings.request_override_rule_list')}
                                </span>
                                <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    onClick={handleAddOverrideRule}
                                    className="h-7 px-2 text-xs"
                                >
                                    <Plus className="mr-1 h-3.5 w-3.5" />
                                    {t('common.add')}
                                </Button>
                            </div>
                            {overrideRulesParse.error ? (
                                <div className="px-3 py-8 text-center text-xs text-danger">
                                    {overrideRulesParse.error}
                                </div>
                            ) : overrideRuleObjects.length === 0 ? (
                                <div className="px-3 py-8 text-center text-xs text-muted-foreground">
                                    {t('settings.request_override_no_rules')}
                                </div>
                            ) : (
                                <div className="max-h-[500px] divide-y divide-border/30 overflow-y-auto">
                                    {overrideRuleObjects.map((rule, index) => {
                                        const ruleName = getOverrideRuleName(rule, `rule-${index + 1}`)
                                        const selected = selectedOverrideRuleIndex === index
                                        const status = getRuleRuntimeStatus(rule, upstreams, overrideBindings, requestOverridesEnabled)
                                        const statusBadgeClass = cn(
                                            "h-5 rounded-md px-2 text-xs font-semibold",
                                            status.kind === 'active' && "border-success/40 bg-success/10 text-success",
                                            status.kind === 'blocked' && "border-warning/40 bg-warning/10 text-warning",
                                            status.kind === 'inactive' && "border-warning/40 bg-warning/10 text-warning",
                                            status.kind === 'unbound' && "border-input bg-muted/50 text-muted-foreground",
                                        )
                                        const statusText =
                                            status.kind === 'active'
                                                ? t('settings.rule_status_active')
                                                : status.kind === 'unbound'
                                                    ? t('settings.rule_status_unbound')
                                                    : status.kind === 'inactive'
                                                        ? t('settings.rule_status_inactive')
                                                    : status.reason === 'global'
                                                        ? t('settings.rule_status_blocked_global')
                                                        : status.reason === 'rule'
                                                            ? t('settings.rule_status_blocked_rule')
                                                            : t('settings.rule_status_blocked_bindings')
                                        const detail =
                                                status.kind === 'active'
                                                ? [
                                                    formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t),
                                                    formatInactiveTargets(status.inactiveTargets, t),
                                                ].filter(Boolean).join('  ·  ')
                                                : status.kind === 'blocked'
                                                    ? [
                                                        formatUpstreamList(status.enabledUpstreams, status.disabledUpstreams, t),
                                                        formatInactiveTargets(status.inactiveTargets, t),
                                                    ].filter(Boolean).join('  ·  ')
                                                    : status.kind === 'inactive'
                                                        ? formatInactiveTargets(status.inactiveTargets, t)
                                                    : ''
                                        return (
                                            <button
                                                key={`${ruleName}-${index}`}
                                                type="button"
                                                onClick={() => handleSelectOverrideRule(index)}
                                                className={cn(
                                                    "flex w-full items-start gap-3 px-3 py-3 text-left transition-colors hover:bg-muted/40",
                                                    selected && "bg-primary/10"
                                                )}
                                            >
                                                <FileCode className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                                                <span className="min-w-0 flex-1 space-y-1">
                                                    <span className="block truncate text-sm font-semibold text-foreground">
                                                        {ruleName}
                                                    </span>
                                                    <span className="flex flex-wrap items-center gap-1.5">
                                                        <Badge variant="outline" className={statusBadgeClass}>
                                                            {statusText}
                                                        </Badge>
                                                        {detail && (
                                                            <span className="min-w-0 truncate text-xs text-muted-foreground">
                                                                {detail}
                                                            </span>
                                                        )}
                                                    </span>
                                                </span>
                                            </button>
                                        )
                                    })}
                                </div>
                            )}
                        </div>

                        <div className="rounded-md border border-input bg-background">
                            <div className="flex flex-wrap items-center justify-between gap-2 border-b border-input px-3 py-2">
                                <div className="min-w-0">
                                    <div className="truncate text-sm font-semibold text-foreground">
                                        {overrideRuleObjects[selectedOverrideRuleIndex]
                                            ? getOverrideRuleName(overrideRuleObjects[selectedOverrideRuleIndex], `rule-${selectedOverrideRuleIndex + 1}`)
                                            : t('settings.request_override_no_rule_selected')}
                                    </div>
                                    <div className="text-xs text-muted-foreground">
                                        {t('settings.request_override_single_rule_hint')}
                                    </div>
                                </div>
                                {overrideRuleObjects[selectedOverrideRuleIndex] && (
                                    <div className="flex shrink-0 items-center gap-1">
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="icon"
                                            onClick={() => handleDuplicateOverrideRule(selectedOverrideRuleIndex)}
                                            className="h-8 w-8"
                                            aria-label={t('settings.rule_duplicate')}
                                        >
                                            <Copy className="h-3.5 w-3.5" />
                                        </Button>
                                        <Button
                                            type="button"
                                            variant="ghost"
                                            size="icon"
                                            onClick={() => handleDeleteOverrideRule(selectedOverrideRuleIndex)}
                                            className="h-8 w-8 text-muted-foreground hover:text-danger"
                                            aria-label={t('common.delete')}
                                        >
                                            <Trash2 className="h-3.5 w-3.5" />
                                        </Button>
                                    </div>
                                )}
                            </div>
                            {overrideRuleObjects[selectedOverrideRuleIndex] ? (
                                <div className="space-y-4 p-3">
                                    <div className="grid gap-4 rounded-md border border-input bg-muted/20 p-4 md:grid-cols-[minmax(0,1fr)_auto] md:items-end">
                                        <FieldBlock label={t('settings.request_override_rule_name')}>
                                            <Input
                                                value={selectedOverrideRuleName}
                                                onChange={event => handleOverrideRuleNameChange(event.target.value)}
                                                onBlur={handleCommitOverrideRuleName}
                                                onKeyDown={event => {
                                                    if (event.key === 'Enter') event.currentTarget.blur()
                                                }}
                                                className="h-9 rounded-md border-input bg-background font-mono text-sm"
                                            />
                                            {selectedOverrideRuleNameError && (
                                                <div className="text-xs text-danger">
                                                    {selectedOverrideRuleNameError}
                                                </div>
                                            )}
                                        </FieldBlock>
                                        <div className="flex h-9 items-center gap-2 rounded-md border border-input bg-background px-3">
                                            <Switch
                                                id="selected-override-rule-enabled"
                                                checked={getOverrideRuleEnabled(overrideRuleObjects[selectedOverrideRuleIndex])}
                                                onCheckedChange={checked => handleToggleOverrideRule(selectedOverrideRuleIndex, checked)}
                                                className="shrink-0 data-[state=unchecked]:bg-border/60"
                                            />
                                            <Label htmlFor="selected-override-rule-enabled" className="cursor-pointer text-sm font-medium">
                                                {t('settings.rule_enabled')}
                                            </Label>
                                        </div>
                                    </div>

                                    <div className="grid gap-4 lg:grid-cols-2">
                                        <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                            <div>
                                                <Label className="text-xs font-semibold text-muted-foreground">
                                                    {t('settings.request_override_match')}
                                                </Label>
                                                <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                    {t('settings.request_override_match_hint')}
                                                </p>
                                            </div>
                                            <Textarea
                                                value={selectedOverrideMatchText}
                                                onChange={event => handleOverrideMatchTextChange(event.target.value)}
                                                rows={9}
                                                spellCheck={false}
                                                className="min-h-[220px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                            />
                                            {selectedOverrideMatchError && (
                                                <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                    {selectedOverrideMatchError}
                                                </div>
                                            )}
                                        </div>

                                        <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                            <div>
                                                <Label className="text-xs font-semibold text-muted-foreground">
                                                    {t('settings.request_override_body_patch')}
                                                </Label>
                                                <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                    {t('settings.request_override_body_patch_hint')}
                                                </p>
                                            </div>
                                            <Textarea
                                                value={selectedOverridePatchText}
                                                onChange={event => handleOverridePatchTextChange(event.target.value)}
                                                rows={9}
                                                spellCheck={false}
                                                className="min-h-[220px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                            />
                                            {selectedOverridePatchError && (
                                                <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                    {selectedOverridePatchError}
                                                </div>
                                            )}
                                        </div>
                                    </div>

                                    <div className="space-y-3 rounded-md border border-input bg-muted/20 p-4">
                                        <div className="flex items-start justify-between gap-3">
                                            <div>
                                                <Label className="text-xs font-semibold text-muted-foreground">
                                                    {t('settings.request_override_headers')}
                                                </Label>
                                                <p className="mt-1 text-xs leading-5 text-muted-foreground">
                                                    {t('settings.request_override_headers_hint')}
                                                </p>
                                            </div>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={handleAddHeaderOp}
                                                className="h-7 shrink-0 px-2 text-xs"
                                            >
                                                <Plus className="mr-1 h-3.5 w-3.5" />
                                                {t('common.add')}
                                            </Button>
                                        </div>
                                        {getSelectedRuleHeaders().length > 0 ? (
                                            <div className="space-y-2">
                                                {getSelectedRuleHeaders().map((header, hIdx) => (
                                                    <div key={hIdx} className="grid grid-cols-1 items-center gap-2 sm:grid-cols-[100px_minmax(140px,1fr)_minmax(180px,2fr)_32px]">
                                                        <Select
                                                            value={header.op}
                                                            onValueChange={value => handleUpdateHeaderOp(hIdx, 'op', value)}
                                                        >
                                                            <SelectTrigger className="h-9 w-full rounded-lg border-input bg-background text-xs">
                                                                <SelectValue />
                                                            </SelectTrigger>
                                                            <SelectContent>
                                                                <SelectItem value="set">{t('settings.header_op_set')}</SelectItem>
                                                                <SelectItem value="remove">{t('settings.header_op_remove')}</SelectItem>
                                                            </SelectContent>
                                                        </Select>
                                                        <Input
                                                            value={header.name}
                                                            onChange={event => handleUpdateHeaderOp(hIdx, 'name', event.target.value)}
                                                            placeholder={t('settings.header_name_placeholder')}
                                                            className="h-9 min-w-0 rounded-lg border-input bg-background text-xs"
                                                        />
                                                        <HeaderValueInput
                                                            value={header.value ?? ''}
                                                            onChange={value => handleUpdateHeaderOp(hIdx, 'value', value)}
                                                            placeholder={header.op === 'remove' ? '—' : t('settings.header_value_placeholder')}
                                                            disabled={header.op === 'remove'}
                                                            sensitive={isSensitiveHeaderName(header.name, sensitiveHeaders)}
                                                            showLabel={t('settings.show_sensitive_value')}
                                                            hideLabel={t('settings.hide_sensitive_value')}
                                                        />
                                                        <Button
                                                            type="button"
                                                            variant="ghost"
                                                            size="icon"
                                                            onClick={() => handleRemoveHeaderOp(hIdx)}
                                                            className="h-8 w-8 shrink-0 text-muted-foreground hover:text-danger"
                                                        >
                                                            <Trash2 className="h-3.5 w-3.5" />
                                                        </Button>
                                                    </div>
                                                ))}
                                            </div>
                                        ) : (
                                            <div className="rounded-lg border border-dashed border-input px-3 py-4 text-center text-xs text-muted-foreground">
                                                {t('settings.request_override_no_headers')}
                                            </div>
                                        )}
                                    </div>

                                    <details className="group rounded-md border border-input bg-muted/20">
                                        <summary className="flex cursor-pointer list-none items-center justify-between gap-3 px-4 py-3 text-xs font-semibold text-muted-foreground marker:content-none hover:text-foreground [&::-webkit-details-marker]:hidden">
                                            <span>{t('settings.request_override_final_preview')}</span>
                                            <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
                                        </summary>
                                        <div className="space-y-2 border-t border-input p-3">
                                            <p className="text-xs leading-5 text-muted-foreground">
                                                {t('settings.request_override_final_preview_hint')}
                                            </p>
                                            <Textarea
                                                value={selectedOverrideRulePreview}
                                                readOnly
                                                rows={12}
                                                spellCheck={false}
                                                className="min-h-[260px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                            />
                                            <div className="flex justify-end">
                                                <Button type="button" variant="outline" size="sm" onClick={() => handleCopy(selectedOverrideRulePreview)} className="h-8 text-xs">
                                                    <Copy className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('common.copy')}
                                                </Button>
                                            </div>
                                        </div>
                                    </details>

                                    <div className="rounded-md border border-input bg-muted/10">
                                        <button
                                            type="button"
                                            onClick={() => setSelectedRuleAdvancedOpen(open => !open)}
                                            className="flex w-full items-center justify-between gap-3 px-4 py-3 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                                        >
                                            <span>{t('settings.request_override_raw_rule')}</span>
                                            <span>{selectedRuleAdvancedOpen ? t('common.hide') : t('common.show')}</span>
                                        </button>
                                        {selectedRuleAdvancedOpen && (
                                            <div className="space-y-2 border-t border-input p-3">
                                                <Textarea
                                                    value={selectedOverrideRuleText}
                                                    onChange={event => handleOverrideRuleTextChange(event.target.value)}
                                                    rows={14}
                                                    spellCheck={false}
                                                    className="min-h-[320px] w-full resize-y rounded-lg border-input bg-background font-mono text-xs leading-relaxed"
                                                />
                                                {selectedOverrideRuleError && (
                                            <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                {selectedOverrideRuleError}
                                            </div>
                                        )}
                                            </div>
                                        )}
                                    </div>
                                </div>
                            ) : (
                                <div className="px-4 py-16 text-center text-xs text-muted-foreground">
                                    {t('settings.request_override_no_rule_selected')}
                                </div>
                            )}
                        </div>
                    </div>

                    <div className="mt-4 rounded-md border border-input bg-muted/20">
                        <button
                            type="button"
                            onClick={() => setAdvancedRulesOpen(open => !open)}
                            className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                        >
                            <span>{t('settings.request_override_advanced_json')}</span>
                            <span>{advancedRulesOpen ? t('common.hide') : t('common.show')}</span>
                        </button>
                        {advancedRulesOpen && (
                            <div className="border-t border-input p-3">
                                <Textarea
                                    value={overrideRulesText}
                                    onChange={e => setOverrideRulesText(e.target.value)}
                                    rows={12}
                                    spellCheck={false}
                                    className="min-h-[260px] w-full rounded-lg border-input bg-background font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background resize-y"
                                    placeholder={requestOverrideExample}
                                />
                            </div>
                        )}
                    </div>
                </SettingSection>

                    </>
                )}

                {activeRuleTab === 'usage_extraction' && (
                    <>
                <SettingSection title={t('settings.usage_extraction_config')}>
                    <ToggleSetting
                        label={t('settings.usage_extraction_enable')}
                        description={t('settings.usage_extraction_enable_hint')}
                        checked={usageExtractionEnabled}
                        onCheckedChange={setUsageExtractionEnabled}
                    />
                </SettingSection>

                <SettingSection
                    title={t('settings.usage_extraction_rules')}
                    description={t('settings.usage_extraction_rules_hint')}
                >
                        <div className="mb-3 flex flex-wrap items-center justify-between gap-3 rounded-md border border-input bg-background px-3 py-2">
                            <p className="text-xs leading-5 text-muted-foreground">
                                {t('settings.usage_extraction_scope_hint')}
                            </p>
                            <Button
                                type="button"
                                variant="outline"
                                size="sm"
                                onClick={handleMergeDefaultUsageRules}
                                disabled={Boolean(usageRulesParse.error)}
                                className="h-8 text-xs font-semibold"
                            >
                                <Plus className="mr-1.5 h-3.5 w-3.5" />
                                {t('settings.usage_extraction_insert_example')}
                            </Button>
                        </div>

                        <div className="grid gap-4 xl:grid-cols-[minmax(280px,0.32fr)_minmax(0,1fr)]">
                            <div className="rounded-md border border-input bg-background">
                                <div className="flex items-center justify-between gap-2 border-b border-input px-3 py-2">
                                    <span className="text-xs font-semibold text-muted-foreground">
                                        {t('settings.usage_extraction_rule_list')}
                                    </span>
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={handleAddUsageRule}
                                        className="h-7 px-2 text-xs"
                                    >
                                        <Plus className="mr-1 h-3.5 w-3.5" />
                                        {t('common.add')}
                                    </Button>
                                </div>
                                {usageRulesParse.error ? (
                                    <div className="px-3 py-8 text-center text-xs text-danger">
                                        {usageRulesParse.error}
                                    </div>
                                ) : usageRuleObjects.length === 0 ? (
                                    <div className="px-3 py-8 text-center text-xs text-muted-foreground">
                                        {t('settings.usage_extraction_no_rules')}
                                    </div>
                                ) : (
                                    <div className="max-h-[420px] divide-y divide-border/30 overflow-y-auto">
                                        {usageRuleObjects.map((rule, index) => {
                                            const ruleName = getOverrideRuleName(rule, `usage-rule-${index + 1}`)
                                            const boundCount = Object.values(usageBindings).filter(binding => getBindingRuleNames(binding).includes(ruleName)).length
                                                + upstreams.reduce((count, upstream) => count + Object.values(upstream.targets || {}).filter(target => (
                                                    target.usage_extraction && getBindingRuleNames(target.usage_extraction).includes(ruleName)
                                                )).length, 0)
                                            const selected = selectedUsageRuleIndex === index
                                            return (
                                                <button
                                                    key={`${ruleName}-${index}`}
                                                    type="button"
                                                    onClick={() => handleSelectUsageRule(index)}
                                                    className={cn(
                                                        "flex w-full items-start gap-3 px-3 py-3 text-left transition-colors hover:bg-muted/40",
                                                        selected && "bg-primary/10"
                                                    )}
                                                >
                                                    <FileCode className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
                                                    <span className="min-w-0 flex-1 space-y-1">
                                                        <span className="block truncate text-sm font-semibold text-foreground">
                                                            {ruleName}
                                                        </span>
                                                        <span className="flex flex-wrap items-center gap-1.5">
                                                            <Badge
                                                                variant="outline"
                                                                className={cn(
                                                                    "h-5 rounded-md px-1.5 text-xs",
                                                                    getOverrideRuleEnabled(rule)
                                                                        ? "border-success/30 bg-success/10 text-success"
                                                                        : "border-input bg-muted/50 text-muted-foreground"
                                                                )}
                                                            >
                                                                {getOverrideRuleEnabled(rule) ? t('settings.rule_enabled') : t('settings.rule_disabled')}
                                                            </Badge>
                                                            {boundCount > 0 && (
                                                                <Badge variant="outline" className="h-5 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
                                                                    {t('settings.rule_bound_count', { count: boundCount })}
                                                                </Badge>
                                                            )}
                                                        </span>
                                                    </span>
                                                </button>
                                            )
                                        })}
                                    </div>
                                )}
                            </div>

                            <div className="rounded-md border border-input bg-background">
                                <div className="flex flex-wrap items-center justify-between gap-2 border-b border-input px-3 py-2">
                                    <div className="min-w-0">
                                        <div className="truncate text-sm font-semibold text-foreground">
                                            {usageRuleObjects[selectedUsageRuleIndex]
                                                ? getOverrideRuleName(usageRuleObjects[selectedUsageRuleIndex], `usage-rule-${selectedUsageRuleIndex + 1}`)
                                                : t('settings.usage_extraction_no_rule_selected')}
                                        </div>
                                        <div className="text-xs text-muted-foreground">
                                            {t('settings.usage_extraction_single_rule_hint')}
                                        </div>
                                    </div>
                                    {usageRuleObjects[selectedUsageRuleIndex] && (
                                        <div className="flex shrink-0 items-center gap-1">
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => handleToggleUsageRule(selectedUsageRuleIndex, !getOverrideRuleEnabled(usageRuleObjects[selectedUsageRuleIndex]))}
                                                className="h-8 px-2 text-xs"
                                            >
                                                {getOverrideRuleEnabled(usageRuleObjects[selectedUsageRuleIndex])
                                                    ? t('settings.rule_disable')
                                                    : t('settings.rule_enable')}
                                            </Button>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="icon"
                                                onClick={() => handleDuplicateUsageRule(selectedUsageRuleIndex)}
                                                className="h-8 w-8"
                                                aria-label={t('settings.rule_duplicate')}
                                            >
                                                <Copy className="h-3.5 w-3.5" />
                                            </Button>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="icon"
                                                onClick={() => handleDeleteUsageRule(selectedUsageRuleIndex)}
                                                className="h-8 w-8 text-muted-foreground hover:text-danger"
                                                aria-label={t('common.delete')}
                                            >
                                                <Trash2 className="h-3.5 w-3.5" />
                                            </Button>
                                        </div>
                                    )}
                                </div>
                                {usageRuleObjects[selectedUsageRuleIndex] ? (
                                    <div className="space-y-2 p-3">
                                        <Textarea
                                            value={selectedUsageRuleText}
                                            onChange={e => handleUsageRuleTextChange(e.target.value)}
                                            rows={14}
                                            spellCheck={false}
                                            className="min-h-[320px] w-full resize-y rounded-lg border-input bg-muted/20 font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background"
                                        />
                                        {selectedUsageRuleError && (
                                            <div className="rounded-lg border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                                                {selectedUsageRuleError}
                                            </div>
                                        )}
                                    </div>
                                ) : (
                                    <div className="px-4 py-16 text-center text-xs text-muted-foreground">
                                        {t('settings.usage_extraction_no_rule_selected')}
                                    </div>
                                )}
                            </div>
                        </div>

                        <div className="mt-4 rounded-md border border-input bg-background">
                            <button
                                type="button"
                                onClick={() => setUsageAdvancedRulesOpen(open => !open)}
                                className="flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs font-semibold text-muted-foreground transition-colors hover:text-foreground"
                            >
                                <span>{t('settings.usage_extraction_advanced_json')}</span>
                                <span>{usageAdvancedRulesOpen ? t('common.hide') : t('common.show')}</span>
                            </button>
                            {usageAdvancedRulesOpen && (
                                <div className="border-t border-input p-3">
                                    <Textarea
                                        value={usageRulesText}
                                        onChange={e => setUsageRulesText(e.target.value)}
                                        rows={12}
                                        spellCheck={false}
                                        className="min-h-[260px] w-full rounded-lg border-input bg-background font-mono text-xs leading-relaxed transition-colors focus-visible:bg-background resize-y"
                                        placeholder={usageExtractionExample}
                                    />
                                </div>
                            )}
                        </div>
                </SettingSection>

                    </>
                )}

            </div>
        </div>
    )
}
