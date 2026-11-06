import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import { Download, ExternalLink, RefreshCw } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { SystemBackups } from '@/components/SystemBackups'
import { StoragePolicySetting } from '@/components/StoragePolicySetting'
import type { AppConfig, StorageUsage, SystemMetrics, UpdateInfo } from '@/lib/api'
import type { StoragePolicy } from '@/lib/storage-policy'
import { useSystemBackups } from '@/lib/use-system-backups'
import { cn } from '@/lib/utils'
import type { TranslationFn } from './helpers'
import { SettingSection } from './shared'

function formatBytes(value?: number | null): string {
    if (value === undefined || value === null) return '-'
    if (value < 1024) return `${value} B`
    const units = ['KB', 'MB', 'GB', 'TB']
    let size = value / 1024
    let unitIndex = 0
    while (size >= 1024 && unitIndex < units.length - 1) {
        size /= 1024
        unitIndex += 1
    }
    return `${size >= 100 ? size.toFixed(0) : size.toFixed(1)} ${units[unitIndex]}`
}

function formatPercent(value?: number | null): string {
    if (value === undefined || value === null) return '-'
    if (value < 0.1) return '0%'
    return `${value >= 10 ? value.toFixed(1) : value.toFixed(2)}%`
}

function formatDuration(seconds?: number | null): string {
    if (seconds === undefined || seconds === null) return '-'
    const total = Math.max(0, Math.floor(seconds))
    const days = Math.floor(total / 86400)
    const hours = Math.floor((total % 86400) / 3600)
    const minutes = Math.floor((total % 3600) / 60)
    if (days > 0) return `${days}d ${hours}h`
    if (hours > 0) return `${hours}h ${minutes}m`
    return `${minutes}m ${total % 60}s`
}

function MetricCard({ label, value, detail }: { label: string; value: string; detail: string }) {
    return <div className="min-w-0 space-y-1 rounded-md bg-muted/30 px-4 py-3">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-xl font-semibold tabular-nums">{value}</div>
        <div className="text-xs leading-5 text-muted-foreground">{detail}</div>
    </div>
}

type SystemTabProps = {
    t: TranslationFn
    metrics: SystemMetrics | null
    metricsLoading: boolean
    metricsError: string
    memoryUsedPercent: number | null
    metricsUpdatedAt: string
    storageUsage: StorageUsage | null
    storageLoading: boolean
    storageError: string
    storageCalculatedAt: string
    updateInfo: UpdateInfo | null
    updateLoading: boolean
    updateError: string
    config: AppConfig | null
    onStoragePolicySaved: (policy: StoragePolicy) => void
    loadMetrics: (showLoading?: boolean) => void
    handleCalculateStorage: () => void
    handleCheckUpdate: () => void
}

