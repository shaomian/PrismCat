import { lazy, Suspense, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FileSearch } from 'lucide-react'
import { toast } from 'sonner'
import type { RequestLog } from '@/lib/api'
import { fetchReviewLog, reviewError, type ReviewReport, type ReviewFinding } from '@/lib/review'
import { formatLatency } from '@/lib/utils'
import { Button } from '@/components/ui/button'
const LogDetail = lazy(() => import('@/components/LogDetail').then(m => ({ default: m.LogDetail })))

export default function ReviewReportView({ report }: { report: ReviewReport }) {
  const { t } = useTranslation()
  const [log, setLog] = useState<RequestLog | null>(null)
  const [reading, setReading] = useState('')
  const input = report.usage?.prompt_tokens ?? report.usage?.input_tokens
  const generated = report.usage?.completion_tokens ?? report.usage?.output_tokens
  const total = report.usage?.total_tokens ?? (typeof input === 'number' && typeof generated === 'number' ? input + generated : undefined)
  const output = report.output
  if (!output) return <p className="rounded-lg bg-muted p-3 text-sm text-muted-foreground">{t(`review.status.${report.status}`)}{report.error ? ` · ${reviewError(new Error(report.error), t)}` : ''}</p>
  const open = async (id: string) => {
    setReading(id)
    try { setLog(await fetchReviewLog(report.experiment_id, report.id, id)) }
    catch (e) { toast.error(reviewError(e, t)) }
    finally { setReading('') }
  }
  const value = (finding: ReviewFinding, metric?: string) => {
    if (finding.source !== 'statistics' || !metric) return finding.value
    const task = report.tasks.find(task => task.run_id === finding.run_id)
    if (!task) return finding.value
    if (metric === 'span_ms') return task.metrics.span_ms == null ? '—' : formatLatency(task.metrics.span_ms)
    const n = task.metrics[metric as keyof typeof task.metrics]
    return typeof n === 'number' ? n.toLocaleString() : finding.value
  }
  return <div className="space-y-4">
    <p className="whitespace-pre-wrap rounded-lg bg-primary/5 p-4 text-sm leading-relaxed">{output.summary}</p>
    <p className="text-xs leading-relaxed text-muted-foreground">{t('review.evidence_hint')}</p>
    {report.tasks.some(task => task.metrics.sampled || task.metrics.incomplete || task.metrics.usage_known < task.metrics.requests) && <p className="text-xs text-warning">{t('review.partial_data')}</p>}
    {(output.observations ?? []).map((observation, index) => {
      const point = report.config.points.find(p => p.id === observation.point_id)
      return <details key={observation.point_id} className="rounded-lg border border-border" open={index < 2}>
        <summary className="cursor-pointer p-3 text-sm font-medium">{point?.name ?? observation.point_id}</summary>
        <div className="space-y-3 px-3 pb-3">
          <div className="overflow-x-auto"><table className="w-full text-sm"><thead><tr>{observation.findings.map(finding => <th key={finding.run_id} className="min-w-44 p-2 text-left text-xs font-medium text-muted-foreground">{report.tasks.find(task => task.run_id === finding.run_id)?.name ?? '—'}</th>)}</tr></thead>
            <tbody><tr>{observation.findings.map(finding => <td key={finding.run_id} className="p-2 align-top">
              <p className="whitespace-pre-wrap font-medium">{value(finding, point?.metric)}</p>
              <p className="mt-1 text-xs text-muted-foreground">{t(`review.sources.${finding.source}`)}</p>
              {finding.explanation && <p className="mt-2 whitespace-pre-wrap text-xs leading-relaxed">{finding.explanation}</p>}
              <div className="mt-2 flex flex-wrap gap-1">{finding.evidence.map((id, i) => <Button size="xs" variant="outline" key={id} disabled={!!reading} onClick={() => void open(id)}><FileSearch className="size-3" />{reading === id ? t('common.loading') : t('review.evidence', { index: i + 1 })}</Button>)}</div>
            </td>)}</tr></tbody></table></div>
          {observation.comparison && <p className="whitespace-pre-wrap rounded-md bg-muted p-3 text-xs leading-relaxed">{observation.comparison}</p>}
        </div>
      </details>
    })}
    {(output.recommendations ?? []).length > 0 && <div className="space-y-2 rounded-lg border border-border p-4"><h4 className="text-sm font-medium">{t('review.recommendations')}</h4><ul className="list-disc space-y-2 pl-5 text-sm leading-relaxed">{(output.recommendations ?? []).map((text, i) => <li key={i}>{text}</li>)}</ul></div>}
    <p className="text-xs text-muted-foreground">{t('review.report_meta', { model: report.model, time: new Date(report.created_at).toLocaleString() })}</p>
    <details className="text-xs text-muted-foreground"><summary className="cursor-pointer">{t('review.analysis_usage')}</summary><div className="mt-2 flex flex-wrap gap-4">{[['total_tokens', total], ['input_tokens', input], ['output_tokens', generated]].map(([label, count]) => <span key={String(label)}>{t(`experiments.metrics.${label}`)}: {typeof count === 'number' ? count.toLocaleString() : '—'}</span>)}</div></details>
    {log && <Suspense fallback={<p>{t('common.loading')}</p>}><LogDetail log={log} onClose={() => setLog(null)} /></Suspense>}
  </div>
}
