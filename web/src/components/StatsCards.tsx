import { cn } from '@/lib/utils'
import type { LogStats } from '@/lib/api'
import { formatLatency } from '@/lib/utils'
import { useTranslation } from 'react-i18next'

interface StatsCardsProps {
    stats: LogStats | null
    loading?: boolean
}

/** 纯 SVG 迷你趋势线:零依赖,只看形状,不追求坐标精度 */
function Sparkline({ values, className }: { values: number[]; className?: string }) {
    if (values.length < 2) return null
    const max = Math.max(...values, 1)
    const points = values
        .map((value, index) => `${(index / (values.length - 1)) * 100},${(1 - value / max) * 20 + 1}`)
        .join(' ')
    return (
        <svg
            viewBox="0 0 100 22"
            preserveAspectRatio="none"
            aria-hidden="true"
            className={cn('h-5 w-16 shrink-0 overflow-visible', className)}
        >
            <polyline
                points={points}
                fill="none"
                stroke="currentColor"
                strokeWidth="1.5"
                strokeLinecap="round"
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
            />
        </svg>
    )
}

export function StatsCards({ stats, loading }: StatsCardsProps) {
    const { t } = useTranslation()

    // 只有错误数带语义色,其余保持中性 —— 一屏一个焦点
    const cards = [
        {
            title: t('stats.total_requests'),
            value: (stats?.total_requests ?? 0).toLocaleString(),
            trend: stats?.trend?.map(point => point.total),
        },
        { title: t('common.success'), value: (stats?.success_count ?? 0).toLocaleString() },
        {
            title: t('common.error'),
            value: (stats?.error_count ?? 0).toLocaleString(),
            tone: 'danger' as const,
            trend: stats?.trend?.map(point => point.errors),
        },
        { title: t('log_detail.streaming'), value: (stats?.streaming_count ?? 0).toLocaleString() },
        { title: t('stats.avg_latency'), value: formatLatency(stats?.avg_latency_ms ?? 0) },
        { title: t('log_table.upstream'), value: Object.keys(stats?.by_upstream ?? {}).length.toLocaleString() },
    ]

    return (
        <div className="grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-border bg-border md:grid-cols-3 lg:grid-cols-6">
            {cards.map((card) => (
                <div key={card.title} className={cn('bg-card px-3 py-2', loading && 'animate-pulse')}>
                    <div className="text-xs text-muted-foreground">{card.title}</div>
                    <div className="mt-0.5 flex items-center justify-between gap-2">
                        <span
                            className={cn(
                                'text-xl tabular-nums',
                                card.tone === 'danger' && (stats?.error_count ?? 0) > 0 ? 'text-danger' : 'text-foreground',
                            )}
                        >
                            {card.value}
                        </span>
                        {card.trend && !loading && (
                            <Sparkline
                                values={card.trend}
                                className={card.tone === 'danger' && (stats?.error_count ?? 0) > 0 ? 'text-danger/70' : 'text-muted-foreground/50'}
                            />
                        )}
                    </div>
                </div>
            ))}
        </div>
    )
}
