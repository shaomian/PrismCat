import { type Dispatch, type FormEvent, type SetStateAction } from 'react'
import {
    Plus,
    Trash2,
    Save,
    Upload,
    Copy,
    AlertTriangle,
    Pencil,
    Route,
} from 'lucide-react'

import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { UpstreamLoggingPathFilter } from '@/components/UpstreamLoggingPathFilter'
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
import {
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
} from '@/components/ui/table'
import type { LoggingPathFilter, ModelPathTemplate, SSEKeepaliveMode, Upstream, UpstreamTarget } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
    getBindingRuleNames,
    outboundProxyMode,
    type OverrideBinding,
    type TranslationFn,
} from './helpers'
import {
    AdvancedSettings,
    AdvancedSettingsGroup,
    FieldBlock,
    OutboundProxyControl,
    SettingSection,
    ToggleSetting,
} from './shared'

function activeTargetConfig(upstream: Upstream): UpstreamTarget | undefined {
    if (!upstream.active_target) return undefined
    return upstream.targets?.[upstream.active_target]
}

type RoutingTabProps = {
    t: TranslationFn
    previewUpstreamName: string
    domainSuffix: string
    pathRoutingPrefix: string
    setPathRoutingPrefix: Dispatch<SetStateAction<string>>
    enablePathRouting: boolean
    setEnablePathRouting: Dispatch<SetStateAction<boolean>>
    showAddForm: boolean
    setShowAddForm: Dispatch<SetStateAction<boolean>>
    handleAddUpstream: (event: FormEvent) => void
    newName: string
    setNewName: Dispatch<SetStateAction<string>>
    newTarget: string
    setNewTarget: Dispatch<SetStateAction<string>>
    newTimeout: number
    setNewTimeout: Dispatch<SetStateAction<number>>
    newOrder: number
    setNewOrder: Dispatch<SetStateAction<number>>
    newOutboundProxy: string
    setNewOutboundProxy: Dispatch<SetStateAction<string>>
    newResponseHeaderTimeout: number
    setNewResponseHeaderTimeout: Dispatch<SetStateAction<number>>
    newResponseBodyFirstByteTimeout: number
    setNewResponseBodyFirstByteTimeout: Dispatch<SetStateAction<number>>
    newResponseBodyIdleTimeout: number
    setNewResponseBodyIdleTimeout: Dispatch<SetStateAction<number>>
    newSSEKeepaliveMode: SSEKeepaliveMode
    setNewSSEKeepaliveMode: Dispatch<SetStateAction<SSEKeepaliveMode>>
    newSSEKeepaliveInterval: number
    setNewSSEKeepaliveInterval: Dispatch<SetStateAction<number>>
    newLoggingEnabled: boolean
    setNewLoggingEnabled: Dispatch<SetStateAction<boolean>>
    newLoggingPathFilter: LoggingPathFilter
    setNewLoggingPathFilter: Dispatch<SetStateAction<LoggingPathFilter>>
    modelPathTemplates: ModelPathTemplate[]
    upstreams: Upstream[]
    sortedUpstreams: Upstream[]
    overrideBindings: Record<string, OverrideBinding>
    switchingTarget: string
    handleCopy: (value: string) => void
    getProxyUrl: (name: string) => string
    getProxyHost: (name: string) => string
    getPathProxyUrl: (name: string) => string
    handleActivateTarget: (upstreamName: string, targetName: string) => void
    handleEditUpstream: (upstream: Upstream) => void
    handleRemoveUpstream: (name: string) => void
    formatOutboundProxy: (value?: string) => string
}

