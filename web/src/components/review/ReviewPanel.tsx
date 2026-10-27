import { lazy, Suspense, useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2, Settings2, Sparkles, Square, History } from 'lucide-react'
import { fetchAnalysisConfig, type AppConfig } from '@/lib/api'
import { saveExperiment, type Experiment } from '@/lib/experiments'
import { fetchReport, fetchReports, presetTemplates, reviewError, suggestPoints, type ReviewConfig, type ReviewReport, type ReportSummary } from '@/lib/review'
import { getReviewSession, startReview, stopReview, subscribeReview } from '@/lib/review-stream'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import ObservationPicker from './ObservationPicker'
import ReviewReportView from './ReviewReportView'
import ReviewTemplates, { SaveTemplateDialog } from './ReviewTemplates'
const ModelSettings = lazy(() => import('@/components/AnalysisModelSettings'))

export default function ReviewPanel({ experiment, selected, onSaved, onRestoreSelection }: { experiment: Experiment; selected: string[]; onSaved: (value: Experiment) => void; onRestoreSelection: (ids: string[]) => void }) {
  const { t, i18n } = useTranslation()
  const [draft, setDraft] = useState<ReviewConfig>(() => structuredClone(experiment.review ?? presetTemplates(t)[3].config))
  const [model, setModel] = useState<AppConfig['analysis'] | null>(null)
  const [modelLoaded, setModelLoaded] = useState(false)
  const [settings, setSettings] = useState(false)
  const [templates, setTemplates] = useState(false)
  const [savingTemplate, setSavingTemplate] = useState<ReviewConfig | null>(null)
  const [suggesting, setSuggesting] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [lastReport, setLastReport] = useState<ReviewReport | null>(null)
  const [chosen, setChosen] = useState<ReviewReport | null>(null)
  const [history, setHistory] = useState<ReportSummary[]>([])
  const [historyOffset, setHistoryOffset] = useState(0)
  const [historyTotal, setHistoryTotal] = useState(0)
  const [question, setQuestion] = useState('')
  const [loadingReport, setLoadingReport] = useState('')
  const suggestionController = useRef<AbortController | null>(null)
  const restored = useRef(false)
  const subscribe = useCallback((cb: () => void) => subscribeReview(experiment.id, cb), [experiment.id])
  const snapshot = useCallback(() => getReviewSession(experiment.id), [experiment.id])
  const session = useSyncExternalStore(subscribe, snapshot)
  const busy = saving || session.running
  const modelReady = !!model?.enabled && !!model.base_url && !!model.model
  const report = chosen ?? session.report ?? lastReport
  const selectionKey = selected.join(',')
  const loadModel = useCallback(async () => {
    try { setModel(await fetchAnalysisConfig()) } catch { setModel(null) }
    finally { setModelLoaded(true) }
  }, [])
  useEffect(() => { void loadModel() }, [loadModel])
  useEffect(() => {
    return () => { suggestionController.current?.abort() }
  }, [selectionKey])
  useEffect(() => {
    let canceled = false
    void fetchReports(experiment.id, historyOffset).then(async data => {
      if (canceled) return
      setHistory(data.reports); setHistoryTotal(data.total)
      const cached = getReviewSession(experiment.id).report
      if (!restored.current && cached) { onRestoreSelection(cached.tasks.map(task => task.run_id)); restored.current = true }
      if (!getReviewSession(experiment.id).running && !getReviewSession(experiment.id).report && data.reports[0] && historyOffset === 0) {
        const value = await fetchReport(experiment.id, data.reports[0].id)
        if (!canceled) { setLastReport(value); if (!restored.current) { onRestoreSelection(value.tasks.map(task => task.run_id)); restored.current = true } }
      }
    }).catch(e => { if (!canceled) setError(reviewError(e, t)) })
    return () => { canceled = true }
  }, [experiment.id, session.version, historyOffset, t, onRestoreSelection])

  const analyze = async () => {
    setError(''); setChosen(null); setSaving(true)
    try {
      const saved = await saveExperiment({ ...experiment, review: draft })
      if (saved.review) setDraft(saved.review)
      onSaved(saved)
      await startReview(saved.id, { revision: saved.revision, run_ids: selected, language: i18n.resolvedLanguage ?? i18n.language })
    } catch (e) { setError(reviewError(e, t)) }
    finally { setSaving(false) }
  }
  const suggest = async () => {
    const controller = new AbortController(); suggestionController.current = controller
    setSuggesting(true); setNotice(''); setError('')
    try {
      const data = await suggestPoints(experiment.id, experiment.revision, selected, draft, i18n.resolvedLanguage ?? i18n.language, controller.signal)
      if (controller.signal.aborted) return
      setDraft(value => {
        const names = new Set(value.points.map(p => p.name.trim().toLowerCase()))
        const additions = data.points.filter(p => !names.has(p.name.trim().toLowerCase())).slice(0, Math.max(0, 30 - value.points.length))
        return { ...value, points: [...value.points, ...additions] }
      })
      setNotice(t(data.points.length ? 'review.suggestions_added' : 'review.suggestions_empty'))
    } catch (e) { if (!controller.signal.aborted) setError(reviewError(e, t)) }
    finally { if (suggestionController.current === controller) { setSuggesting(false); suggestionController.current = null } }
  }
  const changed = !!report && (JSON.stringify(report.config) !== JSON.stringify(draft) || selected.length > 0 && selected.join(',') !== report.tasks.map(task => task.run_id).join(','))
  return <div className="space-y-5">
    <section className="space-y-4 rounded-xl border border-border bg-card p-4 md:p-5">
      <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">{t('review.step_points')}</h3><Button size="sm" variant="ghost" disabled={busy || suggesting} onClick={() => setTemplates(true)}>{t('review.change_template')}</Button></div>
      <p className="text-sm text-muted-foreground">{t('review.points_intro')}</p>
      <ObservationPicker value={draft} disabled={busy || suggesting} onChange={setDraft} />
      <div className="flex flex-wrap items-center gap-3">
        <Button size="sm" variant="outline" disabled={busy || suggesting || !modelLoaded || !selected.length || draft.points.length >= 30} onClick={() => { if (modelReady) void suggest(); else setSettings(true) }}>{suggesting ? <Loader2 className="size-4 animate-spin" /> : <Sparkles className="size-4" />}{t(suggesting ? 'review.suggesting' : 'review.suggest')}</Button>
        <span className="text-xs text-muted-foreground">{t(modelLoaded && !modelReady ? 'review.model_required' : 'review.suggestion_hint')}</span>
      </div>
      {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
      <label className="block space-y-1 text-xs text-muted-foreground">{t('review.instructions')}<Textarea rows={2} maxLength={2600} placeholder={t('review.instructions_placeholder')} value={draft.instructions} disabled={busy || suggesting} onChange={e => setDraft(v => ({ ...v, instructions: e.target.value }))} /></label>
    </section>
    <section className="space-y-4 rounded-xl border border-border bg-card p-4 md:p-5">
      <h3 className="font-medium">{t('review.step_analyze')}</h3>
      <div className="flex flex-wrap items-center gap-3">
        {session.running ? <Button variant="outline" onClick={() => stopReview(experiment.id)}><Square className="size-4" />{t('review.stop')}</Button> : <Button disabled={busy || suggesting || !modelReady || selected.length < 2 || !draft.selected.length || draft.points.some(p => !p.name.trim())} onClick={() => void analyze()}><Sparkles className="size-4" />{t('review.start')}</Button>}
        <span className="text-xs text-muted-foreground">{selected.length < 2 ? t('review.choose_two') : t('review.ready_hint', { count: selected.length })}</span>
      </div>
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><span>{!modelLoaded ? t('common.loading') : modelReady ? t('review.using_model', { model: model?.model }) : t('review.model_required')}</span><Button size="xs" variant="ghost" disabled={busy || suggesting} onClick={() => setSettings(true)}><Settings2 className="size-3" />{t('review.model_settings')}</Button></div>
      {session.running && <div role="status" className="flex items-center gap-2 rounded-lg bg-muted p-3 text-sm"><Loader2 className="size-4 shrink-0 animate-spin" />{t(`review.phases.${session.phase}`)}<span className="ml-auto text-xs text-muted-foreground">{t('review.continues_hint')}</span></div>}
      {(error || session.error) && <p role="alert" className="text-sm text-danger">{error || reviewError(new Error(session.error), t)}</p>}
      {report && !session.running && <div className="space-y-4 border-t border-border pt-4">
        <div className="flex flex-wrap items-center justify-between gap-2"><h4 className="font-medium">{t(report.question ? 'review.followup_result' : 'review.last_result')}</h4>{report.status === 'completed' && <Button size="sm" variant="outline" disabled={busy} onClick={() => setSavingTemplate(report.config)}>{t('review.save_as_template')}</Button>}</div>
        {report.question && <p className="whitespace-pre-wrap text-sm text-muted-foreground">{report.question}</p>}
        {changed && <p className="text-xs text-muted-foreground">{t('review.changed_hint')}</p>}
        <ReviewReportView key={report.id} report={report} />
        {report.status === 'completed' && <div className="flex flex-col gap-2 border-t border-border pt-4 sm:flex-row">
          <Textarea aria-label={t('review.followup')} placeholder={t('review.followup_placeholder')} value={question} rows={2} maxLength={2600} onChange={e => setQuestion(e.target.value)} />
          <Button className="self-end" disabled={busy || suggesting || !question.trim() || !modelReady} onClick={() => {
            setChosen(null); setError(''); const text = question.trim(); setQuestion('')
            void startReview(experiment.id, { parent_id: report.id, question: text, language: i18n.resolvedLanguage ?? i18n.language })
          }}>{t('review.followup')}</Button>
        </div>}
      </div>}
      {history.length > 0 && <details className="text-xs"><summary className="flex cursor-pointer items-center gap-1 text-muted-foreground"><History className="size-3" />{t('review.history', { count: historyTotal })}</summary>
        <div className="mt-2 space-y-1">{history.map(item => <button className="flex w-full items-center justify-between gap-3 rounded p-2 text-left hover:bg-accent disabled:opacity-50" key={item.id} disabled={busy || !!loadingReport} onClick={async () => {
          setLoadingReport(item.id)
          try { setChosen(await fetchReport(experiment.id, item.id)) } catch (e) { setError(reviewError(e, t)) } finally { setLoadingReport('') }
        }}><span className="truncate">{new Date(item.created_at).toLocaleString()} · {item.question || t('review.last_result')}</span><span className="shrink-0">{loadingReport === item.id ? t('common.loading') : t(`review.status.${item.status}`)}</span></button>)}</div>
        {historyTotal > 10 && <div className="mt-2 flex gap-2"><Button size="xs" variant="outline" disabled={busy || !historyOffset} onClick={() => setHistoryOffset(Math.max(0, historyOffset - 10))}>{t('correlation.previous')}</Button><Button size="xs" variant="outline" disabled={busy || historyOffset + 10 >= historyTotal} onClick={() => setHistoryOffset(historyOffset + 10)}>{t('correlation.next')}</Button></div>}
      </details>}
    </section>
    {settings && <Suspense fallback={<p>{t('common.loading')}</p>}><ModelSettings value={model} onClose={() => setSettings(false)} onSaved={value => { setModel(value); setSettings(false) }} /></Suspense>}
    {savingTemplate && <SaveTemplateDialog config={savingTemplate} onClose={() => setSavingTemplate(null)} />}
    <Dialog open={templates} onOpenChange={setTemplates}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t('review.choose_template')}</DialogTitle><DialogDescription>{t('review.change_template_hint')}</DialogDescription></DialogHeader><ReviewTemplates onChoose={value => { setDraft(structuredClone(value.config)); setTemplates(false); setNotice('') }} /></DialogContent></Dialog>
  </div>
}
