import type { TFunction } from 'i18next'
import { apiRequest, type CaptureRun } from '@/lib/api'

export interface TaskPreviewContent {
  name: string
  summary?: string
  labels?: string[]
  details?: string
  facts?: { label: string; value: string }[]
}

export interface CapturePreview {
  traces: { id: string; name: string }[]
  trace_count: number
  error_count: number
  latest_error?: { status_code: number; path: string }
}

export interface ExperimentPreview {
  background: string
  tasks: { id: string; name: string; agent: string; model: string }[]
  task_count: number
  points: string[]
  point_count: number
  last_report_at?: string
}

export async function fetchTaskPreview<T>(kind: 'capture' | 'experiment', id: string, signal: AbortSignal): Promise<T> {
  const response = await apiRequest(`/api/task-previews/${kind}/${encodeURIComponent(id)}`, { signal }, 'task_preview.load_failed')
  return response.json()
}

export function hasPreviewContent(value: TaskPreviewContent | null): value is TaskPreviewContent {
  return !!value && !!(value.summary?.trim() || value.details?.trim() || value.labels?.length || value.facts?.some(fact => fact.value.trim()))
}

export function capturePreviewContent(run: CaptureRun, preview: CapturePreview | null, t: TFunction, language: string): TaskPreviewContent {
  const facts: NonNullable<TaskPreviewContent['facts']> = []
  if (preview && preview.trace_count > 0) {
    const names = preview.traces.map(trace => trace.name || t('task_preview.unnamed_trace', { id: trace.id.length > 18 ? `${trace.id.slice(0, 8)}…${trace.id.slice(-6)}` : trace.id }))
    const remaining = preview.trace_count - names.length
    if (remaining > 0) names.push(t('task_preview.more_traces', { count: remaining }))
    facts.push({ label: t('task_preview.traces'), value: names.join('\n') })
  }
  if (preview && run.request_count > 0) {
    facts.push({ label: t('task_preview.errors'), value: t(preview.error_count ? 'task_preview.error_count' : 'task_preview.no_errors', { count: preview.error_count }) })
    if (preview.latest_error) {
      const error = preview.latest_error
      const status = error.status_code > 0 ? `HTTP ${error.status_code}` : t('task_preview.no_http_status')
      facts.push({ label: t('task_preview.latest_error'), value: [status, error.path].filter(Boolean).join(' · ') })
    }
  }
  if (run.usage_missing_count > 0) facts.push({ label: t('task_preview.usage'), value: t('task_preview.usage_missing', { count: run.usage_missing_count }) })
  facts.push({ label: t('task_preview.recording_range'), value: `${new Date(run.started_at).toLocaleString(language)}\n→ ${run.stopped_at ? new Date(run.stopped_at).toLocaleString(language) : t('task_preview.recording_now')}` })
  facts.push({ label: t('task_preview.recording_mode'), value: t(run.single_task ? 'task_preview.single_task' : 'task_preview.ordinary_capture') })
  return { name: run.name, facts }
}

export function experimentPreviewContent(name: string, preview: ExperimentPreview, t: TFunction, language: string): TaskPreviewContent | null {
  const facts: NonNullable<TaskPreviewContent['facts']> = []
  if (preview.tasks.length > 0) {
    const tasks = preview.tasks.map(task => [task.name, [task.agent, task.model].filter(Boolean).join(' / ')].filter(Boolean).join(' · '))
    const remaining = preview.task_count - preview.tasks.length
    if (remaining > 0) tasks.push(t('task_preview.more_tasks', { count: remaining }))
    facts.push({ label: t('task_preview.tasks'), value: tasks.join('\n') })
  }
  if (preview.points.length > 0) {
    const remaining = preview.point_count - preview.points.length
    const points = [...preview.points]
    if (remaining > 0) points.push(t('task_preview.more_points', { count: remaining }))
    facts.push({ label: t('task_preview.points'), value: points.join(language.startsWith('zh') ? '、' : ', ') })
  }
  if (preview.last_report_at) facts.push({ label: t('task_preview.last_report'), value: new Date(preview.last_report_at).toLocaleString(language) })
  const content = { name, summary: preview.background, facts }
  return hasPreviewContent(content) ? content : null
}
