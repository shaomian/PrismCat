import type { TraceFilter } from './api'

export const tracePeriods = ['all', 'today', '7d', '30d'] as const
export type TracePeriod = typeof tracePeriods[number]

export interface TraceListFilters {
  query: string
  period: TracePeriod
  upstream: string
  archived: boolean
  hasError: boolean
  pinned: boolean
  label: string
  offset: number
  limit: number
}

export function defaultTraceFilters(query = '', label = ''): TraceListFilters {
  return { query, period: 'all', upstream: '', archived: false, hasError: false, pinned: false, label: label.trim(), offset: 0, limit: 20 }
}

export function countTraceFilters(filters: TraceListFilters) {
  return Number(filters.period !== 'all') + Number(!!filters.upstream) + Number(filters.archived) + Number(filters.hasError) + Number(filters.pinned) + Number(!!filters.label)
}

export function traceListQuery(filters: TraceListFilters, now = new Date()): TraceFilter {
  let start: Date | undefined
  if (filters.period === 'today') {
    start = new Date(now)
    start.setHours(0, 0, 0, 0)
  } else if (filters.period !== 'all') {
    start = new Date(now.getTime() - (filters.period === '7d' ? 7 : 30) * 86400000)
  }
  return {
    q: filters.query.trim() || undefined,
    upstream: filters.upstream || undefined,
    archived: filters.archived,
    has_error: filters.hasError ? true : undefined,
    pinned: filters.pinned ? true : undefined,
    label: filters.label || undefined,
    start_time: start?.toISOString(),
    offset: filters.offset,
    limit: filters.limit,
  }
}
