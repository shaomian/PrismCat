import { lazy, Suspense, useCallback, useEffect, useRef, useState, useSyncExternalStore, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, ChevronUp, Download, Loader2, Settings2, Sparkles, Square, History, Trash2, Wrench } from 'lucide-react'
import { toast } from 'sonner'
import { fetchAnalysisConfig, type AppConfig } from '@/lib/api'
import { saveExperiment, type Experiment } from '@/lib/experiments'
import { fetchReport, fetchReports, mergeSuggestedPoints, presetTemplates, reviewError, reviewHistoryOffset, suggestPoints, type ReviewConfig, type ReviewReport, type ReportSummary } from '@/lib/review'
import { reviewReportFilename, reviewReportMarkdown } from '@/lib/review-report'
import { deleteReviewReport, getReviewSession, startReview, stopReview, subscribeReview, type ReviewToolCall } from '@/lib/review-stream'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import ObservationPicker from './ObservationPicker'
import ReviewReportView from './ReviewReportView'
import ReviewTemplates, { SaveTemplateDialog } from './ReviewTemplates'
const ModelSettings = lazy(() => import('@/components/AnalysisModelSettings'))

/** 分析过程的调用流水。运行中在状态行展开,完成后本次会话内可回看。 */
function ToolCallList({ calls }: { calls: ReviewToolCall[] }) {
  const { t } = useTranslation()
  return <div className="mt-2 max-h-64 space-y-1.5 overflow-y-auto rounded-md bg-muted/50 p-3 text-xs leading-5">
    {calls.map((call, index) => (
      <div key={index} className="flex flex-col gap-0.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="shrink-0 font-medium text-foreground">{call.name}</span>
          <code className="min-w-0 flex-1 truncate text-muted-foreground">{call.args}</code>
          <span className="ml-auto flex shrink-0 items-center gap-2 text-muted-foreground">
            {call.remaining !== undefined && <span>{t('review.activity_remaining', { count: call.remaining })}</span>}
            <span>{call.error ? t('review.activity_failed') : call.chars !== undefined ? t('review.activity_chars', { count: call.chars }) : '…'}</span>
          </span>
        </div>
        {call.preview && <p className="truncate text-muted-foreground">{call.preview}</p>}
        {call.error && <p className="text-danger">{call.error}</p>}
      </div>
    ))}
  </div>
}

interface Props {
  experiment: Experiment
  selected: string[]
  taskPicker: ReactNode
  preparationLocked?: boolean
  disabled?: boolean
  onSaved: (value: Experiment) => void
  onRestoreSelection: (ids: string[]) => void
}

