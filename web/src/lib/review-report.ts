import type { TFunction } from 'i18next'
import type { RunMetrics } from './experiments'
import type { ReviewFinding, ReviewReport, ReviewTask } from './review'
import { formatLatency } from './utils'

const metricKeys = ['total_tokens', 'input_tokens', 'output_tokens', 'span_ms', 'requests', 'errors'] as const
const number = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0
export function reviewMetricValue(metrics: RunMetrics | undefined, metric: string, t: TFunction, language?: string): string {
  const value = metrics?.[metric as keyof RunMetrics]
  if (!number(value)) return t('review.not_recorded')
  return metric === 'span_ms' ? formatLatency(value) : value.toLocaleString(language)
}

export function reviewFindingDisplay(report: ReviewReport, finding: ReviewFinding, metric: string | undefined, t: TFunction, language?: string) {
  if (metric && metricKeys.includes(metric as typeof metricKeys[number])) {
    const task = report.tasks.find(task => task.run_id === finding.run_id)
    const recorded = number(task?.metrics?.[metric as keyof RunMetrics])
    return { value: reviewMetricValue(task?.metrics, metric, t, language), source: recorded ? 'statistics' : 'unknown' }
  }
  return { value: finding.value, source: ['statistics', 'logs', 'inference', 'unknown'].includes(finding.source) ? finding.source : 'unknown' }
}

export function reviewUsage(report: ReviewReport) {
  const input = report.usage?.prompt_tokens ?? report.usage?.input_tokens
  const output = report.usage?.completion_tokens ?? report.usage?.output_tokens
  const total = report.usage?.total_tokens ?? (number(input) && number(output) ? input + output : undefined)
  return { input: number(input) ? input : undefined, output: number(output) ? output : undefined, total: number(total) ? total : undefined }
}

export function reviewHasPartialData(task: ReviewTask) {
  return !!task.metrics?.sampled || !!task.metrics?.incomplete || (task.metrics?.usage_known ?? 0) < (task.metrics?.requests ?? 0)
}

