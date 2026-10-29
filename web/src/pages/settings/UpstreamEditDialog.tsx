import { type Dispatch, type SetStateAction } from 'react'
import {
    Plus,
    Trash2,
    Save,
    AlertTriangle,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog'
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select'
import { UpstreamLoggingPathFilter } from '@/components/UpstreamLoggingPathFilter'
import type { ModelPathTemplate, SSEKeepaliveMode } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
    commitSelectedTarget,
    getOverrideRuleEnabled,
    type EditingUpstream,
    type OverrideRuleObject,
    type TranslationFn,
} from './helpers'
import {
    AdvancedSettings,
    AdvancedSettingsGroup,
    FieldBlock,
    InfoTooltip,
    OutboundProxyControl,
    ToggleSetting,
} from './shared'

type UpstreamEditDialogProps = {
    editingUpstream: EditingUpstream | null
    setEditingUpstream: Dispatch<SetStateAction<EditingUpstream | null>>
    upstreamAdvancedOpen: boolean
    setUpstreamAdvancedOpen: Dispatch<SetStateAction<boolean>>
    newTargetPresetName: string
    setNewTargetPresetName: Dispatch<SetStateAction<string>>
    saving: boolean
    parsedOverrideRules: string[]
    overrideRuleObjects: OverrideRuleObject[]
    parsedUsageRules: string[]
    usageRuleObjects: OverrideRuleObject[]
    modelPathTemplates: ModelPathTemplate[]
    handleSelectTargetPreset: (targetName: string) => void
    handleAddTargetPreset: () => void
    handleEnableTargetPresets: () => void
    handleRemoveTargetPreset: () => void
    handleSaveUpstreamEdit: () => void
    toggleEditingRule: (ruleName: string, checked: boolean) => void
    toggleEditingUsageRule: (ruleName: string, checked: boolean) => void
    t: TranslationFn
}