export function SystemTab({
    t, metrics, metricsLoading, metricsError, memoryUsedPercent, metricsUpdatedAt,
    storageUsage, storageLoading, storageError, storageCalculatedAt,
    updateInfo, updateLoading, updateError, config, onStoragePolicySaved,
    loadMetrics, handleCalculateStorage, handleCheckUpdate,
}: SystemTabProps) {
    const backups = useSystemBackups(config?.storage.cleanup_paused ?? false)
    const { hash } = useLocation()
    useEffect(() => {
        if (hash !== '#storage-policy') return
        const frame = requestAnimationFrame(() => document.getElementById('storage-policy')?.scrollIntoView({ block: 'start' }))
        return () => cancelAnimationFrame(frame)
    }, [hash])

    return <div className="mx-auto max-w-7xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
        <SettingSection title={t('settings.system_status_title')} description={t('settings.system_status_hint')} action={
            <Button type="button" variant="outline" size="sm" onClick={() => loadMetrics(true)} disabled={metricsLoading}>
                <RefreshCw className={cn('size-4', metricsLoading && 'animate-spin')} />{t('common.refresh')}
            </Button>
        }>
            {metricsError && <p role="alert" className="mb-4 text-sm text-danger">{metricsError}</p>}
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <MetricCard label={t('settings.system_process_memory')} value={formatBytes(metrics?.process.rss_bytes)}
                    detail={t('settings.system_heap_detail', { alloc: formatBytes(metrics?.process.heap_alloc_bytes), sys: formatBytes(metrics?.process.heap_sys_bytes) })} />
                <MetricCard label={t('settings.system_process_cpu')} value={formatPercent(metrics?.process.cpu_percent)}
                    detail={t('settings.system_cpu_detail', { seconds: metrics?.process.cpu_seconds?.toFixed(1) ?? '-' })} />
                <MetricCard label={t('settings.system_total_memory')} value={formatPercent(memoryUsedPercent)}
                    detail={t('settings.system_memory_detail', { used: formatBytes(metrics?.memory.used_bytes), total: formatBytes(metrics?.memory.total_bytes), source: metrics?.memory.source || '-' })} />
                <MetricCard label={t('settings.system_uptime')} value={formatDuration(metrics?.runtime.uptime_seconds)}
                    detail={t('settings.system_runtime_detail', { goroutines: metrics?.runtime.goroutines ?? '-', cpu: metrics?.runtime.num_cpu ?? '-' })} />
            </div>
            <dl className="mt-4 flex flex-wrap gap-x-5 gap-y-2 text-xs">
                <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_platform')}</dt><dd>{metrics?.platform || '-'}</dd></div>
                <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_go_version')}</dt><dd className="break-all">{metrics?.runtime.go_version || '-'}</dd></div>
                <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_updated_at')}</dt><dd>{metricsUpdatedAt}</dd></div>
            </dl>
            <details className="mt-3 text-xs">
                <summary className="w-fit cursor-pointer text-muted-foreground">{t('settings.system_runtime_details')}</summary>
                <dl className="mt-3 flex flex-wrap gap-x-5 gap-y-2 rounded-md bg-muted/30 p-3">
                    <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_pid')}</dt><dd>{metrics?.process.pid ?? '-'}</dd></div>
                    <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_available_memory')}</dt><dd>{formatBytes(metrics?.memory.available_bytes)}</dd></div>
                    <div className="flex gap-2"><dt className="text-muted-foreground">{t('settings.system_refresh_interval')}</dt><dd>5s</dd></div>
                </dl>
            </details>
        </SettingSection>

        <SettingSection title={t('settings.storage_management_title')} description={t('settings.storage_usage_calculated_at', { time: storageCalculatedAt })} action={
            <Button type="button" variant="outline" size="sm" onClick={handleCalculateStorage} disabled={storageLoading}>
                <RefreshCw className={cn('size-4', storageLoading && 'animate-spin')} />{t('settings.storage_usage_calculate')}
            </Button>
        }>
            {storageError && <p role="alert" className="mb-4 text-sm text-danger">{storageError}</p>}
            <div className="grid gap-4 divide-y divide-input sm:grid-cols-3 sm:divide-x sm:divide-y-0">
                <div className="space-y-2">
                    <div className="text-xs text-muted-foreground">{t('settings.storage_usage_total')}</div>
                    <div className="text-xl font-semibold tabular-nums">{formatBytes(storageUsage?.total_bytes)}</div>
                    <div className="text-xs text-muted-foreground">{t('settings.storage_usage_blob_store', { store: storageUsage?.blob_store || '-' })}</div>
                </div>
                <div className="space-y-2 pt-4 sm:pl-4 sm:pt-0">
                    <div className="text-xs text-muted-foreground">{t('settings.storage_usage_database')}</div>
                    <div className="text-xl font-semibold tabular-nums">{formatBytes(storageUsage?.database_bytes)}</div>
                    <div className="text-xs text-muted-foreground">{storageUsage ? t('settings.storage_usage_files', { count: storageUsage.database_files }) : '-'}</div>
                </div>
                <div className="space-y-2 pt-4 sm:pl-4 sm:pt-0">
                    <div className="text-xs text-muted-foreground">{t('settings.storage_usage_blobs')}</div>
                    <div className="text-xl font-semibold tabular-nums">{formatBytes(storageUsage?.blob_bytes)}</div>
                    <div className="text-xs text-muted-foreground">{storageUsage ? t('settings.storage_usage_files', { count: storageUsage.blob_files }) : '-'}</div>
                </div>
            </div>
            {config && <StoragePolicySetting policy={config.storage} onSaved={onStoragePolicySaved}
                cleanupPaused={backups.cleanupPaused} onResumeCleanup={backups.resumeCleanup} onCleanupCompleted={handleCalculateStorage} />}
        </SettingSection>

        <SystemBackups status={backups} />

        <section className="space-y-4 rounded-lg border border-input bg-card px-4 py-4 sm:px-6">
            <div className="flex flex-wrap items-center justify-between gap-3">
                <div className="space-y-1">
                    <h3 className="text-sm font-semibold">{t('settings.update_title')}</h3>
                    <p className="text-xs text-muted-foreground">{t('settings.update_current_version', { version: config?.version ? `v${config.version.replace(/^v/, '')}` : '-' })}</p>
                </div>
                <Button type="button" variant="outline" size="sm" onClick={handleCheckUpdate} disabled={updateLoading}>
                    {t('settings.update_check')}
                </Button>
            </div>
            {updateError && <p role="alert" className="text-sm text-danger">{updateError}</p>}
            {updateInfo && <div className="space-y-3">
                <div className="flex flex-wrap items-center gap-3">
                    <Badge variant={updateInfo.update_available ? 'warning' : 'success'}>{t(updateInfo.update_available ? 'settings.update_available' : 'settings.update_latest')}</Badge>
                    {updateInfo.update_available && <span className="text-xs text-muted-foreground">{t('settings.update_latest_version', { version: updateInfo.latest_tag || `v${updateInfo.latest_version}` })}</span>}
                </div>
                {updateInfo.update_available && <div className="flex flex-wrap gap-2">
                    {updateInfo.matching_asset && <Button asChild size="sm">
                        <a href={updateInfo.matching_asset.download_url} target="_blank" rel="noreferrer noopener">
                            <Download className="size-4" />{t('settings.update_download_asset', { size: formatBytes(updateInfo.matching_asset.size) })}
                        </a>
                    </Button>}
                    <Button asChild variant="outline" size="sm"><a href={updateInfo.release_url} target="_blank" rel="noreferrer noopener"><ExternalLink className="size-4" />{t('settings.update_open_release')}</a></Button>
                </div>}
            </div>}
        </section>
    </div>
}