export function RoutingTab({
    t,
    previewUpstreamName,
    domainSuffix,
    pathRoutingPrefix,
    setPathRoutingPrefix,
    enablePathRouting,
    setEnablePathRouting,
    showAddForm,
    setShowAddForm,
    handleAddUpstream,
    newName,
    setNewName,
    newTarget,
    setNewTarget,
    newTimeout,
    setNewTimeout,
    newOrder,
    setNewOrder,
    newOutboundProxy,
    setNewOutboundProxy,
    newResponseHeaderTimeout,
    setNewResponseHeaderTimeout,
    newResponseBodyFirstByteTimeout,
    setNewResponseBodyFirstByteTimeout,
    newResponseBodyIdleTimeout,
    setNewResponseBodyIdleTimeout,
    newSSEKeepaliveMode,
    setNewSSEKeepaliveMode,
    newSSEKeepaliveInterval,
    setNewSSEKeepaliveInterval,
    newLoggingEnabled,
    setNewLoggingEnabled,
    newLoggingPathFilter,
    setNewLoggingPathFilter,
    modelPathTemplates,
    upstreams,
    sortedUpstreams,
    overrideBindings,
    switchingTarget,
    handleCopy,
    getProxyUrl,
    getProxyHost,
    getPathProxyUrl,
    handleActivateTarget,
    handleEditUpstream,
    handleRemoveUpstream,
    formatOutboundProxy,
}: RoutingTabProps) {
    return (
        <div className="animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
            {/* 这个 tab 的主体是一张八列数据表,不是表单,所以吃满外壳的 1600
                而不是收到 max-w-7xl —— 宽度跟着内容类型走 */}
            <div className="w-full space-y-6">
                <SettingSection
                    title={t('settings.access_title')}
                    description={t('settings.access_description')}
                >
                    <div className="grid grid-cols-1 gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
                        <FieldBlock
                            label={t('settings.proxy_domain_suffix')}
                            hint={t('settings.proxy_domain_suffix_hint', {
                                name: previewUpstreamName,
                                suffix: domainSuffix,
                            })}
                        >
                            {/* 容器放宽后给输入框封顶:装 ".localhost" 的框子没必要跟着拉到 500px */}
                            <Input
                                value={`.${domainSuffix}`}
                                readOnly
                                className="h-9 w-full max-w-md rounded-md border-input bg-background text-sm font-medium transition-colors cursor-default"
                            />
                        </FieldBlock>
                        <FieldBlock
                            label={t('settings.path_routing_prefix')}
                            htmlFor="path-routing-prefix"
                            hint={t('settings.path_routing_prefix_hint')}
                        >
                            <Input
                                id="path-routing-prefix"
                                value={pathRoutingPrefix}
                                onChange={e => setPathRoutingPrefix(e.target.value)}
                                placeholder="/_proxy"
                                className="h-9 w-full max-w-md rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                            />
                        </FieldBlock>
                        <div className="flex items-center lg:pt-6">
                            <ToggleSetting
                                label={t('settings.enable_path_routing')}
                                description={t('settings.enable_path_routing_hint')}
                                checked={enablePathRouting}
                                onCheckedChange={setEnablePathRouting}
                            />
                        </div>
                    </div>
                </SettingSection>

                <SettingSection
                    title={t('settings.tabs.upstreams')}
                    description={t('settings.upstreams_description')}
                    // 一屏只留一个实心强调色按钮,这里让位给底部的保存
                    action={
                        <Button
                            type="button"
                            variant="outline"
                            onClick={() => setShowAddForm(prev => !prev)}
                            size="sm"
                            className="h-8 whitespace-nowrap border border-input bg-background font-medium text-foreground hover:bg-accent"
                        >
                            {!showAddForm && <Plus className="mr-1.5 h-4 w-4 shrink-0" />}
                            {showAddForm ? t('common.cancel') : t('upstream_manager.add_new')}
                        </Button>
                    }
                >
                <div className="w-full">
                    {showAddForm && (
                        <div className="mb-8 w-full rounded-lg bg-background p-6 border border-border">
                            <form onSubmit={handleAddUpstream} className="grid grid-cols-1 gap-6 md:grid-cols-12 md:items-end">
                                <div className="md:col-span-3">
                                    <FieldBlock label={t('upstream_manager.name')} htmlFor="name">
                                        <div className="relative">
                                            <Input
                                                id="name"
                                                value={newName}
                                                onChange={e => setNewName(e.target.value)}
                                                placeholder="openai"
                                                className="h-9 rounded-md border-input bg-background pr-20 text-sm transition-colors focus-visible:bg-background"
                                                required
                                            />
                                            <div className="pointer-events-none absolute inset-y-0 right-4 flex items-center text-xs text-muted-foreground">
                                                .{domainSuffix}
                                            </div>
                                        </div>
                                    </FieldBlock>
                                </div>

                                <div className="md:col-span-5">
                                    <FieldBlock label={t('upstream_manager.target')} htmlFor="target">
                                        <Input
                                            id="target"
                                            value={newTarget}
                                            onChange={e => setNewTarget(e.target.value)}
                                            placeholder="https://api.openai.com"
                                            className="h-9 rounded-md border-input bg-background font-mono text-sm transition-colors focus-visible:bg-background"
                                            required
                                        />
                                    </FieldBlock>
                                </div>

                                <div className="md:col-span-2">
                                    <FieldBlock label={t('upstream_manager.timeout')} htmlFor="timeout">
                                        <Input
                                            id="timeout"
                                            type="number"
                                            min="1"
                                            value={newTimeout}
                                            onChange={e => setNewTimeout(Number(e.target.value))}
                                            className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                        />
                                    </FieldBlock>
                                </div>

                                <div className="md:col-span-2">
                                    <FieldBlock label={t('upstream_manager.order')} htmlFor="order">
                                        <Input
                                            id="order"
                                            type="number"
                                            min={0}
                                            value={newOrder}
                                            onChange={e => setNewOrder(Number(e.target.value))}
                                            className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                        />
                                    </FieldBlock>
                                </div>

                                <div className="md:col-span-4">
                                    <FieldBlock
                                        label={t('upstream_manager.outbound_proxy')}
                                        hint={t('upstream_manager.outbound_proxy_hint')}
                                    >
                                        <OutboundProxyControl
                                            value={newOutboundProxy}
                                            onChange={setNewOutboundProxy}
                                            t={t}
                                            className="h-9 bg-background"
                                        />
                                    </FieldBlock>
                                </div>

                                <div className="md:col-span-12">
                                    <AdvancedSettings>
                                        <AdvancedSettingsGroup
                                            title={t('upstream_manager.timeout_strategy')}
                                            description={t('upstream_manager.timeout_strategy_hint')}
                                        >
                                            <div className="grid grid-cols-1 gap-5 sm:grid-cols-2">
                                                <FieldBlock
                                                    label={t('upstream_manager.response_header_timeout')}
                                                    htmlFor="response-header-timeout"
                                                    hint={t('upstream_manager.response_header_timeout_hint')}
                                                >
                                                    <Input
                                                        id="response-header-timeout"
                                                        type="number"
                                                        min="0"
                                                        value={newResponseHeaderTimeout}
                                                        onChange={e => setNewResponseHeaderTimeout(Number(e.target.value))}
                                                        className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                    />
                                                </FieldBlock>

                                                <FieldBlock
                                                    label={t('upstream_manager.response_body_first_byte_timeout')}
                                                    htmlFor="response-body-first-byte-timeout"
                                                    hint={t('upstream_manager.response_body_first_byte_timeout_hint')}
                                                >
                                                    <Input
                                                        id="response-body-first-byte-timeout"
                                                        type="number"
                                                        min="0"
                                                        value={newResponseBodyFirstByteTimeout}
                                                        onChange={e => setNewResponseBodyFirstByteTimeout(Number(e.target.value))}
                                                        className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
                                                    />
                                                </FieldBlock>

                                                <FieldBlock
                                                    label={t('upstream_manager.response_body_idle_timeout')}
                                                    htmlFor="response-body-idle-timeout"
                                                    hint={t('upstream_manager.response_body_idle_timeout_hint')}
                                                >
                                                    <Input
                                                        id="response-body-idle-timeout"
                                                        type="number"
                                                        min="0"
                                                        value={newResponseBodyIdleTimeout}
                                                        onChange={e => setNewResponseBodyIdleTimeout(Number(e.target.value))}
                                                        className="h-9 rounded-md border-input bg-background text-sm transition-colors focus-visible:bg-background"
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
                                                    <Select value={newSSEKeepaliveMode} onValueChange={value => setNewSSEKeepaliveMode(value as SSEKeepaliveMode)}>
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
                                                {newSSEKeepaliveMode !== 'off' && (
                                                    <FieldBlock
                                                        label={t('upstream_manager.sse_keepalive_interval')}
                                                        hint={t('upstream_manager.sse_keepalive_interval_hint')}
                                                    >
                                                        <Input
                                                            type="number"
                                                            min="1"
                                                            value={newSSEKeepaliveInterval}
                                                            onChange={e => setNewSSEKeepaliveInterval(Number(e.target.value))}
                                                            className="h-9 rounded-md border-input bg-background text-sm"
                                                        />
                                                    </FieldBlock>
                                                )}
                                            </div>
                                            {newSSEKeepaliveMode === 'full' && (
                                                <div className="mt-4 flex gap-2 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-xs leading-relaxed text-muted-foreground">
                                                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-warning" />
                                                    <span>{t('upstream_manager.sse_keepalive_full_warning')}</span>
                                                </div>
                                            )}
                                        </AdvancedSettingsGroup>

                                        <AdvancedSettingsGroup title={t('upstream_manager.request_logging')} divider>
                                            <ToggleSetting
                                                label={t('upstream_manager.logging_enabled')}
                                                description={t('upstream_manager.logging_enabled_hint')}
                                                checked={newLoggingEnabled}
                                                onCheckedChange={setNewLoggingEnabled}
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
                                                    value={newLoggingPathFilter}
                                                    onChange={setNewLoggingPathFilter}
                                                    templates={modelPathTemplates}
                                                    disabled={!newLoggingEnabled}
                                                />
                                            </div>
                                        </AdvancedSettingsGroup>
                                    </AdvancedSettings>
                                </div>

                                <div className="flex justify-end md:col-span-12">
                                    <Button type="submit" variant="default" size="lg" className="h-9 rounded-md min-w-[120px] font-medium whitespace-nowrap shrink-0">
                                        <Save className="mr-1.5 h-4 w-4 shrink-0" />
                                        {t('common.save')}
                                    </Button>
                                </div>
                            </form>
                        </div>
                    )}

                    {upstreams.length === 0 ? (
                        <div className="rounded-lg border border-dashed border-input bg-muted/10 px-6 py-20 text-center">
                            <Upload className="mx-auto mb-4 h-9 w-10 text-muted-foreground/30" />
                            <p className="text-sm text-foreground/75">
                                {t('upstream_manager.no_upstreams')}
                            </p>
                        </div>
                    ) : (
                        <Table className="table-fixed">
                            <TableHeader className="bg-muted">
                                <TableRow>
                                    {/* 列宽按最长的真实值定,留给英文界面余量:英文标签普遍比中文长
                                        一倍,标记列 250px 才装得下 "Request Override ×1 · Pass-through" */}
                                    <TableHead className="w-[120px]">{t('upstream_manager.name')}</TableHead>
                                    <TableHead className="w-[280px]">{t('upstream_manager.list_entry')}</TableHead>
                                    <TableHead className="w-[130px]">{t('upstream_manager.target_presets')}</TableHead>
                                    <TableHead>{t('upstream_manager.target')}</TableHead>
                                    <TableHead className="w-[200px]">{t('upstream_manager.outbound_proxy')}</TableHead>
                                    <TableHead className="w-[80px]">{t('upstream_manager.list_timeout')}</TableHead>
                                    <TableHead className="w-[250px]">{t('upstream_manager.list_flags')}</TableHead>
                                    <TableHead className="w-[76px]">{t('upstream_manager.actions')}</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {sortedUpstreams.map(upstream => {
                                    // 有目标预设的上游以激活目标的配置为准（显式 false 不再回退到
                                    // 旧版绑定，否则已禁用的目标会因残留绑定而显示错误的徽标）
                                    const targetConfig = activeTargetConfig(upstream)
                                    const overrideConfig = targetConfig?.request_overrides || (!targetConfig ? overrideBindings[upstream.name] : undefined)
                                    const overrideEnabled = targetConfig
                                        ? targetConfig.request_overrides?.enabled ?? false
                                        : overrideBindings[upstream.name]?.enabled ?? false
                                    const overrideRuleCount = getBindingRuleNames(overrideConfig).length
                                    const targetNames = upstream.targets ? Object.keys(upstream.targets) : []
                                    const activeTarget = upstream.active_target || targetNames[0]
                                    const proxyIsCustom = outboundProxyMode(upstream.outbound_proxy) === 'custom'
                                    const flagLabels: string[] = []
                                    if (overrideEnabled) {
                                        flagLabels.push(t('log_detail.request_override') + (overrideRuleCount ? ' ×' + overrideRuleCount : ''))
                                    }
                                    if (upstream.sse_keepalive?.mode && upstream.sse_keepalive.mode !== 'off') {
                                        flagLabels.push(t('upstream_manager.sse_keepalive_badge', { mode: t(`upstream_manager.sse_keepalive_mode_${upstream.sse_keepalive.mode}`) }))
                                    }
                                    if (upstream.logging_enabled === false) {
                                        flagLabels.push(t('upstream_manager.logging_disabled_badge'))
                                    } else if (upstream.logging_path_filter && upstream.logging_path_filter.mode !== 'all') {
                                        flagLabels.push(t(`upstream_manager.logging_mode_${upstream.logging_path_filter.mode}`) + ` ×${upstream.logging_path_filter.rules?.length || 0}`)
                                    }

                                    return (
                                        <TableRow key={upstream.name}>
                                            <TableCell className="font-medium text-foreground">
                                                <div className="truncate" title={upstream.name}>{upstream.name}</div>
                                            </TableCell>

                                            {/* 路径路由前缀是全局设置,每行都是同一个值;右对齐让它自成一列结构,
                                                而不是跟着入口地址的长度左右漂 */}
                                            <TableCell>
                                                <div className="flex items-center gap-2">
                                                    <button
                                                        type="button"
                                                        onClick={() => handleCopy(getProxyUrl(upstream.name))}
                                                        className="flex min-w-0 flex-1 items-center gap-1.5 text-left text-xs text-muted-foreground transition-colors hover:text-foreground"
                                                        title={getProxyUrl(upstream.name)}
                                                    >
                                                        <Copy className="h-3.5 w-3.5 shrink-0 opacity-40" />
                                                        {/* 只显示 host:协议恒等于面板自身的 origin,是常量;复制出去仍是完整 URL */}
                                                        <span className="min-w-0 truncate font-mono">{getProxyHost(upstream.name)}</span>
                                                    </button>
                                                    {enablePathRouting && (
                                                        <Tooltip>
                                                            <TooltipTrigger asChild>
                                                                <button
                                                                    type="button"
                                                                    onClick={() => handleCopy(getPathProxyUrl(upstream.name))}
                                                                    className="shrink-0 rounded-md p-1 text-muted-foreground/70 transition-colors hover:bg-accent hover:text-foreground"
                                                                    aria-label={t('settings.copy_path_proxy_url')}
                                                                >
                                                                    <Route className="h-3.5 w-3.5" />
                                                                </button>
                                                            </TooltipTrigger>
                                                            {/* tooltip 同时说明了「这是什么」和「点了会怎样」:
                                                                图标只对动作成立,状态标签必须留文字 */}
                                                            <TooltipContent>{t('settings.copy_path_proxy_url')}</TooltipContent>
                                                        </Tooltip>
                                                    )}
                                                </div>
                                            </TableCell>

                                            {/* 静止时是一段标签,hover 才浮出边框告诉你可以点 ——
                                                表格主体全是只读文本,一个实心表单控件会独自抢走视线 */}
                                            <TableCell>
                                                {targetNames.length > 1 && activeTarget ? (
                                                    <Select
                                                        value={activeTarget}
                                                        onValueChange={value => handleActivateTarget(upstream.name, value)}
                                                        disabled={switchingTarget === upstream.name}
                                                    >
                                                        <SelectTrigger className="h-6 w-full gap-1 border-transparent bg-transparent px-1.5 text-xs text-muted-foreground shadow-none hover:border-input hover:bg-background hover:text-foreground data-[state=open]:border-input data-[state=open]:bg-background dark:bg-transparent dark:hover:bg-background">
                                                            <SelectValue />
                                                        </SelectTrigger>
                                                        <SelectContent>
                                                            {targetNames.map(name => (
                                                                <SelectItem key={name} value={name}>{name}</SelectItem>
                                                            ))}
                                                        </SelectContent>
                                                    </Select>
                                                ) : (
                                                    <div className="truncate px-1.5 text-xs text-muted-foreground">
                                                        {activeTarget || <span className="opacity-50">—</span>}
                                                    </div>
                                                )}
                                            </TableCell>

                                            <TableCell>
                                                <button
                                                    type="button"
                                                    onClick={() => handleCopy(upstream.target)}
                                                    className="flex w-full min-w-0 items-center gap-1.5 text-left text-xs text-foreground transition-colors hover:text-primary"
                                                    title={upstream.target}
                                                >
                                                    <Copy className="h-3.5 w-3.5 shrink-0 opacity-40" />
                                                    {/* 目标地址保留 scheme:那是填进去的、会变的(有的上游是 http) */}
                                                    <span className="min-w-0 truncate font-mono">{upstream.target}</span>
                                                </button>
                                            </TableCell>

                                            <TableCell className="text-xs text-muted-foreground">
                                                <div
                                                    className={cn('truncate', proxyIsCustom && 'font-mono')}
                                                    title={proxyIsCustom ? upstream.outbound_proxy : undefined}
                                                >
                                                    {formatOutboundProxy(upstream.outbound_proxy)}
                                                </div>
                                            </TableCell>

                                            <TableCell className="font-mono text-xs tabular-nums text-muted-foreground">
                                                {upstream.timeout}s
                                            </TableCell>

                                            {/* badge 数量是会变的,给它专属一列 —— 挂在名称后面会把后面每一列
                                                的起点推到每行不同的位置,那正是原来看着乱的原因。
                                                不用药丸:列边界已经把它框住了,再加边框和底色是重复的 */}
                                            <TableCell className="text-xs text-muted-foreground">
                                                <div className="truncate" title={flagLabels.join(' · ') || undefined}>
                                                    {flagLabels.length ? flagLabels.join(' · ') : <span className="opacity-50">—</span>}
                                                </div>
                                            </TableCell>

                                            {/* 常驻而不是 hover 才出现:右边缘每行都有内容,这一列才立得住 */}
                                            <TableCell>
                                                <div className="flex items-center gap-0.5">
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <Button
                                                                type="button"
                                                                variant="ghost"
                                                                size="icon"
                                                                onClick={() => handleEditUpstream(upstream)}
                                                                className="h-7 w-7 text-muted-foreground/70 hover:bg-accent hover:text-foreground"
                                                                aria-label={t('common.edit')}
                                                            >
                                                                <Pencil className="h-3.5 w-3.5" />
                                                            </Button>
                                                        </TooltipTrigger>
                                                        <TooltipContent>{t('common.edit')}</TooltipContent>
                                                    </Tooltip>
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <Button
                                                                type="button"
                                                                variant="ghost"
                                                                size="icon"
                                                                onClick={() => handleRemoveUpstream(upstream.name)}
                                                                className="h-7 w-7 text-muted-foreground/70 hover:bg-danger/10 hover:text-danger"
                                                                aria-label={t('common.delete')}
                                                            >
                                                                <Trash2 className="h-3.5 w-3.5" />
                                                            </Button>
                                                        </TooltipTrigger>
                                                        <TooltipContent>{t('common.delete')}</TooltipContent>
                                                    </Tooltip>
                                                </div>
                                            </TableCell>
                                        </TableRow>
                                    )
                                })}
                            </TableBody>
                        </Table>
                    )}
                </div>
                </SettingSection>
            </div>
        </div>
    )
}
