import {
    AlertTriangle,
    Archive,
    Database,
    Download,
    ExternalLink,
    RefreshCw,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { StorageCleanup } from '@/components/StorageCleanup'
import type { AppConfig, StorageUsage, SystemMetrics, UpdateInfo } from '@/lib/api'
import { cn } from '@/lib/utils'
import type { TranslationFn } from './helpers'

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

function MetricCard({
    label,
    value,
    detail,
}: {
    label: string
    value: string
    detail?: string
}) {
    return (
        <div className="rounded-lg border border-border bg-card px-4 py-3">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="mt-1 text-xl tabular-nums text-foreground">{value}</div>
            {detail && (
                <div className="mt-2 text-xs leading-5 text-muted-foreground">
                    {detail}
                </div>
            )}
        </div>
    )
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
    loadMetrics: (showLoading?: boolean) => void
    handleCalculateStorage: () => void
    handleCheckUpdate: () => void
}

export function SystemTab({
    t,
    metrics,
    metricsLoading,
    metricsError,
    memoryUsedPercent,
    metricsUpdatedAt,
    storageUsage,
    storageLoading,
    storageError,
    storageCalculatedAt,
    updateInfo,
    updateLoading,
    updateError,
    config,
    loadMetrics,
    handleCalculateStorage,
    handleCheckUpdate,
}: SystemTabProps) {
    return (
        <div className="mx-auto max-w-7xl space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-300 motion-reduce:animate-none motion-reduce:duration-0">
            {metricsError && (
                <div className="flex items-start gap-3 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
                    <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>{metricsError}</span>
                </div>
            )}

            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
                <MetricCard
                    label={t('settings.system_process_memory')}
                    value={formatBytes(metrics?.process.rss_bytes)}
                    detail={t('settings.system_heap_detail', {
                        alloc: formatBytes(metrics?.process.heap_alloc_bytes),
                        sys: formatBytes(metrics?.process.heap_sys_bytes),
                    })}
                />
                <MetricCard
                    label={t('settings.system_process_cpu')}
                    value={formatPercent(metrics?.process.cpu_percent)}
                    detail={t('settings.system_cpu_detail', {
                        seconds: metrics?.process.cpu_seconds?.toFixed(1) ?? '-',
                    })}
                />
                <MetricCard
                    label={t('settings.system_total_memory')}
                    value={formatPercent(memoryUsedPercent)}
                    detail={t('settings.system_memory_detail', {
                        used: formatBytes(metrics?.memory.used_bytes),
                        total: formatBytes(metrics?.memory.total_bytes),
                        source: metrics?.memory.source || '-',
                    })}
                />
                <MetricCard
                    label={t('settings.system_uptime')}
                    value={formatDuration(metrics?.runtime.uptime_seconds)}
                    detail={t('settings.system_runtime_detail', {
                        goroutines: metrics?.runtime.goroutines ?? '-',
                        cpu: metrics?.runtime.num_cpu ?? '-',
                    })}
                />
            </div>

            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <div className="rounded-md border border-input bg-background px-5 py-4">
                    <div className="mb-3 text-xs font-semibold text-muted-foreground">
                        {t('settings.system_runtime')}
                    </div>
                    <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                        <dt className="text-muted-foreground">{t('settings.system_platform')}</dt>
                        <dd className="font-mono text-foreground">{metrics?.platform || '-'}</dd>
                        <dt className="text-muted-foreground">{t('settings.system_pid')}</dt>
                        <dd className="font-mono text-foreground">{metrics?.process.pid ?? '-'}</dd>
                        <dt className="text-muted-foreground">{t('settings.system_go_version')}</dt>
                        <dd className="break-all font-mono text-foreground">{metrics?.runtime.go_version || '-'}</dd>
                    </dl>
                </div>
                <div className="rounded-md border border-input bg-background px-5 py-4">
                    <div className="mb-3 flex items-center justify-between gap-3">
                        <div className="text-xs font-semibold text-muted-foreground">
                            {t('settings.system_snapshot')}
                        </div>
                        <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={() => loadMetrics(true)}
                            disabled={metricsLoading}
                            className="h-8 rounded-lg px-2.5 text-xs"
                        >
                            <RefreshCw className={cn("mr-1.5 h-3.5 w-3.5", metricsLoading && "animate-spin")} />
                            {t('common.refresh')}
                        </Button>
                    </div>
                    <dl className="grid grid-cols-[120px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
                        <dt className="text-muted-foreground">{t('settings.system_updated_at')}</dt>
                        <dd className="font-mono text-foreground">{metricsUpdatedAt}</dd>
                        <dt className="text-muted-foreground">{t('settings.system_available_memory')}</dt>
                        <dd className="font-mono text-foreground">{formatBytes(metrics?.memory.available_bytes)}</dd>
                        <dt className="text-muted-foreground">{t('settings.system_refresh_interval')}</dt>
                        <dd className="font-mono text-foreground">5s</dd>
                    </dl>
                </div>
            </div>

            <div className="rounded-md border border-input bg-background px-5 py-4">
                <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <div className="text-xs font-semibold text-muted-foreground">
                            {t('settings.storage_usage_title')}
                        </div>
                        <div className="mt-2 text-sm text-muted-foreground">
                            {t('settings.storage_usage_calculated_at', {
                                time: storageCalculatedAt,
                            })}
                        </div>
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        onClick={handleCalculateStorage}
                        disabled={storageLoading}
                        className="h-9 rounded-md"
                    >
                        <RefreshCw className={cn("mr-2 h-4 w-4", storageLoading && "animate-spin")} />
                        {t('settings.storage_usage_calculate')}
                    </Button>
                </div>

                {storageError && (
                    <div className="mb-4 flex items-start gap-3 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                        <span>{storageError}</span>
                    </div>
                )}

                <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
                    <div className="rounded-lg border border-input bg-background px-4 py-3">
                        <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                            <Archive className="h-3.5 w-3.5" />
                            {t('settings.storage_usage_total')}
                        </div>
                        <div className="text-xl font-semibold text-foreground">
                            {formatBytes(storageUsage?.total_bytes)}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                            {t('settings.storage_usage_blob_store', {
                                store: storageUsage?.blob_store || '-',
                            })}
                        </div>
                    </div>
                    <div className="rounded-lg border border-input bg-background px-4 py-3">
                        <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                            <Database className="h-3.5 w-3.5" />
                            {t('settings.storage_usage_database')}
                        </div>
                        <div className="text-xl font-semibold text-foreground">
                            {formatBytes(storageUsage?.database_bytes)}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                            {storageUsage
                                ? t('settings.storage_usage_files', { count: storageUsage.database_files })
                                : '-'}
                        </div>
                    </div>
                    <div className="rounded-lg border border-input bg-background px-4 py-3">
                        <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-muted-foreground">
                            <Archive className="h-3.5 w-3.5" />
                            {t('settings.storage_usage_blobs')}
                        </div>
                        <div className="text-xl font-semibold text-foreground">
                            {formatBytes(storageUsage?.blob_bytes)}
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                            {storageUsage
                                ? t('settings.storage_usage_files', { count: storageUsage.blob_files })
                                : '-'}
                        </div>
                    </div>
                </div>
                <StorageCleanup onCompleted={() => { void handleCalculateStorage() }} />
            </div>

            <div className="rounded-md border border-input bg-background px-5 py-4">
                <div className="mb-5 flex flex-wrap items-center justify-between gap-3">
                    <div>
                        <div className="text-xs font-semibold text-muted-foreground">
                            {t('settings.update_title')}
                        </div>
                        <div className="mt-2 text-sm text-muted-foreground">
                            {t('settings.update_current_version', {
                                version: config?.version ? `v${config.version.replace(/^v/, '')}` : '-',
                            })}
                        </div>
                    </div>
                    <Button
                        type="button"
                        variant="outline"
                        onClick={handleCheckUpdate}
                        disabled={updateLoading}
                        className="h-9 rounded-md"
                    >
                        <RefreshCw className={cn("mr-2 h-4 w-4", updateLoading && "animate-spin")} />
                        {t('settings.update_check')}
                    </Button>
                </div>

                {updateError && (
                    <div className="mb-4 flex items-start gap-3 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
                        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
                        <span>{updateError}</span>
                    </div>
                )}

                {updateInfo && (
                    <div className="space-y-4">
                        <div className="flex flex-wrap items-center gap-3">
                            <Badge
                                variant="outline"
                                className={cn(
                                    "rounded-md px-3 py-1 text-xs font-semibold",
                                    updateInfo.update_available
                                        ? "border-warning/30 bg-warning/10 text-warning"
                                        : "border-success/30 bg-success/10 text-success"
                                )}
                            >
                                {updateInfo.update_available ? t('settings.update_available') : t('settings.update_latest')}
                            </Badge>
                            {updateInfo.update_available && (
                                <span className="text-sm text-muted-foreground">
                                    {t('settings.update_latest_version', {
                                        version: updateInfo.latest_tag || `v${updateInfo.latest_version}`,
                                    })}
                                </span>
                            )}
                        </div>

                        {updateInfo.update_available && (
                            <div className="flex flex-wrap gap-2">
                                {updateInfo.matching_asset && (
                                    <Button asChild className="h-9 rounded-md">
                                        <a href={updateInfo.matching_asset.download_url} target="_blank" rel="noreferrer noopener">
                                            <Download className="mr-2 h-4 w-4" />
                                            {t('settings.update_download_asset', {
                                                size: formatBytes(updateInfo.matching_asset.size),
                                            })}
                                        </a>
                                    </Button>
                                )}
                                <Button asChild variant="outline" className="h-9 rounded-md">
                                    <a href={updateInfo.release_url} target="_blank" rel="noreferrer noopener">
                                        <ExternalLink className="mr-2 h-4 w-4" />
                                        {t('settings.update_open_release')}
                                    </a>
                                </Button>
                            </div>
                        )}
                    </div>
                )}
            </div>
        </div>
    )
}
