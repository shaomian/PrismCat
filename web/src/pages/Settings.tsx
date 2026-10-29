import {
    useEffect,
    useState,
    useCallback,
    useMemo,
    useRef,
    type FormEvent,
} from 'react'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { isSettingsTab, type SettingsTab } from '@/lib/routes'
import { Save } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { PageLoading } from '@/components/PageLoading'
import { DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS, DEFAULT_UPSTREAM_TIMEOUT_SECONDS, fetchUpstreams, addUpstream, removeUpstream, activateUpstreamTarget, fetchConfig, updateConfig, fetchSystemMetrics, fetchUpdateInfo, fetchStorageUsage, fetchModelPathTemplates } from '@/lib/api'
import type { Upstream, UpstreamTarget, AppConfig, SystemMetrics, UpdateInfo, StorageUsage, LoggingPathFilter, ModelPathTemplate, SSEKeepaliveMode } from '@/lib/api'
import type { LoggingRulesTab } from '@/pages/LoggingRules'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { copyText } from '@/lib/clipboard'
import {
    canonicalJson,
    commitSelectedTarget,
    customProxyPlaceholder,
    editingBufferAsTarget,
    getBindingRuleNames,
    getErrorMessage,
    getOverrideRuleName,
    hasAdvancedValues,
    loadTargetBuffer,
    normalizedOutboundProxy,
    outboundProxyMode,
    redactSensitiveRuleHeaders,
    requestOverrideExample,
    uniqueRuleName,
    usageExtractionExample,
    type BindingKind,
    type EditingUpstream,
    type HeaderOp,
    type OverrideBinding,
    type OverrideRuleObject,
    type RuleTab,
} from './settings/helpers'
import { UpstreamEditDialog } from './settings/UpstreamEditDialog'
import { RoutingTab } from './settings/RoutingTab'
import { LoggingTab } from './settings/LoggingTab'
import { SystemTab } from './settings/SystemTab'
import { OverridesTab } from './settings/OverridesTab'