export function UpstreamEditDialog({
    editingUpstream,
    setEditingUpstream,
    upstreamAdvancedOpen,
    setUpstreamAdvancedOpen,
    newTargetPresetName,
    setNewTargetPresetName,
    saving,
    parsedOverrideRules,
    overrideRuleObjects,
    parsedUsageRules,
    usageRuleObjects,
    modelPathTemplates,
    handleSelectTargetPreset,
    handleAddTargetPreset,
    handleEnableTargetPresets,
    handleRemoveTargetPreset,
    handleSaveUpstreamEdit,
    toggleEditingRule,
    toggleEditingUsageRule,
    t,
}: UpstreamEditDialogProps) {
    return (
        <Dialog open={!!editingUpstream} onOpenChange={(open) => !open && setEditingUpstream(null)}>
            <DialogContent className={cn(
                "max-h-[calc(100vh-2rem)] grid-rows-[auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border border-input bg-card p-0 transition-[max-width] duration-200",
                upstreamAdvancedOpen ? "max-w-2xl lg:max-w-5xl" : "max-w-2xl",
            )}>
                <DialogHeader className="border-b border-input px-6 py-5">
                    <DialogTitle className="text-base font-semibold">
                        {editingUpstream ? t('upstream_manager.edit_title', { name: editingUpstream.name }) : t('common.edit')}
                    </DialogTitle>
                    <DialogDescription className="sr-only">
                        {t('upstream_manager.edit_description')}
                    </DialogDescription>
                </DialogHeader>
                {editingUpstream && (
                    <>
                        <div className="min-h-0 space-y-6 overflow-y-auto px-6 py-5">
                            {/* 上游级字段:这三个不属于任何预设,所以留在预设面板外面。
                                记录日志原来混在右栏的预设级分组里,看着像能按预设分别关,
                                实际 loggingEnabled 不在 editingBufferAsTarget 里 */}
                            <section className="space-y-4">
                                <div className="text-sm font-semibold text-foreground">
                                    {t('upstream_manager.scope_upstream')}
                                </div>
                                <div className="grid grid-cols-1 gap-5 sm:grid-cols-[minmax(0,1fr)_120px]">
                                    <FieldBlock label={t('upstream_manager.name')}>
                                        <Input
                                            value={editingUpstream.name}
                                            readOnly
                                            className="h-9 rounded-md border-input bg-muted/50 font-mono text-sm"
                                        />
                                    </FieldBlock>
                                    <FieldBlock label={t('upstream_manager.order')}>
                                        <Input
                                            type="number"
                                            min={0}
                                            value={editingUpstream.order}
                                            onChange={e => setEditingUpstream(current => current ? { ...current, order: Number(e.target.value) } : current)}
                                            className="h-9 rounded-md border-input bg-background text-sm"
                                        />
                                    </FieldBlock>
                                </div>
                                <ToggleSetting
                                    label={t('upstream_manager.logging_enabled')}
                                    description={t('upstream_manager.logging_enabled_hint')}
                                    checked={editingUpstream.loggingEnabled}
                                    onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, loggingEnabled: checked } : current)}
                                />
                                <div className="space-y-2 border-l-2 border-border pl-4">
                                    <div>
                                        <div className="text-sm font-medium text-foreground">
                                            {t('upstream_manager.logging_path_mode')}
                                        </div>
                                        <p className="mt-1 text-xs leading-relaxed text-muted-foreground">
                                            {t('upstream_manager.logging_path_mode_hint')}
                                        </p>
                                    </div>
                                    <UpstreamLoggingPathFilter
                                        value={editingUpstream.loggingPathFilter}
                                        onChange={loggingPathFilter => setEditingUpstream(current => current ? { ...current, loggingPathFilter } : current)}
                                        templates={modelPathTemplates}
                                        disabled={!editingUpstream.loggingEnabled}
                                    />
                                </div>
                            </section>

                            {/* 预设级字段:切换器在面板上沿,面板边框包住的正是它支配的范围。
                                原来边框只圈住选择器本身,被支配的八组字段全在框外面 */}
                            <section>
                                <div className="mb-3 flex flex-wrap items-center gap-3">
                                    <div className="flex items-center gap-2.5">
                                        <span className="text-sm font-semibold text-foreground">
                                            {editingUpstream.usesTargetPresets
                                                ? t('upstream_manager.target_presets')
                                                : t('upstream_manager.scope_target')}
                                        </span>
                                        <InfoTooltip content={t('upstream_manager.target_presets_hint')} />
                                    </div>
                                    {editingUpstream.usesTargetPresets ? (
                                        <>
                                            {/* 用 aria-pressed 的按钮组而不是伪造 tab 语义:
                                                不必自己实现方向键导航,可达性也不打折 */}
                                            <div
                                                role="group"
                                                aria-label={t('upstream_manager.target_presets')}
                                                className="inline-flex items-center gap-0.5 rounded-lg bg-muted p-[3px]"
                                            >
                                                {Object.keys(editingUpstream.targets).map(name => (
                                                    <button
                                                        key={name}
                                                        type="button"
                                                        aria-pressed={name === editingUpstream.selectedTarget}
                                                        onClick={() => handleSelectTargetPreset(name)}
                                                        className={cn(
                                                            'rounded-md px-2.5 py-1 text-xs transition-colors',
                                                            name === editingUpstream.selectedTarget
                                                                ? 'bg-background text-foreground shadow-sm'
                                                                : 'text-muted-foreground hover:text-foreground',
                                                        )}
                                                    >
                                                        {name}
                                                        {name === editingUpstream.activeTarget && (
                                                            <span className="ml-1.5 text-muted-foreground">
                                                                ·{t('upstream_manager.active_target')}
                                                            </span>
                                                        )}
                                                    </button>
                                                ))}
                                            </div>
                                            {/* 不用 ml-auto:宽屏时它会把这组控件顶到最右边,
                                                和左边的切换器之间留出一大片空,窄屏时又变成
                                                单独一行右对齐的孤儿。紧跟着切换器排就行 */}
                                            <div className="flex items-center gap-1.5">
                                                <Input
                                                    value={newTargetPresetName}
                                                    onChange={event => setNewTargetPresetName(event.target.value)}
                                                    onKeyDown={event => {
                                                        if (event.key === 'Enter') {
                                                            event.preventDefault()
                                                            handleAddTargetPreset()
                                                        }
                                                    }}
                                                    placeholder={t('upstream_manager.new_target_name')}
                                                    className="h-8 w-[200px] rounded-md border-input bg-background text-xs"
                                                />
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    className="h-8"
                                                    onClick={handleAddTargetPreset}
                                                    disabled={!newTargetPresetName.trim()}
                                                >
                                                    <Plus className="mr-1 h-3.5 w-3.5" />
                                                    {t('common.add')}
                                                </Button>
                                            </div>
                                        </>
                                    ) : (
                                        <Button
                                            type="button"
                                            variant="outline"
                                            size="sm"
                                            className="ml-auto h-8"
                                            onClick={handleEnableTargetPresets}
                                        >
                                            <Plus className="mr-1.5 h-3.5 w-3.5" />
                                            {t('upstream_manager.enable_target_presets')}
                                        </Button>
                                    )}
                                </div>

                                <div className={cn(
                                    'space-y-5',
                                    editingUpstream.usesTargetPresets && 'rounded-md border border-input bg-muted/15 p-4',
                                )}>
                                    {/* 基础字段整宽排、高级设置整宽排在下面。原来是左右两栏,
                                        左栏只有三个字段、右栏是一大块面板,右边一展开左下就空一片 */}
                                    <div className="space-y-5">
                                        <div className="space-y-5">
                                            <FieldBlock label={t('upstream_manager.target')}>
                                                <Input
                                                    value={editingUpstream.target}
                                                    onChange={e => setEditingUpstream(current => current ? { ...current, target: e.target.value } : current)}
                                                    className="h-9 max-w-2xl rounded-md border-input bg-background font-mono text-sm"
                                                />
                                            </FieldBlock>
                                            <div className="grid grid-cols-1 gap-5 sm:grid-cols-[140px_minmax(0,1fr)] sm:max-w-2xl">
                                                <FieldBlock label={t('upstream_manager.timeout')}>
                                                    <Input
                                                        type="number"
                                                        min="1"
                                                        value={editingUpstream.timeout}
                                                        onChange={e => setEditingUpstream(current => current ? { ...current, timeout: Number(e.target.value) } : current)}
                                                        className="h-9 rounded-md border-input bg-background text-sm"
                                                    />
                                                </FieldBlock>
                                                <FieldBlock
                                                    label={t('upstream_manager.outbound_proxy')}
                                                    hint={t('upstream_manager.outbound_proxy_hint')}
                                                >
                                                    <OutboundProxyControl
                                                        value={editingUpstream.outboundProxy}
                                                        onChange={value => setEditingUpstream(current => current ? { ...current, outboundProxy: value } : current)}
                                                        t={t}
                                                    />
                                                </FieldBlock>
                                            </div>
                                        </div>

                                        <AdvancedSettings
                                            open={upstreamAdvancedOpen}
                                            onOpenChange={setUpstreamAdvancedOpen}
                                        >
                                            <AdvancedSettingsGroup
                                                title={t('upstream_manager.timeout_strategy')}
                                                description={t('upstream_manager.timeout_strategy_hint')}
                                            >
                                                <div className="grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3">
                                                    <FieldBlock
                                                        label={t('upstream_manager.response_header_timeout')}
                                                        hint={t('upstream_manager.response_header_timeout_hint')}
                                                    >
                                                        <Input
                                                            type="number"
                                                            min="0"
                                                            value={editingUpstream.responseHeaderTimeout}
                                                            onChange={e => setEditingUpstream(current => current ? { ...current, responseHeaderTimeout: Number(e.target.value) } : current)}
                                                            className="h-9 rounded-md border-input bg-background text-sm"
                                                        />
                                                    </FieldBlock>
                                                    <FieldBlock
                                                        label={t('upstream_manager.response_body_first_byte_timeout')}
                                                        hint={t('upstream_manager.response_body_first_byte_timeout_hint')}
                                                    >
                                                        <Input
                                                            type="number"
                                                            min="0"
                                                            value={editingUpstream.responseBodyFirstByteTimeout}
                                                            onChange={e => setEditingUpstream(current => current ? { ...current, responseBodyFirstByteTimeout: Number(e.target.value) } : current)}
                                                            className="h-9 rounded-md border-input bg-background text-sm"
                                                        />
                                                    </FieldBlock>
                                                    <FieldBlock
                                                        label={t('upstream_manager.response_body_idle_timeout')}
                                                        hint={t('upstream_manager.response_body_idle_timeout_hint')}
                                                    >
                                                        <Input
                                                            type="number"
                                                            min="0"
                                                            value={editingUpstream.responseBodyIdleTimeout}
                                                            onChange={e => setEditingUpstream(current => current ? { ...current, responseBodyIdleTimeout: Number(e.target.value) } : current)}
                                                            className="h-9 rounded-md border-input bg-background text-sm"
                                                        />
                                                    </FieldBlock>
                                                </div>
                                            </AdvancedSettingsGroup>

                                            <AdvancedSettingsGroup
                                                title={t('upstream_manager.sse_keepalive')}
                                                description={t('upstream_manager.sse_keepalive_hint')}
                                                divider
                                            >
                                                <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                                                    <FieldBlock label={t('upstream_manager.sse_keepalive_mode')}>
                                                        <Select
                                                            value={editingUpstream.sseKeepaliveMode}
                                                            onValueChange={value => setEditingUpstream(current => current ? { ...current, sseKeepaliveMode: value as SSEKeepaliveMode } : current)}
                                                        >
                                                            <SelectTrigger className="h-9 rounded-md border-input bg-background text-sm">
                                                                <SelectValue />
                                                            </SelectTrigger>
                                                            <SelectContent>
                                                                <SelectItem value="off">{t('upstream_manager.sse_keepalive_mode_off')}</SelectItem>
                                                                <SelectItem value="after_headers">{t('upstream_manager.sse_keepalive_mode_after_headers')}</SelectItem>
                                                                <SelectItem value="full">{t('upstream_manager.sse_keepalive_mode_full')}</SelectItem>
                                                            </SelectContent>
                                                        </Select>
                                                    </FieldBlock>
                                                    {editingUpstream.sseKeepaliveMode !== 'off' && (
                                                        <FieldBlock
                                                            label={t('upstream_manager.sse_keepalive_interval')}
                                                            hint={t('upstream_manager.sse_keepalive_interval_hint')}
                                                        >
                                                            <Input
                                                                type="number"
                                                                min="1"
                                                                value={editingUpstream.sseKeepaliveInterval}
                                                                onChange={e => setEditingUpstream(current => current ? { ...current, sseKeepaliveInterval: Number(e.target.value) } : current)}
                                                                className="h-9 rounded-md border-input bg-background text-sm"
                                                            />
                                                        </FieldBlock>
                                                    )}
                                                </div>
                                                {editingUpstream.sseKeepaliveMode === 'full' && (
                                                    <div className="mt-4 flex gap-2 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
                                                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                                        <span>{t('upstream_manager.sse_keepalive_full_warning')}</span>
                                                    </div>
                                                )}
                                            </AdvancedSettingsGroup>

                                            <AdvancedSettingsGroup title={t('upstream_manager.request_overrides')} divider>
                                                    <ToggleSetting
                                                        label={t('upstream_manager.override_enabled')}
                                                        description={t('upstream_manager.override_enabled_hint')}
                                                        checked={editingUpstream.overrideEnabled}
                                                        onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, overrideEnabled: checked } : current)}
                                                    />
                                                    {editingUpstream.overrideEnabled && (
                                                        <div className="space-y-2">
                                                            <div className="text-xs font-semibold text-muted-foreground">
                                                                {t('upstream_manager.bound_rules')}
                                                            </div>
                                                            {parsedOverrideRules.length === 0 ? (
                                                                <div className="rounded-lg border border-dashed border-input bg-background px-3 py-4 text-xs text-muted-foreground">
                                                                    {t('upstream_manager.no_rules')}
                                                                </div>
                                                            ) : (
                                                                <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                                                                    {parsedOverrideRules.map((ruleName, ruleIndex) => {
                                                                        const rule = overrideRuleObjects[ruleIndex]
                                                                        const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                        const checked = editingUpstream.ruleNames.includes(ruleName)
                                                                        return (
                                                                            <label
                                                                                key={`${ruleName}-${ruleIndex}`}
                                                                                className={cn(
                                                                                    "flex items-center gap-2 rounded-lg border border-input bg-background px-3 py-2 text-sm",
                                                                                    !ruleEnabled && "opacity-70"
                                                                                )}
                                                                            >
                                                                                <input
                                                                                    type="checkbox"
                                                                                    checked={checked}
                                                                                    disabled={!ruleEnabled && !checked}
                                                                                    onChange={e => toggleEditingRule(ruleName, e.target.checked)}
                                                                                />
                                                                                <span className="min-w-0 truncate font-mono text-xs">{ruleName}</span>
                                                                                {!ruleEnabled && (
                                                                                    <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
                                                                                        {t('settings.rule_disabled')}
                                                                                    </Badge>
                                                                                )}
                                                                            </label>
                                                                        )
                                                                    })}
                                                                </div>
                                                            )}
                                                        </div>
                                                    )}
                                            </AdvancedSettingsGroup>

                                            <AdvancedSettingsGroup title={t('upstream_manager.usage_stats')} divider>
                                                    <ToggleSetting
                                                        label={t('upstream_manager.usage_enabled')}
                                                        description={t('upstream_manager.usage_enabled_hint')}
                                                        checked={editingUpstream.usageEnabled}
                                                        onCheckedChange={(checked) => setEditingUpstream(current => current ? { ...current, usageEnabled: checked } : current)}
                                                    />
                                                    {editingUpstream.usageEnabled && (
                                                        <div className="space-y-2">
                                                            <div className="text-xs font-semibold text-muted-foreground">
                                                                {t('upstream_manager.bound_usage_rules')}
                                                            </div>
                                                            {parsedUsageRules.length === 0 ? (
                                                                <div className="rounded-lg border border-dashed border-input bg-background px-3 py-4 text-xs text-muted-foreground">
                                                                    {t('upstream_manager.no_usage_rules')}
                                                                </div>
                                                            ) : (
                                                                <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                                                                    {parsedUsageRules.map((ruleName, ruleIndex) => {
                                                                        const rule = usageRuleObjects[ruleIndex]
                                                                        const ruleEnabled = getOverrideRuleEnabled(rule)
                                                                        const checked = editingUpstream.usageRuleNames.includes(ruleName)
                                                                        return (
                                                                            <label
                                                                                key={`${ruleName}-${ruleIndex}`}
                                                                                className={cn(
                                                                                    "flex items-center gap-2 rounded-lg border border-input bg-background px-3 py-2 text-sm",
                                                                                    !ruleEnabled && "opacity-70"
                                                                                )}
                                                                            >
                                                                                <input
                                                                                    type="checkbox"
                                                                                    checked={checked}
                                                                                    disabled={!ruleEnabled && !checked}
                                                                                    onChange={e => toggleEditingUsageRule(ruleName, e.target.checked)}
                                                                                />
                                                                                <span className="min-w-0 truncate font-mono text-xs">{ruleName}</span>
                                                                                {!ruleEnabled && (
                                                                                    <Badge variant="outline" className="ml-auto h-5 shrink-0 rounded-md border-input bg-muted/50 px-1.5 text-xs text-muted-foreground">
                                                                                        {t('settings.rule_disabled')}
                                                                                    </Badge>
                                                                                )}
                                                                            </label>
                                                                        )
                                                                    })}
                                                                </div>
                                                            )}
                                                        </div>
                                                    )}
                                            </AdvancedSettingsGroup>
                                        </AdvancedSettings>
                                    </div>

                                    {/* 不满足条件就不渲染:原来这两个按钮在只有一个预设时恒为
                                        disabled,是两个永远点不动的控件 */}
                                    {editingUpstream.usesTargetPresets && (
                                        editingUpstream.selectedTarget !== editingUpstream.activeTarget
                                        || Object.keys(editingUpstream.targets).length > 1
                                    ) && (
                                        <div className="flex flex-wrap items-center justify-end gap-2 border-t border-input pt-4">
                                            {editingUpstream.selectedTarget !== editingUpstream.activeTarget && (
                                                <Button
                                                    type="button"
                                                    variant="outline"
                                                    size="sm"
                                                    className="h-8"
                                                    onClick={() => setEditingUpstream(current => current ? { ...commitSelectedTarget(current), activeTarget: current.selectedTarget } : current)}
                                                >
                                                    {t('upstream_manager.set_active_target')}
                                                </Button>
                                            )}
                                            {Object.keys(editingUpstream.targets).length > 1 && (
                                                <Button
                                                    type="button"
                                                    variant="ghost"
                                                    size="sm"
                                                    className="h-8 text-muted-foreground hover:bg-danger/10 hover:text-danger"
                                                    onClick={handleRemoveTargetPreset}
                                                >
                                                    <Trash2 className="mr-1.5 h-3.5 w-3.5" />
                                                    {t('upstream_manager.remove_target')}
                                                </Button>
                                            )}
                                        </div>
                                    )}
                                </div>
                            </section>
                        </div>

                        <div className="flex justify-end gap-2 border-t border-input bg-card px-6 py-4">
                            <Button type="button" variant="ghost" onClick={() => setEditingUpstream(null)}>
                                {t('common.cancel')}
                            </Button>
                            <Button type="button" onClick={handleSaveUpstreamEdit} disabled={saving}>
                                <Save className="mr-2 h-4 w-4" />
                                {t('common.save')}
                            </Button>
                        </div>
                    </>
                )}
            </DialogContent>
        </Dialog>
    )
}