export default function ReviewPanel({ experiment, selected, taskPicker, preparationLocked = false, disabled = false, onSaved, onRestoreSelection }: Props) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<ReviewConfig>(() => structuredClone(experiment.review ?? presetTemplates(t)[3].config))
  const [model, setModel] = useState<AppConfig['analysis'] | null>(null)
  const [modelLoaded, setModelLoaded] = useState(false)
  const [settings, setSettings] = useState(false)
  const [templates, setTemplates] = useState(false)
  const [savingTemplate, setSavingTemplate] = useState<ReviewConfig | null>(null)
  const [suggesting, setSuggesting] = useState(false)
  const [suggestedIds, setSuggestedIds] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [lastReport, setLastReport] = useState<ReviewReport | null>(null)
  const [chosen, setChosen] = useState<ReviewReport | null>(null)
  const [history, setHistory] = useState<ReportSummary[]>([])
  const [historyOffset, setHistoryOffset] = useState(0)
  const [historyTotal, setHistoryTotal] = useState(0)
  const [historyLoaded, setHistoryLoaded] = useState(false)
  const [historyRevision, setHistoryRevision] = useState(0)
  const [question, setQuestion] = useState('')
  const [loadingReport, setLoadingReport] = useState('')
  const [deletingReport, setDeletingReport] = useState('')
  const deletingReportRef = useRef('')
  const suggestionController = useRef<AbortController | null>(null)
  const reportAnchor = useRef<HTMLElement | null>(null)
  const scrollAfterReview = useRef(false)
  const restored = useRef(false)
  const subscribe = useCallback((cb: () => void) => subscribeReview(experiment.id, cb), [experiment.id])
  const snapshot = useCallback(() => getReviewSession(experiment.id), [experiment.id])
  const session = useSyncExternalStore(subscribe, snapshot)
  const busy = disabled || saving || session.running || !!deletingReport
  const modelReady = !!model?.enabled && !!model.base_url && !!model.model
  const report = chosen ?? session.report ?? lastReport
  const completed = report?.status === 'completed' && !!report.output
  const preparing = !session.running && (editing || !completed || preparationLocked)
  const loadingInitialReport = !historyLoaded && !report && !session.running
  const language = i18n.resolvedLanguage ?? i18n.language
  const selectionKey = selected.join(',')
  useEffect(() => {
    if (!session.running && !session.error && completed && scrollAfterReview.current) {
      scrollAfterReview.current = false
      reportAnchor.current?.scrollIntoView({ block: 'start' })
    }
  }, [session.running, session.error, completed, report?.id])
  const loadModel = useCallback(async () => {
    try { setModel(await fetchAnalysisConfig()) } catch { setModel(null) }
    finally { setModelLoaded(true) }
  }, [])
  useEffect(() => { void loadModel() }, [loadModel])
  useEffect(() => () => { suggestionController.current?.abort() }, [selectionKey])
  useEffect(() => {
    let canceled = false
    void fetchReports(experiment.id, historyOffset).then(async data => {
      if (canceled) return
      const offset = reviewHistoryOffset(historyOffset, data.total)
      if (offset !== historyOffset) { setHistoryOffset(offset); return }
      setHistory(data.reports); setHistoryTotal(data.total)
      if (!data.total) setLastReport(null)
      const cached = getReviewSession(experiment.id).report
      if (!restored.current && cached) { onRestoreSelection(cached.tasks.map(task => task.run_id)); restored.current = true }
      if (!getReviewSession(experiment.id).running && !cached && data.reports[0] && historyOffset === 0) {
        const value = await fetchReport(experiment.id, data.reports[0].id)
        if (!canceled) { setLastReport(value); if (!restored.current) { onRestoreSelection(value.tasks.map(task => task.run_id)); restored.current = true } }
      }
    }).catch(reason => { if (!canceled) setError(reviewError(reason, t)) }).finally(() => { if (!canceled) setHistoryLoaded(true) })
    return () => { canceled = true }
  }, [experiment.id, session.version, historyOffset, historyRevision, t, onRestoreSelection])

  const analyze = async () => {
    setError(''); setChosen(null); setSaving(true)
    try {
      const saved = await saveExperiment({ ...experiment, review: draft })
      if (saved.review) setDraft(saved.review)
      onSaved(saved); setEditing(false); scrollAfterReview.current = true
      await startReview(saved.id, { revision: saved.revision, run_ids: selected, language })
      if (getReviewSession(saved.id).error) { scrollAfterReview.current = false; setEditing(true) }
    } catch (reason) { scrollAfterReview.current = false; setError(reviewError(reason, t)); setEditing(true) }
    finally { setSaving(false) }
  }
  const suggest = async () => {
    const controller = new AbortController(); suggestionController.current = controller
    setSuggesting(true); setNotice(''); setError('')
    try {
      const data = await suggestPoints(experiment.id, experiment.revision, selected, draft, language, controller.signal)
      if (controller.signal.aborted) return
      const next = mergeSuggestedPoints(draft, data.points)
      const additions = next.points.slice(draft.points.length).map(point => point.id)
      setDraft(next); setSuggestedIds(previous => [...previous, ...additions])
      setNotice(t(additions.length ? 'review.suggestions_added' : 'review.suggestions_empty'))
    } catch (reason) { if (!controller.signal.aborted) setError(reviewError(reason, t)) }
    finally { if (suggestionController.current === controller) { setSuggesting(false); suggestionController.current = null } }
  }
  const download = () => {
    if (!report) return
    try {
      const content = reviewReportMarkdown(report, experiment.name, t, language, window.location.origin)
      const url = URL.createObjectURL(new Blob([content], { type: 'text/markdown;charset=utf-8' }))
      const anchor = document.createElement('a')
      anchor.href = url; anchor.download = reviewReportFilename(experiment.name, report)
      document.body.appendChild(anchor); anchor.click(); anchor.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (reason) { toast.error((reason as Error).message) }
  }
  const removeReport = async (item: ReportSummary) => {
    if (deletingReportRef.current) return
    if (!window.confirm(t('review.delete_report_confirm'))) return
    deletingReportRef.current = item.id
    setDeletingReport(item.id); setError('')
    try {
      await deleteReviewReport(experiment.id, item.id)
      setChosen(value => value?.id === item.id ? null : value)
      setLastReport(value => value?.id === item.id ? null : value)
      setHistoryRevision(value => value + 1)
      toast.success(t('review.report_deleted'))
    } catch (reason) { setError(reviewError(reason, t)) }
    finally { deletingReportRef.current = ''; setDeletingReport('') }
  }
  const changed = !!report && (JSON.stringify(report.config) !== JSON.stringify(draft) || [...selected].sort().join(',') !== report.tasks.map(task => task.run_id).sort().join(','))
  return <div className="space-y-6">
    {loadingInitialReport && <p className="flex items-center gap-2 py-4 text-sm text-muted-foreground"><Loader2 className="size-4 animate-spin" />{t('common.loading')}</p>}
    {!loadingInitialReport && (completed || session.running) && <div className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-muted/50 px-3 py-2.5">
      <span className="min-w-0 break-words text-xs text-muted-foreground">{t('review.preparation_summary', { tasks: selected.length, points: draft.selected.length })}{draft.template_name ? ` · ${draft.template_name}` : ''}</span>
      <Button type="button" variant="ghost" size="sm" disabled={busy || suggesting || (preparing && preparationLocked)} onClick={() => setEditing(!preparing)}>{preparing ? <ChevronUp className="size-3.5" /> : <ChevronDown className="size-3.5" />}{t(preparing ? 'review.collapse_conditions' : 'review.adjust_conditions')}</Button>
    </div>}
    <div hidden={!preparing || loadingInitialReport} className="space-y-6">
      {taskPicker}
      <section className="space-y-3 border-t border-border pt-5">
        <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">{t('review.step_points')}</h3><Button size="sm" variant="ghost" disabled={busy || suggesting} onClick={() => setTemplates(true)}>{t('review.change_template')}</Button></div>
        <ObservationPicker value={draft} disabled={busy || suggesting} onChange={setDraft} suggestedIds={suggestedIds} actions={<Button type="button" size="sm" variant="outline" className="text-primary" disabled={busy || suggesting || !modelLoaded || !selected.length || draft.points.length >= 30} onClick={() => { if (modelReady) void suggest(); else setSettings(true) }}>{suggesting ? <Loader2 className="size-3.5 animate-spin" /> : <Sparkles className="size-3.5" />}{t(suggesting ? 'review.suggesting' : 'review.suggest')}</Button>} />
        <p className="text-xs text-muted-foreground">{t(modelLoaded && !modelReady ? 'review.model_required' : 'review.suggestion_hint')}</p>
        {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
        <details className="text-xs text-muted-foreground"><summary className="w-fit cursor-pointer">{t('review.instructions')}</summary><Textarea className="mt-2" aria-label={t('review.instructions')} rows={2} maxLength={2600} placeholder={t('review.instructions_placeholder')} value={draft.instructions} disabled={busy || suggesting} onChange={event => setDraft(value => ({ ...value, instructions: event.target.value }))} /></details>
      </section>
    </div>
    {!loadingInitialReport && (preparing || session.running) && <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border pt-4">
      <Button type="button" size="sm" variant="ghost" className="h-auto max-w-full whitespace-normal text-left text-xs text-muted-foreground" disabled={busy || suggesting} onClick={() => setSettings(true)}><Settings2 className="size-3.5 shrink-0" />{!modelLoaded ? t('common.loading') : modelReady ? t('review.using_model', { model: model?.model }) : t('review.model_settings')}</Button>
      <div className="flex flex-wrap items-center gap-3"><span className="text-xs text-muted-foreground">{selected.length < 2 ? t('review.choose_two') : t('review.ready_hint', { count: selected.length })}</span>{session.running ? <Button variant="outline" onClick={() => stopReview(experiment.id)}><Square className="size-4" />{t('review.stop')}</Button> : <Button disabled={busy || suggesting || !modelReady || selected.length < 2 || !draft.selected.length || draft.points.some(point => !point.name.trim())} onClick={() => void analyze()}><Sparkles className="size-4" />{t('review.start')}</Button>}</div>
    </div>}
    {session.running && (session.calls.length ? (
      <details role="status" className="text-sm" data-testid="review-activity">
        <summary className="flex cursor-pointer flex-wrap items-center gap-2">
          <Loader2 className="size-4 shrink-0 animate-spin" />
          {t(`review.phases.${session.phase}`)}
          <span className="text-xs text-muted-foreground">{t('review.activity_title', { count: session.calls.filter(call => call.chars !== undefined || call.error).length })}</span>
          <span className="ml-auto text-xs text-muted-foreground">{t('review.continues_hint')}</span>
        </summary>
        <ToolCallList calls={session.calls} />
      </details>
    ) : (
      <div role="status" className="flex flex-wrap items-center gap-2 text-sm"><Loader2 className="size-4 shrink-0 animate-spin" />{t(`review.phases.${session.phase}`)}<span className="ml-auto text-xs text-muted-foreground">{t('review.continues_hint')}</span></div>
    ))}
    {!session.running && session.calls.length > 0 && (
      <details className="text-xs text-muted-foreground">
        <summary className="flex w-fit cursor-pointer items-center gap-1"><Wrench className="size-3.5" />{t('review.activity_title', { count: session.calls.filter(call => call.chars !== undefined || call.error).length })}</summary>
        <ToolCallList calls={session.calls} />
      </details>
    )}
    {(error || session.error) && <p role="alert" className="text-sm text-danger">{error || reviewError(new Error(session.error), t)}</p>}
    {report && !session.running && <section ref={reportAnchor} className="scroll-mt-20 space-y-5 border-t border-border pt-5" aria-label={t('review.report_title')}>
      <header className="flex flex-wrap items-start justify-between gap-3 border-b border-border pb-4"><div className="min-w-0 space-y-1"><h3 className="font-medium">{t(report.question ? 'review.followup_result' : 'review.report_title')}</h3><p className="break-words text-xs text-muted-foreground">{t('review.report_meta', { model: report.model, time: new Date(report.created_at).toLocaleString() })}</p></div><Button type="button" size="sm" variant="outline" disabled={!completed} onClick={download}><Download className="size-3.5" />{t('review.export_markdown')}</Button></header>
      {report.question && <p className="whitespace-pre-wrap text-sm text-muted-foreground">{report.question}</p>}
      {changed && <p className="text-xs text-muted-foreground">{t('review.changed_hint')}</p>}
      <ReviewReportView key={report.id} report={report} />
      {completed && <details className="border-t border-border pt-4 text-sm"><summary className="w-fit cursor-pointer">{t('review.followup')}</summary><div className="mt-3 flex flex-col gap-2 sm:flex-row"><Textarea aria-label={t('review.followup')} placeholder={t('review.followup_placeholder')} value={question} rows={2} maxLength={2600} onChange={event => setQuestion(event.target.value)} /><Button className="self-end" disabled={busy || suggesting || !question.trim() || !modelReady} onClick={() => { setChosen(null); setError(''); setEditing(false); scrollAfterReview.current = true; const text = question.trim(); void startReview(experiment.id, { parent_id: report.id, question: text, language }).then(() => { if (getReviewSession(experiment.id).error) scrollAfterReview.current = false; else setQuestion('') }) }}>{t('review.followup')}</Button></div></details>}
    </section>}
    {(historyTotal > 0 || completed) && <div className="flex flex-wrap items-start justify-between gap-3 border-t border-border pt-3">
      {historyTotal > 0 && <details className="min-w-0 flex-1 text-xs"><summary className="flex w-fit cursor-pointer items-center gap-1 text-muted-foreground"><History className="size-3.5" />{t('review.history', { count: historyTotal })}</summary><div className="mt-2 space-y-1">{history.map(item => <div key={item.id} className="flex items-center gap-1"><button type="button" className="flex min-w-0 flex-1 items-center justify-between gap-3 rounded p-2 text-left hover:bg-accent disabled:opacity-50" disabled={busy || !!loadingReport} aria-pressed={report?.id === item.id} onClick={async () => {
        setLoadingReport(item.id)
        try { const value = await fetchReport(experiment.id, item.id); scrollAfterReview.current = value.status === 'completed'; setChosen(value) } catch (reason) { setError(reviewError(reason, t)) } finally { setLoadingReport('') }
      }}><span className="truncate">{new Date(item.created_at).toLocaleString()} · {item.question || t('review.last_result')}</span><span className="shrink-0">{loadingReport === item.id ? t('common.loading') : t(`review.status.${item.status}`)}</span></button>
        {item.status !== 'running' && <Button type="button" size="icon-xs" variant="ghost" className="shrink-0 text-muted-foreground hover:text-danger" disabled={busy || !!loadingReport} aria-label={t('review.delete_report')} aria-busy={deletingReport === item.id} onClick={() => { void removeReport(item) }}><Trash2 className="size-3.5" /></Button>}</div>)}</div>{(historyTotal > 10 || historyOffset > 0) && <div className="mt-2 flex gap-2"><Button size="xs" variant="outline" disabled={busy || !historyOffset} onClick={() => setHistoryOffset(Math.max(0, historyOffset - 10))}>{t('correlation.previous')}</Button><Button size="xs" variant="outline" disabled={busy || historyOffset + 10 >= historyTotal} onClick={() => setHistoryOffset(historyOffset + 10)}>{t('correlation.next')}</Button></div>}</details>}
      {completed && <Button type="button" size="sm" variant="ghost" className="ml-auto text-xs text-muted-foreground" disabled={busy} onClick={() => setSavingTemplate(report.config)}>{t('review.save_as_template')}</Button>}
    </div>}
    {settings && <Suspense fallback={<p>{t('common.loading')}</p>}><ModelSettings value={model} onClose={() => setSettings(false)} onSaved={value => { setModel(value); setSettings(false) }} /></Suspense>}
    {savingTemplate && <SaveTemplateDialog config={savingTemplate} onClose={() => setSavingTemplate(null)} />}
    <Dialog open={templates} onOpenChange={setTemplates}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t('review.choose_template')}</DialogTitle><DialogDescription>{t('review.change_template_hint')}</DialogDescription></DialogHeader><ReviewTemplates onChoose={value => { setDraft(structuredClone(value.config)); setTemplates(false); setNotice(''); setSuggestedIds([]) }} /></DialogContent></Dialog>
  </div>
}