export function Settings() {
    const { t, i18n } = useTranslation()
    const navigate = useNavigate()
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [config, setConfig] = useState<AppConfig | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)
    const [showAddForm, setShowAddForm] = useState(false)
    // 鍒嗗尯鐢?URL 鍐冲畾,瀵艰埅鍏ュ彛鍙湁渚ц竟鏍忎竴澶勩€?settings 涓嶅甫鍒嗗尯鏃惰惤鍒?routing,
    // 涓嶅仛閲嶅畾鍚?鍏嶅緱鍦ㄤ繚瀛樻爮鏈夋湭鎻愪氦鏀瑰姩鏃跺洜涓哄湴鍧€璺宠浆涓㈢姸鎬併€?
    const { tab: tabParam, subtab: subtabParam } = useParams()
    const [searchParams] = useSearchParams()
    const deepLinkHandled = useRef(false)
    const activeTab: SettingsTab = isSettingsTab(tabParam) ? tabParam : 'routing'
    const loggingSection: 'general' | LoggingRulesTab = subtabParam === 'models' || subtabParam === 'ignored'
        ? subtabParam
        : 'general'
    const [activeRuleTab, setActiveRuleTab] = useState<RuleTab>('request_overrides')
    const [metrics, setMetrics] = useState<SystemMetrics | null>(null)
    const [metricsLoading, setMetricsLoading] = useState(false)
    const [metricsError, setMetricsError] = useState('')
    const [updateInfo, setUpdateInfo] = useState<UpdateInfo | null>(null)
    const [updateLoading, setUpdateLoading] = useState(false)
    const [updateError, setUpdateError] = useState('')
    const [storageUsage, setStorageUsage] = useState<StorageUsage | null>(null)
    const [storageLoading, setStorageLoading] = useState(false)
    const [storageError, setStorageError] = useState('')

    const [newName, setNewName] = useState('')
    const [newTarget, setNewTarget] = useState('')
    const [newTimeout, setNewTimeout] = useState(DEFAULT_UPSTREAM_TIMEOUT_SECONDS)
    const [newResponseHeaderTimeout, setNewResponseHeaderTimeout] = useState(0)
    const [newResponseBodyFirstByteTimeout, setNewResponseBodyFirstByteTimeout] = useState(0)
    const [newResponseBodyIdleTimeout, setNewResponseBodyIdleTimeout] = useState(0)
    const [newSSEKeepaliveMode, setNewSSEKeepaliveMode] = useState<SSEKeepaliveMode>('off')
    const [newSSEKeepaliveInterval, setNewSSEKeepaliveInterval] = useState(DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS)
    const [newOrder, setNewOrder] = useState(100)
    const [newOutboundProxy, setNewOutboundProxy] = useState('env')
    const [newLoggingEnabled, setNewLoggingEnabled] = useState(true)
    const [newLoggingPathFilter, setNewLoggingPathFilter] = useState<LoggingPathFilter>({ mode: 'all', rules: [] })
    const [modelPathTemplates, setModelPathTemplates] = useState<ModelPathTemplate[]>([])
    const [editingUpstream, setEditingUpstream] = useState<EditingUpstream | null>(null)
    const [upstreamAdvancedOpen, setUpstreamAdvancedOpen] = useState(false)
    const [newTargetPresetName, setNewTargetPresetName] = useState('')
    const [switchingTarget, setSwitchingTarget] = useState('')

    const [enablePathRouting, setEnablePathRouting] = useState(false)
    const [pathRoutingPrefix, setPathRoutingPrefix] = useState('/_proxy')

    const [maxRequestBody, setMaxRequestBody] = useState(5120)
    const [maxResponseBody, setMaxResponseBody] = useState(32768)
    const [sensitiveHeaders, setSensitiveHeaders] = useState('')
    const [detachBodyOver, setDetachBodyOver] = useState(2048)
    const [bodyPreview, setBodyPreview] = useState(512)
    const [storeBase64, setStoreBase64] = useState(true)
    const [earlyRequestBodySnapshot, setEarlyRequestBodySnapshot] = useState(false)

    const [retentionDays, setRetentionDays] = useState(30)
    const [maxStorageGB, setMaxStorageGB] = useState(0)
    const [requestOverridesEnabled, setRequestOverridesEnabled] = useState(false)
    const [overrideMaxBodyKB, setOverrideMaxBodyKB] = useState(1024)
    const [overrideRulesText, setOverrideRulesText] = useState('')
    const [selectedOverrideRuleIndex, setSelectedOverrideRuleIndex] = useState(0)
    const [selectedOverrideRuleText, setSelectedOverrideRuleText] = useState('')
    const [selectedOverrideRuleName, setSelectedOverrideRuleName] = useState('')
    const [selectedOverrideMatchText, setSelectedOverrideMatchText] = useState('')
    const [selectedOverridePatchText, setSelectedOverridePatchText] = useState('')
    const [selectedRuleAdvancedOpen, setSelectedRuleAdvancedOpen] = useState(false)
    const [advancedRulesOpen, setAdvancedRulesOpen] = useState(false)
    const [overrideBindings, setOverrideBindings] = useState<Record<string, OverrideBinding>>({})
    const [usageExtractionEnabled, setUsageExtractionEnabled] = useState(false)
    const [usageRulesText, setUsageRulesText] = useState('')
    const [selectedUsageRuleIndex, setSelectedUsageRuleIndex] = useState(0)
    const [selectedUsageRuleText, setSelectedUsageRuleText] = useState('')
    const [usageAdvancedRulesOpen, setUsageAdvancedRulesOpen] = useState(false)
    const [usageBindings, setUsageBindings] = useState<Record<string, OverrideBinding>>({})
    const [dirtyTargetBindingUpstreams, setDirtyTargetBindingUpstreams] = useState<Set<string>>(new Set())

    const domainSuffix = config?.server?.proxy_domains?.[0] || 'localhost'
    const previewUpstreamName = upstreams[0]?.name || 'openai'
    const sortedUpstreams = useMemo(() => {
        return [...upstreams].sort((a, b) => {
            const orderDiff = (a.order || 0) - (b.order || 0)
            if (orderDiff !== 0) return orderDiff
            return a.name.localeCompare(b.name)
        })
    }, [upstreams])
    const overrideRulesParse = useMemo((): { rules: OverrideRuleObject[]; error: string } => {
        try {
            const parsed = overrideRulesText.trim() ? JSON.parse(overrideRulesText) : []
            if (!Array.isArray(parsed)) {
                return { rules: [], error: t('settings.request_overrides_rules_must_be_array') }
            }
            return {
                rules: parsed.filter((rule): rule is OverrideRuleObject => Boolean(rule) && typeof rule === 'object' && !Array.isArray(rule)),
                error: '',
            }
        } catch {
            return { rules: [], error: t('settings.request_overrides_rules_invalid') }
        }
    }, [overrideRulesText, t])
    const overrideRuleObjects = overrideRulesParse.rules
    const parsedOverrideRules = useMemo(() => {
        return overrideRuleObjects.map((rule, index) => getOverrideRuleName(rule, `rule-${index + 1}`))
    }, [overrideRuleObjects])
    const selectedOverrideRuleError = useMemo(() => {
        if (!selectedOverrideRuleText.trim()) return ''
        try {
            const parsed = JSON.parse(selectedOverrideRuleText)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                return t('settings.request_override_rule_must_be_object')
            }
            const name = typeof parsed.name === 'string' ? parsed.name.trim() : ''
            if (!name) return t('settings.request_override_rule_name_required')
            if (overrideRuleObjects.some((rule, index) => (
                index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
            ))) {
                return t('settings.request_override_rule_name_duplicate')
            }
            return ''
        } catch {
            return t('settings.request_override_rule_invalid')
        }
    }, [overrideRuleObjects, selectedOverrideRuleIndex, selectedOverrideRuleText, t])
    const selectedOverrideRuleNameError = useMemo(() => {
        const name = selectedOverrideRuleName.trim()
        if (!name) return t('settings.request_override_rule_name_required')
        if (overrideRuleObjects.some((rule, index) => (
            index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
        ))) {
            return t('settings.request_override_rule_name_duplicate')
        }
        return ''
    }, [overrideRuleObjects, selectedOverrideRuleIndex, selectedOverrideRuleName, t])
    const selectedOverrideMatchError = useMemo(() => {
        try {
            const parsed = JSON.parse(selectedOverrideMatchText)
            return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
                ? ''
                : t('settings.request_override_match_must_be_object')
        } catch {
            return t('settings.request_override_match_invalid')
        }
    }, [selectedOverrideMatchText, t])
    const selectedOverridePatchError = useMemo(() => {
        try {
            return Array.isArray(JSON.parse(selectedOverridePatchText))
                ? ''
                : t('settings.request_override_patch_must_be_array')
        } catch {
            return t('settings.request_override_patch_invalid')
        }
    }, [selectedOverridePatchText, t])
    const selectedOverrideRulePreview = useMemo(() => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        return rule
            ? JSON.stringify(redactSensitiveRuleHeaders(rule, sensitiveHeaders), null, 2)
            : ''
    }, [overrideRuleObjects, selectedOverrideRuleIndex, sensitiveHeaders])
    const usageRulesParse = useMemo((): { rules: OverrideRuleObject[]; error: string } => {
        try {
            const parsed = usageRulesText.trim() ? JSON.parse(usageRulesText) : []
            if (!Array.isArray(parsed)) {
                return { rules: [], error: t('settings.usage_extraction_rules_must_be_array') }
            }
            return {
                rules: parsed.filter((rule): rule is OverrideRuleObject => Boolean(rule) && typeof rule === 'object' && !Array.isArray(rule)),
                error: '',
            }
        } catch {
            return { rules: [], error: t('settings.usage_extraction_rules_invalid') }
        }
    }, [usageRulesText, t])
    const usageRuleObjects = usageRulesParse.rules
    const parsedUsageRules = useMemo(() => {
        return usageRuleObjects.map((rule, index) => getOverrideRuleName(rule, `usage-rule-${index + 1}`))
    }, [usageRuleObjects])
    const selectedUsageRuleError = useMemo(() => {
        if (!selectedUsageRuleText.trim()) return ''
        try {
            const parsed = JSON.parse(selectedUsageRuleText)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
                return t('settings.usage_extraction_rule_must_be_object')
            }
            return ''
        } catch {
            return t('settings.usage_extraction_rule_invalid')
        }
    }, [selectedUsageRuleText, t])

    const formatOutboundProxy = useCallback((value?: string) => {
        const mode = outboundProxyMode(value)
        if (mode === 'env') return t('upstream_manager.outbound_proxy_env')
        if (mode === 'direct') return t('upstream_manager.outbound_proxy_direct')
        return value || customProxyPlaceholder
    }, [t])

    const proxyBase = useMemo(() => {
        const proto = window.location.protocol
        const hostname = window.location.hostname
        const port = window.location.port
        const portSuffix = port && port !== '80' && port !== '443' ? `:${port}` : ''
        return { proto, hostname, portSuffix }
    }, [])

    // 鍒楄〃閲屽彧鏄剧ず host,涓嶆樉绀?scheme:鍏ュ彛鍦板潃鐨勫崗璁亽绛変簬闈㈡澘鑷韩鐨?origin,
    // 鏄父閲?鍗犲搴﹀嵈涓嶆惡甯︿俊鎭€傚鍒跺嚭鍘荤殑浠嶇劧鏄?getProxyUrl 鐨勫畬鏁?URL銆?
    const getProxyHost = useCallback((name: string) => {
        return `${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const getProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const normalizedPathPrefix = useMemo(() => {
        let prefix = pathRoutingPrefix.trim() || '/_proxy'
        if (!prefix.startsWith('/')) {
            prefix = `/${prefix}`
        }
        return prefix.replace(/\/+$/, '') || '/_proxy'
    }, [pathRoutingPrefix])

    const getPathProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${proxyBase.hostname}${proxyBase.portSuffix}${normalizedPathPrefix}/${name}`
    }, [proxyBase, normalizedPathPrefix])

    const handleCopy = useCallback(async (value: string) => {
        if (await copyText(value)) {
            toast.success(t('log_detail.copy_success'))
        } else {
            toast.error(t('log_detail.copy_failed'))
        }
    }, [t])

    useEffect(() => {
        if (overrideRulesParse.error || overrideRuleObjects.length === 0) {
            setSelectedOverrideRuleIndex(0)
            setSelectedOverrideRuleText('')
            setSelectedOverrideRuleName('')
            setSelectedOverrideMatchText('')
            setSelectedOverridePatchText('')
            return
        }

        const nextIndex = Math.min(selectedOverrideRuleIndex, overrideRuleObjects.length - 1)
        if (nextIndex !== selectedOverrideRuleIndex) {
            setSelectedOverrideRuleIndex(nextIndex)
        }
        const nextRule = overrideRuleObjects[nextIndex]
        setSelectedOverrideRuleName(getOverrideRuleName(nextRule, `rule-${nextIndex + 1}`))

        // 鐢ㄦ埛缂栬緫鍚堟硶 JSON 鏃朵細绔嬪嵆鍥炲啓 overrideRuleObjects锛堟柊鐨勫璞℃爣璇?
        // 浼氳Е鍙戞湰 effect锛夈€傝嫢姝ゆ椂鎶?pretty-print 缁撴灉瑕嗙洊鍥炵紪杈戝櫒锛?
        // 姣忔鍚堟硶鎸夐敭閮戒細閲嶆帓鏁翠釜 textarea 骞舵妸鍏夋爣鐢╁埌鏈熬鈥斺€?
        // 鍥犳缂栬緫鍣ㄥ唴瀹逛笌閫変腑瑙勫垯璇箟涓€鑷存椂璺宠繃鍚屾銆?
        const editorMatchesRule = (() => {
            const raw = selectedOverrideRuleText.trim()
            if (!raw) return false
            try {
                return canonicalJson(JSON.parse(raw)) === canonicalJson(nextRule)
            } catch {
                return false
            }
        })()
        if (!editorMatchesRule) {
            setSelectedOverrideRuleText(JSON.stringify(nextRule, null, 2))
            setSelectedOverrideMatchText(JSON.stringify(nextRule.match ?? {}, null, 2))
            setSelectedOverridePatchText(JSON.stringify(nextRule.patch ?? [], null, 2))
        }
    }, [overrideRulesParse.error, overrideRuleObjects, selectedOverrideRuleIndex, selectedOverrideRuleText])

    useEffect(() => {
        if (usageRulesParse.error || usageRuleObjects.length === 0) {
            setSelectedUsageRuleIndex(0)
            setSelectedUsageRuleText('')
            return
        }

        const nextIndex = Math.min(selectedUsageRuleIndex, usageRuleObjects.length - 1)
        if (nextIndex !== selectedUsageRuleIndex) {
            setSelectedUsageRuleIndex(nextIndex)
        }
        setSelectedUsageRuleText(JSON.stringify(usageRuleObjects[nextIndex], null, 2))
    }, [usageRulesParse.error, usageRuleObjects, selectedUsageRuleIndex])

    const loadData = useCallback(async () => {
        setLoading(true)
        try {
            const [upstreamsData, configData, modelPathData] = await Promise.all([
                fetchUpstreams(),
                fetchConfig(),
                fetchModelPathTemplates(),
            ])
            setUpstreams(upstreamsData || [])
            setDirtyTargetBindingUpstreams(new Set())
            setConfig(configData)
            setModelPathTemplates(modelPathData.templates || [])
            setShowAddForm(prev => prev || !upstreamsData?.length)
            const nextOrder = Math.max(0, ...(upstreamsData || []).map(item => item.order || 0)) + 10
            setNewOrder(nextOrder)

            setMaxRequestBody(Math.round(configData.logging.max_request_body / 1024))
            setMaxResponseBody(Math.round(configData.logging.max_response_body / 1024))
            setSensitiveHeaders(configData.logging.sensitive_headers.join('\n'))
            setDetachBodyOver(Math.round(configData.logging.detach_body_over_bytes / 1024))
            setBodyPreview(Math.round(configData.logging.body_preview_bytes / 1024))
            setStoreBase64(configData.logging.store_base64)
            setEarlyRequestBodySnapshot(configData.logging.early_request_body_snapshot)
            setRetentionDays(configData.storage.retention_days)
            setMaxStorageGB(parseFloat((configData.storage.max_storage_bytes / (1024 * 1024 * 1024)).toFixed(2)))
            setEnablePathRouting(configData.server.enable_path_routing)
            setPathRoutingPrefix(configData.server.path_routing_prefix || '/_proxy')
            setRequestOverridesEnabled(configData.request_overrides?.enabled ?? false)
            setOverrideMaxBodyKB(Math.round((configData.request_overrides?.max_body_bytes ?? 1048576) / 1024))
            setOverrideBindings(configData.request_overrides?.upstreams ?? {})
            const overrideRules = configData.request_overrides?.rules ?? []
            setOverrideRulesText(overrideRules.length ? JSON.stringify(overrideRules, null, 2) : '')
            setUsageExtractionEnabled(configData.usage_extraction?.enabled ?? false)
            setUsageBindings(configData.usage_extraction?.upstreams ?? {})
            const usageRules = configData.usage_extraction?.rules ?? []
            // 绌鸿鍒欐椂淇濇寔绌烘枃鏈紙绀轰緥浠呬綔涓?textarea 鍗犱綅绗﹀睍绀猴級銆?
            // 姝ゅ墠浼氭妸鍐呯疆绀轰緥褰撲綔鐪熷疄瑙勫垯鍥炲～锛岀敤鎴峰彧鎯充繚瀛樻棤鍏抽厤缃椂
            // 绀轰緥瑙勫垯浼氳涓€骞舵寔涔呭寲锛屼笖鍒犱笉骞插噣锛堥噸鏂板姞杞藉張浼氬娲伙級銆?
            setUsageRulesText(usageRules.length ? JSON.stringify(usageRules, null, 2) : '')
        } catch (err) {
            console.error('Failed to load settings:', err)
            toast.error(i18n.t('common.error'))
        } finally {
            setLoading(false)
        }
    }, [i18n])

    useEffect(() => {
        loadData()
    }, [loadData])

    const loadMetrics = useCallback(async (showLoading = false) => {
        if (showLoading) setMetricsLoading(true)
        try {
            const nextMetrics = await fetchSystemMetrics()
            setMetrics(nextMetrics)
            setMetricsError('')
        } catch (err) {
            console.error('Failed to load system metrics:', err)
            setMetricsError(t('settings.system_metrics_failed'))
        } finally {
            if (showLoading) setMetricsLoading(false)
        }
    }, [t])

    useEffect(() => {
        if (activeTab !== 'system') return
        loadMetrics(true)
        const timer = window.setInterval(() => loadMetrics(false), 5000)
        return () => window.clearInterval(timer)
    }, [activeTab, loadMetrics])

    const memoryUsedPercent = useMemo(() => {
        if (!metrics?.memory.total_bytes || metrics.memory.used_bytes === undefined) return null
        return metrics.memory.used_bytes / metrics.memory.total_bytes * 100
    }, [metrics])

    const metricsUpdatedAt = useMemo(() => {
        if (!metrics?.timestamp) return '-'
        return new Date(metrics.timestamp).toLocaleTimeString()
    }, [metrics])

    const storageCalculatedAt = useMemo(() => {
        if (!storageUsage?.calculated_at) return '-'
        return new Date(storageUsage.calculated_at).toLocaleTimeString()
    }, [storageUsage])

    const handleCheckUpdate = async () => {
        setUpdateLoading(true)
        try {
            const info = await fetchUpdateInfo()
            setUpdateInfo(info)
            setUpdateError('')
        } catch (err) {
            setUpdateError(getErrorMessage(err, t('settings.update_failed')))
        } finally {
            setUpdateLoading(false)
        }
    }

    const handleCalculateStorage = async () => {
        setStorageLoading(true)
        try {
            const usage = await fetchStorageUsage()
            setStorageUsage(usage)
            setStorageError('')
        } catch (err) {
            setStorageError(getErrorMessage(err, t('settings.storage_usage_failed')))
        } finally {
            setStorageLoading(false)
        }
    }

    const handleAddUpstream = async (e: FormEvent) => {
        e.preventDefault()
        try {
            await addUpstream(
                newName,
                newTarget.trim(),
                newTimeout,
                newResponseHeaderTimeout,
                newResponseBodyFirstByteTimeout,
                newResponseBodyIdleTimeout,
                newOrder,
                normalizedOutboundProxy(newOutboundProxy),
                newLoggingEnabled,
                '',
                undefined,
                newLoggingPathFilter,
                { mode: newSSEKeepaliveMode, interval: newSSEKeepaliveInterval },
            )
            setNewName('')
            setNewTarget('')
            setNewTimeout(DEFAULT_UPSTREAM_TIMEOUT_SECONDS)
            setNewResponseHeaderTimeout(0)
            setNewResponseBodyFirstByteTimeout(0)
            setNewResponseBodyIdleTimeout(0)
            setNewSSEKeepaliveMode('off')
            setNewSSEKeepaliveInterval(DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS)
            setNewOrder(prev => prev + 10)
            setNewOutboundProxy('env')
            setNewLoggingEnabled(true)
            setNewLoggingPathFilter({ mode: 'all', rules: [] })
            setShowAddForm(false)
            loadData()
            toast.success(t('settings.upstream_added'))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        }
    }

    const handleRemoveUpstream = async (name: string) => {
        if (!confirm(t('upstream_manager.confirm_delete', { name }))) return
        try {
            await removeUpstream(name)
            loadData()
            toast.success(t('settings.upstream_removed'))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        }
    }

    const buildOverridesPayload = (bindings: Record<string, OverrideBinding>, rules: unknown[]) => ({
        enabled: requestOverridesEnabled,
        max_body_bytes: overrideMaxBodyKB * 1024,
        upstreams: bindings,
        rules,
    })

    const buildUsageExtractionPayload = (bindings: Record<string, OverrideBinding>, rules: unknown[]) => ({
        enabled: usageExtractionEnabled,
        upstreams: bindings,
        rules,
    })

    const parseOverrideRules = () => {
        const selectedRuleExists = Boolean(overrideRuleObjects[selectedOverrideRuleIndex])
        const editorError = selectedRuleExists
            ? selectedOverrideRuleNameError || selectedOverrideMatchError || selectedOverridePatchError || selectedOverrideRuleError
            : ''
        if (editorError) {
            throw new Error(editorError)
        }
        const trimmedRules = overrideRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.request_overrides_rules_must_be_array'))
        }
        const names = new Set<string>()
        for (const rule of parsed) {
            const name = getOverrideRuleName(rule, '')
            if (!name) throw new Error(t('settings.request_override_rule_name_required'))
            if (names.has(name)) throw new Error(t('settings.request_override_rule_name_duplicate'))
            names.add(name)
        }
        return parsed
    }

    const parseUsageRules = () => {
        if (selectedUsageRuleError) {
            throw new Error(selectedUsageRuleError)
        }
        const trimmedRules = usageRulesText.trim()
        if (!trimmedRules) return []
        const parsed = JSON.parse(trimmedRules)
        if (!Array.isArray(parsed)) {
            throw new Error(t('settings.usage_extraction_rules_must_be_array'))
        }
        return parsed
    }

    const setOverrideRulesArray = (rules: OverrideRuleObject[], nextIndex = 0) => {
        const safeIndex = Math.max(0, Math.min(nextIndex, Math.max(0, rules.length - 1)))
        const selectedRule = rules[safeIndex]
        setOverrideRulesText(rules.length ? JSON.stringify(rules, null, 2) : '')
        setSelectedOverrideRuleIndex(safeIndex)
        setSelectedOverrideRuleText(selectedRule ? JSON.stringify(selectedRule, null, 2) : '')
        setSelectedOverrideRuleName(selectedRule ? getOverrideRuleName(selectedRule, `rule-${safeIndex + 1}`) : '')
        setSelectedOverrideMatchText(selectedRule ? JSON.stringify(selectedRule.match ?? {}, null, 2) : '')
        setSelectedOverridePatchText(selectedRule ? JSON.stringify(selectedRule.patch ?? [], null, 2) : '')
    }

    const handleActivateTarget = async (upstreamName: string, targetName: string) => {
        setSwitchingTarget(upstreamName)
        try {
            await activateUpstreamTarget(upstreamName, targetName)
            await loadData()
            toast.success(t('upstream_manager.target_switched', { target: targetName }))
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSwitchingTarget('')
        }
    }

    const handleSelectOverrideRule = (index: number) => {
        const rule = overrideRuleObjects[index]
        if (!rule) return
        setSelectedOverrideRuleIndex(index)
        setSelectedOverrideRuleText(JSON.stringify(rule, null, 2))
        setSelectedOverrideRuleName(getOverrideRuleName(rule, `rule-${index + 1}`))
        setSelectedOverrideMatchText(JSON.stringify(rule.match ?? {}, null, 2))
        setSelectedOverridePatchText(JSON.stringify(rule.patch ?? [], null, 2))
        setSelectedRuleAdvancedOpen(false)
    }

    const syncTargetBindingRuleNames = (
        kind: BindingKind,
        transform: (names: string[]) => string[],
    ) => {
        setUpstreams(current => {
            const changed = new Set<string>()
            const next = current.map(upstream => {
                if (!upstream.targets || Object.keys(upstream.targets).length === 0) return upstream
                let upstreamChanged = false
                const targets = Object.fromEntries(Object.entries(upstream.targets).map(([targetName, target]) => {
                    const binding = target[kind]
                    if (!binding) return [targetName, target]
                    const nextNames = transform(getBindingRuleNames(binding))
                    if (nextNames.join('\u0000') === getBindingRuleNames(binding).join('\u0000')) return [targetName, target]
                    upstreamChanged = true
                    return [targetName, {
                        ...target,
                        [kind]: { ...binding, rule_names: nextNames },
                    }]
                })) as Record<string, UpstreamTarget>
                if (!upstreamChanged) return upstream
                changed.add(upstream.name)
                return { ...upstream, targets }
            })
            if (changed.size > 0) {
                setDirtyTargetBindingUpstreams(previous => new Set([...previous, ...changed]))
            }
            return next
        })
    }

    const replaceSelectedOverrideRule = (nextRule: OverrideRuleObject, syncBindingName = false) => {
        const currentRule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!currentRule) return
        const previousName = getOverrideRuleName(currentRule, '')
        const nextName = getOverrideRuleName(nextRule, '')
        const nextRules = [...overrideRuleObjects]
        nextRules[selectedOverrideRuleIndex] = nextRule
        setOverrideRulesText(JSON.stringify(nextRules, null, 2))
        setSelectedOverrideRuleText(JSON.stringify(nextRule, null, 2))

        if (syncBindingName && previousName && nextName && previousName !== nextName) {
            setOverrideBindings(current => Object.fromEntries(
                Object.entries(current).map(([upstream, binding]) => [
                    upstream,
                    {
                        ...binding,
                        rule_names: getBindingRuleNames(binding).map(name => name === previousName ? nextName : name),
                    },
                ]),
            ))
            syncTargetBindingRuleNames('request_overrides', names => names.map(name => name === previousName ? nextName : name))
        }
    }

    const handleOverrideRuleNameChange = (value: string) => {
        setSelectedOverrideRuleName(value)
    }

    const handleCommitOverrideRuleName = () => {
        const name = selectedOverrideRuleName.trim()
        if (!name || overrideRuleObjects.some((rule, index) => (
            index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
        ))) return
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule) return
        setSelectedOverrideRuleName(name)
        replaceSelectedOverrideRule({ ...rule, name }, true)
    }

    const handleOverrideMatchTextChange = (value: string) => {
        setSelectedOverrideMatchText(value)
        try {
            const match = JSON.parse(value)
            if (!match || typeof match !== 'object' || Array.isArray(match)) return
            const rule = overrideRuleObjects[selectedOverrideRuleIndex]
            if (rule) replaceSelectedOverrideRule({ ...rule, match })
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleOverridePatchTextChange = (value: string) => {
        setSelectedOverridePatchText(value)
        try {
            const patch = JSON.parse(value)
            if (!Array.isArray(patch)) return
            const rule = overrideRuleObjects[selectedOverrideRuleIndex]
            if (rule) replaceSelectedOverrideRule({ ...rule, patch })
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleOverrideRuleTextChange = (value: string) => {
        setSelectedOverrideRuleText(value)
        try {
            const parsed = JSON.parse(value)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
            const nextRule = parsed as OverrideRuleObject
            const name = getOverrideRuleName(nextRule, '')
            if (!name || overrideRuleObjects.some((rule, index) => (
                index !== selectedOverrideRuleIndex && getOverrideRuleName(rule, '') === name
            ))) return
            replaceSelectedOverrideRule(nextRule, true)
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleAddOverrideRule = () => {
        const nextRule = {
            name: `New rule ${overrideRuleObjects.length + 1}`,
            enabled: true,
            match: {
                methods: ['POST'],
            },
            patch: [],
        }
        setOverrideRulesArray([...overrideRuleObjects, nextRule], overrideRuleObjects.length)
    }

    const handleAddOverrideExample = () => {
        if (overrideRulesParse.error) return
        const [template] = JSON.parse(requestOverrideExample) as OverrideRuleObject[]
        const nextRule = {
            ...template,
            name: uniqueRuleName(getOverrideRuleName(template, 'Example rule'), overrideRuleObjects),
        }
        setOverrideRulesArray([...overrideRuleObjects, nextRule], overrideRuleObjects.length)
    }

    const handleDuplicateOverrideRule = (index: number) => {
        const source = overrideRuleObjects[index]
        if (!source) return
        const copy = JSON.parse(JSON.stringify(source)) as OverrideRuleObject
        copy.name = `${getOverrideRuleName(copy, `rule-${index + 1}`)} copy`
        const nextRules = [...overrideRuleObjects]
        nextRules.splice(index + 1, 0, copy)
        setOverrideRulesArray(nextRules, index + 1)
    }

    const handleDeleteOverrideRule = (index: number) => {
        const ruleName = parsedOverrideRules[index]
        const nextRules = overrideRuleObjects.filter((_, currentIndex) => currentIndex !== index)
        const nextBindings = Object.fromEntries(
            Object.entries(overrideBindings).map(([upstream, binding]) => [
                upstream,
                {
                    ...binding,
                    rule_names: getBindingRuleNames(binding).filter(name => name !== ruleName),
                },
            ]),
        )
        setOverrideBindings(nextBindings)
        syncTargetBindingRuleNames('request_overrides', names => names.filter(name => name !== ruleName))
        setOverrideRulesArray(nextRules, Math.min(index, nextRules.length - 1))
    }

    const handleToggleOverrideRule = (index: number, enabled: boolean) => {
        const nextRules = [...overrideRuleObjects]
        nextRules[index] = { ...nextRules[index], enabled }
        setOverrideRulesArray(nextRules, index)
    }

    const getSelectedRuleHeaders = (): HeaderOp[] => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule || !Array.isArray(rule.headers)) return []
        return (rule.headers as HeaderOp[]).filter(
            (h): h is HeaderOp => h && typeof h === 'object' && typeof h.op === 'string' && typeof h.name === 'string',
        )
    }

    const updateSelectedRuleHeaders = (headers: HeaderOp[]) => {
        const rule = overrideRuleObjects[selectedOverrideRuleIndex]
        if (!rule) return
        const nextRule = { ...rule, headers: headers.length > 0 ? headers : undefined }
        const nextRules = [...overrideRuleObjects]
        nextRules[selectedOverrideRuleIndex] = nextRule
        setOverrideRulesArray(nextRules, selectedOverrideRuleIndex)
    }

    const handleAddHeaderOp = () => {
        updateSelectedRuleHeaders([...getSelectedRuleHeaders(), { op: 'set', name: '', value: '' }])
    }

    const handleUpdateHeaderOp = (hIndex: number, field: keyof HeaderOp, value: string) => {
        const headers = [...getSelectedRuleHeaders()]
        headers[hIndex] = { ...headers[hIndex], [field]: value }
        updateSelectedRuleHeaders(headers)
    }

    const handleRemoveHeaderOp = (hIndex: number) => {
        updateSelectedRuleHeaders(getSelectedRuleHeaders().filter((_, i) => i !== hIndex))
    }

    const setUsageRulesArray = (rules: OverrideRuleObject[], nextIndex = 0) => {
        setUsageRulesText(rules.length ? JSON.stringify(rules, null, 2) : '')
        setSelectedUsageRuleIndex(Math.max(0, Math.min(nextIndex, Math.max(0, rules.length - 1))))
        setSelectedUsageRuleText(rules[nextIndex] ? JSON.stringify(rules[nextIndex], null, 2) : '')
    }

    const handleSelectUsageRule = (index: number) => {
        if (!usageRuleObjects[index]) return
        setSelectedUsageRuleIndex(index)
        setSelectedUsageRuleText(JSON.stringify(usageRuleObjects[index], null, 2))
    }

    const handleUsageRuleTextChange = (value: string) => {
        setSelectedUsageRuleText(value)
        try {
            const parsed = JSON.parse(value)
            if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return
            const nextRules = [...usageRuleObjects]
            const previousName = getOverrideRuleName(nextRules[selectedUsageRuleIndex], '')
            const nextName = getOverrideRuleName(parsed, '')
            nextRules[selectedUsageRuleIndex] = parsed as OverrideRuleObject
            setUsageRulesText(JSON.stringify(nextRules, null, 2))
            if (previousName && nextName && previousName !== nextName) {
                setUsageBindings(current => Object.fromEntries(
                    Object.entries(current).map(([upstream, binding]) => [
                        upstream,
                        {
                            ...binding,
                            rule_names: getBindingRuleNames(binding).map(name => name === previousName ? nextName : name),
                        },
                    ]),
                ))
                syncTargetBindingRuleNames('usage_extraction', names => names.map(name => name === previousName ? nextName : name))
            }
        } catch {
            // Keep the local editor text so users can finish typing valid JSON.
        }
    }

    const handleAddUsageRule = () => {
        const nextRule = {
            name: `New usage rule ${usageRuleObjects.length + 1}`,
            enabled: true,
            match: {
                content_types: ['application/json', 'text/event-stream'],
            },
            paths: {
                input_tokens: [],
                output_tokens: [],
                total_tokens: [],
                raw_usage: [],
            },
        }
        setUsageRulesArray([...usageRuleObjects, nextRule], usageRuleObjects.length)
    }

    const handleMergeDefaultUsageRules = () => {
        if (usageRulesParse.error) return
        const defaults = JSON.parse(usageExtractionExample) as OverrideRuleObject[]
        const existingNames = new Set(usageRuleObjects.map(rule => getOverrideRuleName(rule, '')))
        const additions = defaults.filter(rule => !existingNames.has(getOverrideRuleName(rule, '')))
        if (additions.length === 0) {
            toast.info(t('settings.usage_extraction_defaults_complete'))
            return
        }
        setUsageRulesArray([...usageRuleObjects, ...additions], usageRuleObjects.length)
    }

    const handleDuplicateUsageRule = (index: number) => {
        const source = usageRuleObjects[index]
        if (!source) return
        const copy = JSON.parse(JSON.stringify(source)) as OverrideRuleObject
        copy.name = `${getOverrideRuleName(copy, `usage-rule-${index + 1}`)} copy`
        const nextRules = [...usageRuleObjects]
        nextRules.splice(index + 1, 0, copy)
        setUsageRulesArray(nextRules, index + 1)
    }

    const handleDeleteUsageRule = (index: number) => {
        const ruleName = parsedUsageRules[index]
        const nextRules = usageRuleObjects.filter((_, currentIndex) => currentIndex !== index)
        const nextBindings = Object.fromEntries(
            Object.entries(usageBindings).map(([upstream, binding]) => [
                upstream,
                {
                    ...binding,
                    rule_names: getBindingRuleNames(binding).filter(name => name !== ruleName),
                },
            ]),
        )
        setUsageBindings(nextBindings)
        syncTargetBindingRuleNames('usage_extraction', names => names.filter(name => name !== ruleName))
        setUsageRulesArray(nextRules, Math.min(index, nextRules.length - 1))
    }

    const handleToggleUsageRule = (index: number, enabled: boolean) => {
        const nextRules = [...usageRuleObjects]
        nextRules[index] = { ...nextRules[index], enabled }
        setUsageRulesArray(nextRules, index)
    }

    const handleEditUpstream = useCallback((upstream: Upstream) => {
        const binding = overrideBindings[upstream.name]
        const usageBinding = usageBindings[upstream.name]
        const targets = upstream.targets || {}
        const usesTargetPresets = Object.keys(targets).length > 0
        const selectedTarget = usesTargetPresets
            ? (upstream.active_target && targets[upstream.active_target] ? upstream.active_target : Object.keys(targets)[0])
            : ''
        // 蹇呴』浠庡疄闄呭瓨鍊肩殑鍦版柟璇?鍚敤浜嗙洰鏍囬璁剧殑涓婃父,杩欎簺瀛楁鍦?targets[鍚嶅瓧] 閲?
        // 涓婃父绾т竴寰嬫槸 0,缁戝畾杩樹細琚?delete 鎺?鈥斺€?鍘熸潵鐨勫垽鏂瀹冧滑姘歌繙涓哄亣
        setUpstreamAdvancedOpen(hasAdvancedValues(
            usesTargetPresets
                ? targets[selectedTarget]
                : {
                    response_header_timeout: upstream.response_header_timeout,
                    response_body_first_byte_timeout: upstream.response_body_first_byte_timeout,
                    response_body_idle_timeout: upstream.response_body_idle_timeout,
                    sse_keepalive: upstream.sse_keepalive,
                    request_overrides: binding,
                    usage_extraction: usageBinding,
                },
        ))
        const editing: EditingUpstream = {
            name: upstream.name,
            target: upstream.target,
            timeout: upstream.timeout,
            responseHeaderTimeout: upstream.response_header_timeout || 0,
            responseBodyFirstByteTimeout: upstream.response_body_first_byte_timeout || 0,
            responseBodyIdleTimeout: upstream.response_body_idle_timeout || 0,
            sseKeepaliveMode: upstream.sse_keepalive?.mode || 'off',
            sseKeepaliveInterval: upstream.sse_keepalive?.interval || DEFAULT_SSE_KEEPALIVE_INTERVAL_SECONDS,
            order: upstream.order || 0,
            outboundProxy: upstream.outbound_proxy || 'env',
            loggingEnabled: upstream.logging_enabled !== false,
            loggingPathFilter: upstream.logging_path_filter || { mode: 'all', rules: [] },
            overrideEnabled: usesTargetPresets ? false : (binding?.enabled ?? false),
            ruleNames: usesTargetPresets ? [] : getBindingRuleNames(binding),
            usageEnabled: usesTargetPresets ? false : (usageBinding?.enabled ?? false),
            usageRuleNames: usesTargetPresets ? [] : getBindingRuleNames(usageBinding),
            usesTargetPresets,
            activeTarget: upstream.active_target || selectedTarget,
            selectedTarget,
            targets,
        }
        setNewTargetPresetName('')
        setEditingUpstream(usesTargetPresets ? loadTargetBuffer(editing, selectedTarget) : editing)
    }, [overrideBindings, usageBindings])

    useEffect(() => {
        if (deepLinkHandled.current || loading || activeTab !== 'routing') return
        const requested = searchParams.get('edit')
        if (!requested) return
        const upstream = upstreams.find(item => item.name === requested)
        if (!upstream) return
        deepLinkHandled.current = true
        handleEditUpstream(upstream)
    }, [activeTab, handleEditUpstream, loading, searchParams, upstreams])

    const handleEnableTargetPresets = () => {
        setEditingUpstream(current => {
            if (!current || current.usesTargetPresets) return current
            const next = {
                ...current,
                usesTargetPresets: true,
                activeTarget: 'default',
                selectedTarget: 'default',
                targets: { default: editingBufferAsTarget(current) },
            }
            return next
        })
    }

    const handleSelectTargetPreset = (targetName: string) => {
        // 鍒囧埌涓€涓湁楂樼骇璁剧疆鐨勯璁炬椂鎶婇潰鏉垮睍寮€,鍚﹀垯閭ｄ簺闈為粯璁ゅ€兼槸鐪嬩笉瑙佺殑銆?
        // 鍙睍寮€涓嶆敹璧?鈥斺€?鑷姩鍏虫帀鐢ㄦ埛鎵嬪姩鎵撳紑鐨勯潰鏉挎洿鐑︿汉銆?
        // commitSelectedTarget 鍙洖鍐欏綋鍓嶉€変腑鐨勯偅涓?鎵€浠ヨ繖閲岃鍒囨崲鍓嶇殑鐘舵€佺瓑浠?
        if (hasAdvancedValues(editingUpstream?.targets[targetName])) {
            setUpstreamAdvancedOpen(true)
        }
        setEditingUpstream(current => {
            if (!current) return current
            return loadTargetBuffer(commitSelectedTarget(current), targetName)
        })
    }

    const handleAddTargetPreset = () => {
        const name = newTargetPresetName.trim().toLowerCase()
        if (!name) return
        setEditingUpstream(current => {
            if (!current) return current
            const committed = commitSelectedTarget(current)
            if (committed.targets[name]) {
                toast.error(t('upstream_manager.target_name_duplicate'))
                return current
            }
            const target: UpstreamTarget = {
                url: '',
                timeout: current.timeout,
                response_header_timeout: current.responseHeaderTimeout,
                response_body_first_byte_timeout: current.responseBodyFirstByteTimeout,
                response_body_idle_timeout: current.responseBodyIdleTimeout,
                sse_keepalive: {
                    mode: current.sseKeepaliveMode,
                    interval: current.sseKeepaliveInterval,
                },
                outbound_proxy: current.outboundProxy,
                request_overrides: { enabled: false, rule_names: [] },
                usage_extraction: {
                    enabled: false,
                    rule_names: [],
                },
            }
            const next = { ...committed, targets: { ...committed.targets, [name]: target } }
            return loadTargetBuffer(next, name)
        })
        setNewTargetPresetName('')
    }

    const handleRemoveTargetPreset = () => {
        setEditingUpstream(current => {
            if (!current || !current.usesTargetPresets || Object.keys(current.targets).length <= 1) return current
            const committed = commitSelectedTarget(current)
            const targets = { ...committed.targets }
            delete targets[committed.selectedTarget]
            const nextName = Object.keys(targets)[0]
            const activeTarget = committed.activeTarget === committed.selectedTarget ? nextName : committed.activeTarget
            return loadTargetBuffer({ ...committed, targets, activeTarget }, nextName)
        })
    }

    const toggleEditingRule = (ruleName: string, checked: boolean) => {
        setEditingUpstream(current => {
            if (!current) return current
            const nextRules = checked
                ? [...current.ruleNames, ruleName].filter((value, index, arr) => arr.indexOf(value) === index)
                : current.ruleNames.filter(name => name !== ruleName)
            return { ...current, ruleNames: nextRules }
        })
    }

    const toggleEditingUsageRule = (ruleName: string, checked: boolean) => {
        setEditingUpstream(current => {
            if (!current) return current
            const nextRules = checked
                ? [...current.usageRuleNames, ruleName].filter((value, index, arr) => arr.indexOf(value) === index)
                : current.usageRuleNames.filter(name => name !== ruleName)
            return { ...current, usageRuleNames: nextRules }
        })
    }

    const handleSaveUpstreamEdit = async () => {
        if (!editingUpstream) return
        setSaving(true)
        try {
            const committed = commitSelectedTarget(editingUpstream)
            const overrideRules = parseOverrideRules()
            const usageRules = parseUsageRules()
            const nextBindings = { ...overrideBindings }
            const nextUsageBindings = { ...usageBindings }
            if (committed.usesTargetPresets) {
                delete nextBindings[committed.name]
                delete nextUsageBindings[committed.name]
                if (Object.values(committed.targets).some(target => !target.url.trim())) {
                    throw new Error(t('upstream_manager.target_url_required'))
                }
            } else {
                nextBindings[committed.name] = {
                    enabled: committed.overrideEnabled,
                    rule_names: committed.ruleNames,
                }
                nextUsageBindings[committed.name] = {
                    enabled: committed.usageEnabled,
                    rule_names: committed.usageRuleNames,
                }
            }
            await addUpstream(
                committed.name,
                committed.usesTargetPresets ? '' : committed.target.trim(),
                committed.usesTargetPresets ? 0 : committed.timeout,
                committed.usesTargetPresets ? 0 : committed.responseHeaderTimeout,
                committed.usesTargetPresets ? 0 : committed.responseBodyFirstByteTimeout,
                committed.usesTargetPresets ? 0 : committed.responseBodyIdleTimeout,
                committed.order,
                committed.usesTargetPresets ? '' : normalizedOutboundProxy(committed.outboundProxy),
                committed.loggingEnabled,
                committed.usesTargetPresets ? committed.activeTarget : '',
                committed.usesTargetPresets ? committed.targets : undefined,
                committed.loggingPathFilter,
                committed.usesTargetPresets ? undefined : {
                    mode: committed.sseKeepaliveMode,
                    interval: committed.sseKeepaliveInterval,
                },
            )
            await updateConfig({
                request_overrides: buildOverridesPayload(nextBindings, overrideRules),
                usage_extraction: buildUsageExtractionPayload(nextUsageBindings, usageRules),
            })
            toast.success(t('settings.config_saved'))
            setEditingUpstream(null)
            loadData()
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSaving(false)
        }
    }

    const saveDirtyTargetBindings = async () => {
        for (const upstream of upstreams) {
            if (!dirtyTargetBindingUpstreams.has(upstream.name) || !upstream.targets || Object.keys(upstream.targets).length === 0) continue
            await addUpstream(
                upstream.name,
                '',
                0,
                0,
                0,
                0,
                upstream.order || 0,
                '',
                upstream.logging_enabled !== false,
                upstream.active_target || '',
                upstream.targets,
            )
        }
    }

    const handleSaveAll = async () => {
        setSaving(true)
        try {
            const overrideRules = parseOverrideRules()
            const usageRules = parseUsageRules()

            await updateConfig({
                server: {
                    enable_path_routing: enablePathRouting,
                    path_routing_prefix: pathRoutingPrefix,
                },
                logging: {
                    max_request_body: maxRequestBody * 1024,
                    max_response_body: maxResponseBody * 1024,
                    sensitive_headers: sensitiveHeaders.split('\n').map(s => s.trim()).filter(Boolean),
                    detach_body_over_bytes: detachBodyOver * 1024,
                    body_preview_bytes: bodyPreview * 1024,
                    store_base64: storeBase64,
                    early_request_body_snapshot: earlyRequestBodySnapshot,
                },
                storage: {
                    retention_days: retentionDays,
                    max_storage_bytes: Math.round(maxStorageGB * 1024 * 1024 * 1024),
                },
                request_overrides: buildOverridesPayload(overrideBindings, overrideRules),
                usage_extraction: buildUsageExtractionPayload(usageBindings, usageRules),
            })
            await saveDirtyTargetBindings()
            toast.success(t('settings.config_saved'))
            loadData()
        } catch (err: unknown) {
            toast.error(getErrorMessage(err, t('common.error')))
        } finally {
            setSaving(false)
        }
    }

    if (loading) {
        return <PageLoading className="h-96 min-h-0" />
    }

    return (
        <div className="w-full">
            <UpstreamEditDialog
                editingUpstream={editingUpstream}
                setEditingUpstream={setEditingUpstream}
                upstreamAdvancedOpen={upstreamAdvancedOpen}
                setUpstreamAdvancedOpen={setUpstreamAdvancedOpen}
                newTargetPresetName={newTargetPresetName}
                setNewTargetPresetName={setNewTargetPresetName}
                saving={saving}
                parsedOverrideRules={parsedOverrideRules}
                overrideRuleObjects={overrideRuleObjects}
                parsedUsageRules={parsedUsageRules}
                usageRuleObjects={usageRuleObjects}
                modelPathTemplates={modelPathTemplates}
                handleSelectTargetPreset={handleSelectTargetPreset}
                handleAddTargetPreset={handleAddTargetPreset}
                handleEnableTargetPresets={handleEnableTargetPresets}
                handleRemoveTargetPreset={handleRemoveTargetPreset}
                handleSaveUpstreamEdit={handleSaveUpstreamEdit}
                toggleEditingRule={toggleEditingRule}
                toggleEditingUsageRule={toggleEditingUsageRule}
                t={t}
            />

            <div className="relative z-10 w-full space-y-6 pb-16 animate-fade-in">

                {/* Content Area */}
                <div>
                    {activeTab === 'routing' && (
                        <RoutingTab
                            t={t}
                            previewUpstreamName={previewUpstreamName}
                            domainSuffix={domainSuffix}
                            pathRoutingPrefix={pathRoutingPrefix}
                            setPathRoutingPrefix={setPathRoutingPrefix}
                            enablePathRouting={enablePathRouting}
                            setEnablePathRouting={setEnablePathRouting}
                            showAddForm={showAddForm}
                            setShowAddForm={setShowAddForm}
                            handleAddUpstream={handleAddUpstream}
                            newName={newName}
                            setNewName={setNewName}
                            newTarget={newTarget}
                            setNewTarget={setNewTarget}
                            newTimeout={newTimeout}
                            setNewTimeout={setNewTimeout}
                            newOrder={newOrder}
                            setNewOrder={setNewOrder}
                            newOutboundProxy={newOutboundProxy}
                            setNewOutboundProxy={setNewOutboundProxy}
                            newResponseHeaderTimeout={newResponseHeaderTimeout}
                            setNewResponseHeaderTimeout={setNewResponseHeaderTimeout}
                            newResponseBodyFirstByteTimeout={newResponseBodyFirstByteTimeout}
                            setNewResponseBodyFirstByteTimeout={setNewResponseBodyFirstByteTimeout}
                            newResponseBodyIdleTimeout={newResponseBodyIdleTimeout}
                            setNewResponseBodyIdleTimeout={setNewResponseBodyIdleTimeout}
                            newSSEKeepaliveMode={newSSEKeepaliveMode}
                            setNewSSEKeepaliveMode={setNewSSEKeepaliveMode}
                            newSSEKeepaliveInterval={newSSEKeepaliveInterval}
                            setNewSSEKeepaliveInterval={setNewSSEKeepaliveInterval}
                            newLoggingEnabled={newLoggingEnabled}
                            setNewLoggingEnabled={setNewLoggingEnabled}
                            newLoggingPathFilter={newLoggingPathFilter}
                            setNewLoggingPathFilter={setNewLoggingPathFilter}
                            modelPathTemplates={modelPathTemplates}
                            upstreams={upstreams}
                            sortedUpstreams={sortedUpstreams}
                            overrideBindings={overrideBindings}
                            switchingTarget={switchingTarget}
                            handleCopy={handleCopy}
                            getProxyUrl={getProxyUrl}
                            getProxyHost={getProxyHost}
                            getPathProxyUrl={getPathProxyUrl}
                            handleActivateTarget={handleActivateTarget}
                            handleEditUpstream={handleEditUpstream}
                            handleRemoveUpstream={handleRemoveUpstream}
                            formatOutboundProxy={formatOutboundProxy}
                        />
                    )}

                    {activeTab === 'logging' && (
                        <LoggingTab
                            t={t}
                            loggingSection={loggingSection}
                            navigate={navigate}
                            maxRequestBody={maxRequestBody}
                            setMaxRequestBody={setMaxRequestBody}
                            maxResponseBody={maxResponseBody}
                            setMaxResponseBody={setMaxResponseBody}
                            detachBodyOver={detachBodyOver}
                            setDetachBodyOver={setDetachBodyOver}
                            bodyPreview={bodyPreview}
                            setBodyPreview={setBodyPreview}
                            retentionDays={retentionDays}
                            setRetentionDays={setRetentionDays}
                            maxStorageGB={maxStorageGB}
                            setMaxStorageGB={setMaxStorageGB}
                            earlyRequestBodySnapshot={earlyRequestBodySnapshot}
                            setEarlyRequestBodySnapshot={setEarlyRequestBodySnapshot}
                            storeBase64={storeBase64}
                            setStoreBase64={setStoreBase64}
                            sensitiveHeaders={sensitiveHeaders}
                            setSensitiveHeaders={setSensitiveHeaders}
                        />
                    )}

                    {activeTab === 'system' && (
                        <SystemTab
                            t={t}
                            metrics={metrics}
                            metricsLoading={metricsLoading}
                            metricsError={metricsError}
                            memoryUsedPercent={memoryUsedPercent}
                            metricsUpdatedAt={metricsUpdatedAt}
                            storageUsage={storageUsage}
                            storageLoading={storageLoading}
                            storageError={storageError}
                            storageCalculatedAt={storageCalculatedAt}
                            updateInfo={updateInfo}
                            updateLoading={updateLoading}
                            updateError={updateError}
                            config={config}
                            loadMetrics={loadMetrics}
                            handleCalculateStorage={handleCalculateStorage}
                            handleCheckUpdate={handleCheckUpdate}
                        />
                    )}

                    {activeTab === 'overrides' && (
                        <OverridesTab
                            t={t}
                            activeRuleTab={activeRuleTab}
                            setActiveRuleTab={setActiveRuleTab}
                            requestOverridesEnabled={requestOverridesEnabled}
                            setRequestOverridesEnabled={setRequestOverridesEnabled}
                            overrideMaxBodyKB={overrideMaxBodyKB}
                            setOverrideMaxBodyKB={setOverrideMaxBodyKB}
                            overrideRulesText={overrideRulesText}
                            setOverrideRulesText={setOverrideRulesText}
                            overrideRulesParse={overrideRulesParse}
                            overrideRuleObjects={overrideRuleObjects}
                            selectedOverrideRuleIndex={selectedOverrideRuleIndex}
                            selectedOverrideRuleName={selectedOverrideRuleName}
                            selectedOverrideRuleError={selectedOverrideRuleError}
                            selectedOverrideRuleNameError={selectedOverrideRuleNameError}
                            selectedOverrideMatchError={selectedOverrideMatchError}
                            selectedOverridePatchError={selectedOverridePatchError}
                            selectedOverrideRulePreview={selectedOverrideRulePreview}
                            selectedOverrideMatchText={selectedOverrideMatchText}
                            selectedOverridePatchText={selectedOverridePatchText}
                            selectedOverrideRuleText={selectedOverrideRuleText}
                            selectedRuleAdvancedOpen={selectedRuleAdvancedOpen}
                            setSelectedRuleAdvancedOpen={setSelectedRuleAdvancedOpen}
                            advancedRulesOpen={advancedRulesOpen}
                            setAdvancedRulesOpen={setAdvancedRulesOpen}
                            overrideBindings={overrideBindings}
                            upstreams={upstreams}
                            sensitiveHeaders={sensitiveHeaders}
                            usageExtractionEnabled={usageExtractionEnabled}
                            setUsageExtractionEnabled={setUsageExtractionEnabled}
                            usageRulesText={usageRulesText}
                            setUsageRulesText={setUsageRulesText}
                            usageRulesParse={usageRulesParse}
                            usageRuleObjects={usageRuleObjects}
                            selectedUsageRuleIndex={selectedUsageRuleIndex}
                            selectedUsageRuleText={selectedUsageRuleText}
                            selectedUsageRuleError={selectedUsageRuleError}
                            usageAdvancedRulesOpen={usageAdvancedRulesOpen}
                            setUsageAdvancedRulesOpen={setUsageAdvancedRulesOpen}
                            usageBindings={usageBindings}
                            handleAddOverrideExample={handleAddOverrideExample}
                            handleAddOverrideRule={handleAddOverrideRule}
                            handleSelectOverrideRule={handleSelectOverrideRule}
                            handleDuplicateOverrideRule={handleDuplicateOverrideRule}
                            handleDeleteOverrideRule={handleDeleteOverrideRule}
                            handleToggleOverrideRule={handleToggleOverrideRule}
                            handleOverrideRuleNameChange={handleOverrideRuleNameChange}
                            handleCommitOverrideRuleName={handleCommitOverrideRuleName}
                            handleOverrideMatchTextChange={handleOverrideMatchTextChange}
                            handleOverridePatchTextChange={handleOverridePatchTextChange}
                            handleOverrideRuleTextChange={handleOverrideRuleTextChange}
                            handleAddHeaderOp={handleAddHeaderOp}
                            handleUpdateHeaderOp={handleUpdateHeaderOp}
                            handleRemoveHeaderOp={handleRemoveHeaderOp}
                            getSelectedRuleHeaders={getSelectedRuleHeaders}
                            handleCopy={handleCopy}
                            handleMergeDefaultUsageRules={handleMergeDefaultUsageRules}
                            handleAddUsageRule={handleAddUsageRule}
                            handleSelectUsageRule={handleSelectUsageRule}
                            handleToggleUsageRule={handleToggleUsageRule}
                            handleDuplicateUsageRule={handleDuplicateUsageRule}
                            handleDeleteUsageRule={handleDeleteUsageRule}
                            handleUsageRuleTextChange={handleUsageRuleTextChange}
                        />
                    )}
                </div>
                {(activeTab === 'routing' || (activeTab === 'logging' && loggingSection === 'general') || activeTab === 'overrides') && (
                    <div className="sticky bottom-0 z-30 -mx-4 border-t border-input bg-background px-4 py-3 sm:-mx-10 sm:px-10">
                        <div className="mx-auto flex max-w-7xl items-center justify-end gap-4">
                            <Button
                                type="button"
                                onClick={handleSaveAll}
                                disabled={saving}
                                variant="default"
                                size="lg"
                                className="h-9 min-w-[160px] rounded-md font-medium transition-all whitespace-nowrap"
                            >
                                <Save className="mr-2 h-4 w-4 shrink-0" />
                                {t('common.save')}
                            </Button>
                        </div>
                    </div>
                )}
            </div>
        </div>
    )
}
