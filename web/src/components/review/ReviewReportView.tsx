import { lazy, memo, Suspense, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { FileSearch } from 'lucide-react'
import { toast } from 'sonner'
import type { RequestLog } from '@/lib/api'
import { fetchReviewLog, reviewError, type ReviewReport } from '@/lib/review'
import { reviewFindingDisplay, reviewHasPartialData, reviewMetricValue, reviewUsage } from '@/lib/review-report'
import { Button } from '@/components/ui/button'
const LogDetail = lazy(() => import('@/components/LogDetail').then(module => ({ default: module.LogDetail })))

function ReviewReportView({ report }: { report: ReviewReport }) {
  const { t, i18n } = useTranslation()
  const [log, setLog] = useState<RequestLog | null>(null)
  const [reading, setReading] = useState('')
  const usage = reviewUsage(report)
  const language = i18n.resolvedLanguage ?? i18n.language
  const output = report.output
  if (!output) return <p className="py-3 text-sm text-muted-foreground">{t(`review.status.${report.status}`)}{report.error ? ` · ${reviewError(new Error(report.error), t)}` : ''}</p>
  const open = async (id: string) => {
    setReading(id)
    try { setLog(await fetchReviewLog(report.experiment_id, report.id, id)) }
    catch (reason) { toast.error(reviewError(reason, t)) }
    finally { setReading('') }
  }
  return <article className="space-y-5">
    <section className="space-y-2 border-l-2 border-primary/50 pl-4"><h4 className="text-sm font-medium">{t('review.report_summary')}</h4><p className="whitespace-pre-wrap break-words text-sm leading-relaxed">{output.summary}</p></section>
    {report.background && <details className="text-sm"><summary className="w-fit cursor-pointer text-muted-foreground">{t('review.export_background')}</summary><p className="mt-2 whitespace-pre-wrap break-words text-xs leading-relaxed">{report.background}</p></details>}
    <section className="space-y-2" aria-label={t('review.report_statistics')}>
      <h4 className="text-sm font-medium">{t('review.report_statistics')}</h4>
      <div className="overflow-x-auto"><table className="w-full text-xs"><thead><tr className="border-b border-border text-muted-foreground"><th className="py-2 pr-3 text-left font-normal">{t('review.export_task')}</th>{['total_tokens', 'span', 'requests', 'errors'].map(metric => <th key={metric} className="whitespace-nowrap px-2 py-2 text-right font-normal">{t(`experiments.metrics.${metric}`)}</th>)}</tr></thead><tbody>{report.tasks.map(task => <tr key={task.run_id} className="border-b border-border"><td className="min-w-32 py-3 pr-3"><p className="break-words font-medium">{task.name}</p>{[task.agent !== t('review.unlabelled_agent') ? task.agent : '', task.model].some(Boolean) && <p className="mt-1 break-words text-muted-foreground">{[task.agent !== t('review.unlabelled_agent') ? task.agent : '', task.model].filter(Boolean).join(' / ')}</p>}</td>{['total_tokens', 'span_ms', 'requests', 'errors'].map(metric => <td key={metric} className="whitespace-nowrap px-2 py-3 text-right align-top font-mono tabular-nums">{reviewMetricValue(task.metrics, metric, t, language)}</td>)}</tr>)}</tbody></table></div>
      {report.tasks.some(reviewHasPartialData) && <p className="text-xs text-warning">{t('review.partial_data')}</p>}
    </section>
    <p className="text-xs leading-relaxed text-muted-foreground">{t('review.evidence_hint')}</p>
    <div>{(output.observations ?? []).map((observation, index) => {
      const point = report.config.points.find(point => point.id === observation.point_id)
      return <details key={observation.point_id} className="border-t border-border py-4" open={index < 2}>
        <summary className="cursor-pointer text-sm font-medium">{point?.name ?? observation.point_id}</summary>
        <div className="mt-4 space-y-4">
          <div className="grid gap-5 sm:grid-cols-2">{observation.findings.map(finding => {
            const display = reviewFindingDisplay(report, finding, point?.metric, t, language)
            return <section key={finding.run_id} className="min-w-0 space-y-2">
              <h5 className="break-words text-xs text-muted-foreground">{report.tasks.find(task => task.run_id === finding.run_id)?.name ?? finding.run_id}</h5>
              <p className="whitespace-pre-wrap break-words text-sm font-medium">{display.value}</p>
              <p className="text-xs text-muted-foreground">{t(`review.sources.${display.source}`)}</p>
              {finding.explanation && <p className="whitespace-pre-wrap break-words text-xs leading-relaxed">{finding.explanation}</p>}
              {!!finding.evidence?.length && <div className="flex flex-wrap gap-1">{finding.evidence.map((id, index) => <Button type="button" size="xs" variant="ghost" className="text-primary" key={id} disabled={!!reading} onClick={() => void open(id)}><FileSearch className="size-3" />{reading === id ? t('common.loading') : t('review.evidence', { index: index + 1 })}</Button>)}</div>}
            </section>
          })}</div>
          {observation.comparison && <p className="whitespace-pre-wrap break-words border-l-2 border-border pl-3 text-xs leading-relaxed">{observation.comparison}</p>}
        </div>
      </details>
    })}</div>
    {!!output.recommendations?.length && <section className="space-y-3 border-t border-border pt-4"><h4 className="text-sm font-medium">{t('review.recommendations')}</h4><ul className="list-disc space-y-2 pl-5 text-sm leading-relaxed">{output.recommendations.map((text, index) => <li className="whitespace-pre-wrap break-words" key={index}>{text}</li>)}</ul></section>}
    <details className="text-xs text-muted-foreground"><summary className="w-fit cursor-pointer">{t('review.analysis_usage')}</summary><div className="mt-2 flex flex-wrap gap-4">{[['total_tokens', usage.total], ['input_tokens', usage.input], ['output_tokens', usage.output]].map(([label, count]) => <span key={String(label)}>{t(`experiments.metrics.${label}`)}: {typeof count === 'number' ? count.toLocaleString(language) : t('review.not_recorded')}</span>)}</div></details>
    {log && <Suspense fallback={<p>{t('common.loading')}</p>}><LogDetail log={log} onClose={() => setLog(null)} /></Suspense>}
  </article>
}

export default memo(ReviewReportView)