// All report content is plain text in the UI. Preserve that meaning in Markdown,
// including untrusted names, multiline explanations, code fences and HTML.
const text = (value: string) => value.replace(/\r\n?/g, '\n').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  .replace(/[\\`*_{}[\]#|]/g, '\\$&').replace(/^(\s*)([-+]|\d+[.)])(?=\s)/gm, '$1\\$2')
const cell = (value: string) => text(value).replace(/\n/g, '<br>')
const segment = (value: string) => encodeURIComponent(value).replace(/[()]/g, char => char === '(' ? '%28' : '%29')
export function reviewEvidenceUrl(report: ReviewReport, id: string, origin: string): string | undefined {
  try {
    const url = new URL(origin)
    if (!['http:', 'https:'].includes(url.protocol)) return undefined
    url.username = ''; url.password = ''; url.search = ''; url.hash = ''
    url.pathname = `/api/experiments/${segment(report.experiment_id)}/review/reports/${segment(report.id)}/logs/${segment(id)}`
    return url.href
  } catch { return undefined }
}

export function reviewReportMarkdown(report: ReviewReport, title: string, t: TFunction, language?: string, origin = ''): string {
  if (report.status !== 'completed' || !report.output) throw new Error(t('review.export_unavailable'))
  const lines: string[] = [`# ${text(title || t('review.report_title'))}`, '']
  const item = (label: string, value: string) => { lines.push(`- ${text(label)}: ${text(value)}`) }
  const section = (label: string) => { lines.push('', `## ${text(label)}`, '') }
  item(t('review.export_created'), report.created_at)
  if (report.evidence_at) item(t('review.export_evidence_as_of'), report.evidence_at)
  item(t('review.export_analysis_model'), report.model || t('review.not_recorded'))
  item(t('review.export_report_id'), report.id)
  if (report.parent_id) item(t('review.export_parent_id'), report.parent_id)
  if (report.config.template_name) item(t('review.export_template'), report.config.template_name)
  if (report.background) { section(t('review.export_background')); lines.push(text(report.background)) }
  if (report.question) { section(t('review.followup')); lines.push(text(report.question)) }
  section(t('review.report_summary')); lines.push(text(report.output.summary))
  section(t('review.report_statistics'))
  const labels = [t('review.export_task'), t('experiments.metrics.total_tokens'), t('experiments.metrics.span'), t('experiments.metrics.requests'), t('experiments.metrics.errors')]
  lines.push(`| ${labels.map(cell).join(' | ')} |`, '| --- | ---: | ---: | ---: | ---: |')
  for (const task of report.tasks) {
    lines.push(`| ${[task.name, ...['total_tokens', 'span_ms', 'requests', 'errors'].map(metric => reviewMetricValue(task.metrics, metric, t, language))].map(cell).join(' | ')} |`)
  }
  if (report.tasks.some(reviewHasPartialData)) lines.push('', text(t('review.partial_data')))
  section(t('review.export_tasks'))
  for (const task of report.tasks) {
    lines.push(`### ${text(task.name)}`, '')
    item(t('experiments.agent'), task.agent || t('review.not_recorded'))
    item(t('review.export_task_model'), task.model || t('review.not_recorded'))
    item(t('review.export_run_id'), task.run_id)
    if (task.prompt) lines.push('', `${text(t('review.export_task_prompt'))}:`, '', text(task.prompt))
    if (task.notes) lines.push('', `${text(t('review.task_notes'))}:`, '', text(task.notes))
    for (const warning of task.warnings ?? []) {
      const key = `experiments.warnings.${warning}`
      lines.push('', text(t(key, { defaultValue: warning })))
    }
    lines.push('')
  }
  section(t('review.export_points'))
  for (const id of report.config.selected) {
    const point = report.config.points.find(point => point.id === id)
    if (!point) continue
    lines.push(`- ${text(point.name)}${point.instructions ? `: ${text(point.instructions).replace(/\n/g, ' ')}` : ''}`)
  }
  if (report.config.instructions) lines.push('', `${text(t('review.instructions'))}:`, '', text(report.config.instructions))
  section(t('review.report_observations'))
  lines.push(text(t('review.evidence_hint')), '')
  for (const observation of report.output.observations ?? []) {
    const point = report.config.points.find(point => point.id === observation.point_id)
    lines.push(`### ${text(point?.name ?? observation.point_id)}`, '')
    for (const finding of observation.findings ?? []) {
      const task = report.tasks.find(task => task.run_id === finding.run_id)
      const display = reviewFindingDisplay(report, finding, point?.metric, t, language)
      lines.push(`#### ${text(task?.name ?? finding.run_id)}`, '', text(display.value), '', `${text(t('review.export_source'))}: ${text(t(`review.sources.${display.source}`))}`)
      if (finding.explanation) lines.push('', text(finding.explanation))
      for (const id of finding.evidence ?? []) {
        const url = reviewEvidenceUrl(report, id, origin)
        lines.push(`- ${text(t('review.export_log_id'))}: ${url ? `[${text(id)}](${url})` : text(id)}`)
      }
      lines.push('')
    }
    if (observation.comparison) lines.push(`**${text(t('review.report_comparison'))}**`, '', text(observation.comparison), '')
  }
  if (report.output.recommendations?.length) {
    section(t('review.recommendations'))
    for (const recommendation of report.output.recommendations) lines.push(`- ${text(recommendation).replace(/\n/g, ' ')}`)
  }
  const usage = reviewUsage(report)
  if (usage.total != null || usage.input != null || usage.output != null) {
    section(t('review.analysis_usage'))
    for (const [label, value] of [['total_tokens', usage.total], ['input_tokens', usage.input], ['output_tokens', usage.output]] as const) {
      if (value != null) item(t(`experiments.metrics.${label}`), value.toLocaleString(language))
    }
  }
  return `${lines.join('\n').trim()}\n`
}

export function reviewReportFilename(title: string, report: Pick<ReviewReport, 'id' | 'created_at'>) {
  const safe = (value: string, limit: number) => Array.from(Array.from(value, char => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127 ? '-' : char).join('').replace(/[<>:"/\\|?*]/g, '-').replace(/\s+/g, ' ').replace(/[. ]+$/g, '').trim()).slice(0, limit).join('')
  const date = new Date(report.created_at)
  const stamp = Number.isNaN(date.getTime()) ? 'report' : date.toISOString().slice(0, 19).replace(/[-:]/g, '').replace('T', '_')
  return `${safe(title, 48) || 'PrismCat'}_${stamp}_${safe(report.id, 8) || 'report'}.md`
}
