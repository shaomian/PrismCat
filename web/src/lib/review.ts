import type { TFunction } from 'i18next'
import { apiRequest, jsonInit } from './api'
import type { RunMetrics } from './experiments'

export interface ReviewPoint { id: string; name: string; instructions: string; metric?: string }
export interface ReviewConfig { template_id: string; template_name: string; points: ReviewPoint[]; selected: string[]; instructions: string }
export interface ReviewSummary { template_id?: string; template_name: string; point_count: number; status: string }
export interface ReviewTemplate { id: string; name: string; description: string; config: ReviewConfig; revision: number; archived: boolean; created_at?: string; updated_at?: string }
export interface TemplateSummary { id: string; name: string; description: string; point_count: number; revision: number }
export interface ReviewTask { run_id: string; name: string; agent: string; model: string; revision: number; prompt?: string; notes?: string; warnings?: string[]; metrics: RunMetrics }
export interface ReviewFinding { run_id: string; value: string; explanation: string; source: 'statistics' | 'logs' | 'inference' | 'unknown'; evidence: string[] }
export interface ReviewObservation { point_id: string; comparison: string; findings: ReviewFinding[] }
export interface ReviewOutput { summary: string; observations: ReviewObservation[]; recommendations: string[] }
export interface ReviewReport { background?: string; id: string; experiment_id: string; parent_id: string; question: string; status: string; config: ReviewConfig; tasks: ReviewTask[]; output?: ReviewOutput; error: string; model: string; usage?: Record<string, unknown>; created_at: string; finished_at?: string }
export interface ReportSummary { id: string; parent_id: string; question: string; status: string; model: string; created_at: string }
export const reviewBase = (id: string) => `/api/experiments/${encodeURIComponent(id)}/review`
async function request<T>(url: string, init: RequestInit = {}): Promise<T> {
  const response = await apiRequest(url, init, 'review.failed')
  return response.json()
}
export const fetchTemplates = (offset = 0, archived = false) => request<{ templates: TemplateSummary[]; total: number }>(`/api/review-templates?${new URLSearchParams({ offset: String(offset), limit: '20', archived: String(archived) })}`)
export const fetchTemplate = (id: string) => request<ReviewTemplate>(`/api/review-templates/${encodeURIComponent(id)}`)
export const saveTemplate = (value: ReviewTemplate) => {
  const payload = { id: value.id, name: value.name, description: value.description, config: value.config, revision: value.revision, archived: value.archived }
  return request<ReviewTemplate>(`/api/review-templates${value.id ? '/' + encodeURIComponent(value.id) : ''}`, jsonInit(value.id ? 'PUT' : 'POST', payload))
}
export const fetchReports = (id: string, offset = 0) => request<{ reports: ReportSummary[]; total: number }>(`${reviewBase(id)}/reports?offset=${offset}&limit=10`)
export function reviewHistoryOffset(offset: number, total: number) {
  return Math.min(Math.max(0, offset), Math.floor(Math.max(0, total - 1) / 10) * 10)
}
export const fetchReport = (id: string, report: string) => request<ReviewReport>(`${reviewBase(id)}/reports/${encodeURIComponent(report)}`)
export const deleteReport = (id: string, report: string) => request<{ status: string }>(`${reviewBase(id)}/reports/${encodeURIComponent(report)}`, { method: 'DELETE' })
export const fetchReviewLog = (id: string, report: string, log: string) => request<import('./api').RequestLog>(`${reviewBase(id)}/reports/${encodeURIComponent(report)}/logs/${encodeURIComponent(log)}`)
export const suggestPoints = (id: string, revision: number, runIds: string[], config: ReviewConfig, language: string, signal: AbortSignal) => request<{ points: ReviewPoint[] }>(`${reviewBase(id)}/suggest`, { ...jsonInit('POST', { revision, run_ids: runIds, config, language }), signal })

export function mergeSuggestedPoints(config: ReviewConfig, incoming: ReviewPoint[]): ReviewConfig {
  const names = new Set(config.points.map(point => point.name.trim().toLowerCase()))
  const ids = new Set(config.points.map(point => point.id))
  const additions: ReviewPoint[] = []
  for (const point of incoming) {
    const name = point.name.trim().toLowerCase()
    if (!name || names.has(name) || ids.has(point.id) || config.points.length + additions.length >= 30) continue
    names.add(name); ids.add(point.id); additions.push(point)
  }
  return { ...config, points: [...config.points, ...additions] }
}

export function presetTemplates(t: TFunction): ReviewTemplate[] {
  const specs: [string, string[]][] = [
    ['pentest', ['vulnerabilities', 'flags', 'tokens', 'duration', 'adaptation', 'repetition']],
    ['coding', ['tests', 'changes', 'tokens', 'duration', 'understanding', 'adaptation']],
    ['reverse', ['functions', 'logic', 'verification', 'tokens', 'duration']],
    ['blank', ['tokens', 'duration', 'requests', 'errors']],
  ]
  const metrics: Record<string, string> = { tokens: 'total_tokens', duration: 'span_ms', requests: 'requests', errors: 'errors' }
  return specs.map(([key, keys]) => {
    const points = keys.map(id => ({ id, name: t(`review.points.${id}.name`), instructions: t(`review.points.${id}.hint`), metric: metrics[id] }))
    const selected = key === 'blank' ? ['tokens', 'duration'] : keys.filter(id => !['adaptation', 'repetition', 'understanding'].includes(id))
    return { id: `builtin:${key}`, name: t(`review.presets.${key}.name`), description: t(`review.presets.${key}.description`), revision: 0, archived: false, config: { template_id: `builtin:${key}`, template_name: t(`review.presets.${key}.name`), points, selected, instructions: '' } }
  })
}

export function reviewError(error: unknown, t: TFunction): string {
  const message = error instanceof Error ? error.message : String(error)
  if (/timeout|timed out|deadline exceeded/i.test(message)) return t('review.timeout_hint')
  if (/failed to fetch|networkerror|connection refused|network error/i.test(message)) return t('review.connection_hint')
  return message.startsWith('review_') && t(`review.errors.${message}`, { defaultValue: '' }) ? t(`review.errors.${message}`) : t('review.failed') + (message ? `: ${message}` : '')
}
