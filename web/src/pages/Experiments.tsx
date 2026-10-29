import { useListMemory } from '@/hooks/useListMemory'
import { useCallback, useEffect, useState, useRef, useSyncExternalStore, lazy, Suspense, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { ArrowLeft, Code2, FileText, Plus, RefreshCw, ScanSearch, Shield } from 'lucide-react'
import { ApiError, fetchCaptureRuns, fetchUpstreams, type CaptureRun, type Upstream } from '@/lib/api'
import {
  emptyExperiment, emptyRun, fetchExperiment, fetchExperimentComparison,
  fetchExperimentList, saveExperiment, saveExperimentRun, startExperimentRun,
  stopExperimentRun, fetchTraceTaskMetadata, type Experiment, type ExperimentComparison,
  type ExperimentRun, type RunResult,
} from '@/lib/experiments'
import { copyText } from '@/lib/clipboard'
import { cn, formatLatency, PANEL_CLASS } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/EmptyState'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { TraceSearch } from '@/components/TraceTaskManager'
import { CopyTraceIdButton } from '@/components/CopyTraceIdButton'
import { TaskActions, TaskBatchActions, TaskSelectionToggle } from '@/components/TaskManagement'
import { TaskActionBar, TaskIconButton, TaskPreviewTrigger } from '@/components/TaskHover'
import { getReviewSession, subscribeReview } from '@/lib/review-stream'
import type { ReviewTemplate } from '@/lib/review'
import { experimentPreviewContent, fetchTaskPreview, type ExperimentPreview } from '@/lib/task-preview'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
const ReviewPanel = lazy(() => import('@/components/review/ReviewPanel'))
const ReviewTemplates = lazy(() => import('@/components/review/ReviewTemplates'))

const panel = cn(PANEL_CLASS, 'space-y-3 p-4')
const selectClass = 'h-8 rounded-md border border-input bg-background px-2 text-sm'

function runLabel(run: ExperimentRun) {
  return `${run.conditions.agent} / ${run.conditions.model || '—'}`
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="block space-y-1 text-xs text-muted-foreground"><span>{label}</span>{children}</label>
}

function ErrorMessage({ error }: { error: string }) {
  return error ? <p role="alert" className="text-xs text-danger">{error}</p> : null
}

function useReviewRunning(id: string) {
  const subscribe = useCallback((callback: () => void) => subscribeReview(id, callback), [id])
  const snapshot = useCallback(() => getReviewSession(id).running, [id])
  return useSyncExternalStore(subscribe, snapshot)
}

function ExperimentCardActions({ item, archived, disabled, onChanged }: { item: Experiment; archived: boolean; disabled: boolean; onChanged: () => void }) {
  const { t } = useTranslation()
  const running = useReviewRunning(item.id)
  const serverRunning = item.review_summary?.status === 'running' && getReviewSession(item.id).version === 0
  const [editing, setEditing] = useState<Experiment | null>(null)
  const [loading, setLoading] = useState(false)
  const edit = async () => {
    setLoading(true)
    try { setEditing((await fetchExperiment(item.id)).experiment) }
    catch (error) { toast.error((error as Error).message) }
    finally { setLoading(false) }
  }
  return <>
    <TaskActionBar><TaskActions kind="experiment" items={[{ id: item.id, revision: item.revision }]} name={item.name} archived={archived} disabled={disabled || loading || running || serverRunning} onChanged={onChanged} onEdit={() => void edit()} editLabel={t('experiments.edit')} inline /></TaskActionBar>
    {editing && <ExperimentEditor experiment={editing} onCancel={() => setEditing(null)} onSaved={() => { setEditing(null); onChanged() }} />}
  </>
}

export function Experiments() {
  const { experimentId } = useParams()
  return experimentId ? <ExperimentDetail key={experimentId} id={experimentId} /> : <ExperimentList />
}

function ExperimentList() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const [page, setPage] = useState<{ key: string; items: Experiment[]; total: number } | null>(null)
  const [query, setQuery] = useListMemory('reviews.query', '')
  const [draftQuery, setDraftQuery] = useListMemory('reviews.draftQuery', '')
  const [archived, setArchived] = useListMemory('reviews.archived', false)
  const [offset, setOffset] = useListMemory('reviews.offset', 0)
  const [picker, setPicker] = useState(false)
  const [view, setView] = useListMemory('reviews.view', 'cards')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  const [selection, setSelection] = useState<{ key: string; ids: string[]; active: boolean }>({ key: '', ids: [], active: false })
  const pageKey = JSON.stringify([query, archived, offset, refresh])
  const selecting = selection.key === pageKey && selection.active
  const selected = selecting ? selection.ids : []
  const items = page?.key === pageKey ? page.items : []
  const total = page?.total ?? 0
  const loading = page?.key !== pageKey && !error
  const selectedItems = items.filter(item => selected.includes(item.id)).map(item => ({ id: item.id, revision: item.revision }))
  const exitSelection = () => setSelection({ key: '', ids: [], active: false })
  const toggleSelected = (id: string, checked: boolean) => setSelection({ key: pageKey, active: true, ids: checked ? [...selected.filter(value => value !== id), id] : selected.filter(value => value !== id) })
  const changed = () => { exitSelection(); setRefresh(value => value + 1) }
  useEffect(() => {
    if (draftQuery === query) return
    const timer = window.setTimeout(() => { setQuery(draftQuery); setOffset(0) }, 250)
    return () => window.clearTimeout(timer)
  }, [draftQuery, query, setQuery, setOffset])
  useEffect(() => {
    let cancelled = false
    void fetchExperimentList(query, archived, offset).then(data => {
      if (!cancelled) {
        if (offset > 0 && offset >= data.total) { setOffset(Math.max(0, Math.ceil(data.total / 20) - 1) * 20); return }
        setPage({ key: pageKey, items: data.experiments, total: data.total }); setError('')
      }
    }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [query, archived, offset, pageKey, setOffset])
  const create = async (template: ReviewTemplate) => {
    if (busy) return
    setBusy(true)
    try {
      const value = emptyExperiment(); value.name = `${template.name} · ${new Date().toLocaleString(i18n.resolvedLanguage, { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })}`; value.review = structuredClone(template.config)
      const saved = await saveExperiment(value)
      navigate(`/experiments/${saved.id}`)
    } catch (reason) { setError((reason as Error).message) }
    finally { setBusy(false) }
  }
  return <div className="space-y-4">
    <div className="flex flex-wrap items-center justify-between gap-3">
      <p className="text-sm text-muted-foreground">{t('review.intro')}</p>
      <Button onClick={() => setPicker(true)}><Plus className="size-4" />{t('review.create_card')}</Button>
    </div>
    <Tabs value={view} onValueChange={value => { exitSelection(); setView(value) }}>
      <TabsList variant="line" aria-label={t('review.workspace')} className="w-full justify-start rounded-none border-b border-border pb-1">
        {['cards', 'templates'].map(key => <TabsTrigger key={key} value={key} className="flex-none px-3">{t(`review.tabs.${key}`)}</TabsTrigger>)}
      </TabsList>
    </Tabs>
    <ErrorMessage error={error} />
    {view === 'cards' ? <>
      <div className="flex flex-wrap items-center gap-3">
        <Input className="max-w-md" placeholder={t('review.search_cards')} value={draftQuery} onChange={e => { setDraftQuery(e.target.value); exitSelection() }} />
        <select aria-label={t('task_management.archive_filter')} className={selectClass} value={String(archived)} onChange={event => { setArchived(event.target.value === 'true'); setOffset(0); exitSelection() }}>
          <option value="false">{t('task_management.unarchived')}</option><option value="true">{t('task_management.archived')}</option>
        </select>
        {(items.length > 0 || selecting) && <TaskSelectionToggle active={selecting} disabled={loading || busy} onToggle={() => selecting ? exitSelection() : setSelection({ key: pageKey, ids: [], active: true })} />}
      </div>
      {selecting && items.length > 0 && <div className="flex flex-wrap items-center justify-between gap-3">
        <label className="flex items-center gap-2 text-xs text-muted-foreground"><input type="checkbox" checked={items.every(item => selected.includes(item.id))} onChange={event => setSelection({ key: pageKey, active: true, ids: event.target.checked ? items.map(item => item.id) : [] })} />{t('task_management.select_page')}</label>
        {selectedItems.length > 0 && <TaskBatchActions kind="experiment" items={selectedItems} archived={archived} onChanged={changed} />}
      </div>}
      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{items.map(item => {
        const Icon = item.review_summary?.template_id === 'builtin:pentest' ? Shield : item.review_summary?.template_id === 'builtin:coding' ? Code2 : item.review_summary?.template_id === 'builtin:reverse' ? ScanSearch : FileText
        const content = <>
          <h3 data-task-preview-title className="break-words font-medium transition-colors group-hover/review:text-foreground group-focus-visible/review:text-primary">{item.name}</h3>
          <p className="flex items-center gap-1.5 text-xs text-muted-foreground"><Icon className="size-3.5 shrink-0" />{item.review_summary?.template_name || t('review.basic_card')}</p>
          <div className="flex flex-wrap gap-3 text-xs text-muted-foreground"><span>{t('review.task_count', { count: item.run_count })}</span>{!!item.review_summary?.point_count && <span>{t('review.point_count', { count: item.review_summary.point_count })}</span>}{item.review_summary?.status && <span>{t(`review.status.${item.review_summary.status}`)}</span>}</div>
        </>
        const contentClass = 'group/review block min-w-0 flex-1 space-y-3 rounded-sm text-left focus-visible:outline-2 focus-visible:outline-primary'
        return <article className={cn('task-item flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-4 transition-colors hover:border-border', selecting && selected.includes(item.id) && 'border-primary/50 bg-primary/5')} key={item.id}>
          <div className="flex min-w-0 items-start gap-3">
            {selecting && <input className="mt-1" type="checkbox" aria-label={t('task_management.select_item', { name: item.name })} checked={selected.includes(item.id)} onChange={event => toggleSelected(item.id, event.target.checked)} />}
            {selecting ? <button type="button" className={contentClass} aria-pressed={selected.includes(item.id)} onClick={() => toggleSelected(item.id, !selected.includes(item.id))}>{content}</button> : <TaskPreviewTrigger name={item.name} titleOnly
              previewKey={JSON.stringify(['experiment', item.id, item.revision, item.updated_at, item.run_count, item.review_summary?.status, item.review_summary?.point_count, i18n.language])}
              loadPreview={async signal => experimentPreviewContent(item.name, await fetchTaskPreview<ExperimentPreview>('experiment', item.id, signal), t, i18n.language)}
            ><Link className={contentClass} to={`/experiments/${item.id}`}>{content}</Link></TaskPreviewTrigger>}
          </div>
          <div className="mt-auto flex min-h-8 flex-wrap items-center justify-between gap-x-2 gap-y-1">
            <time className="text-xs text-muted-foreground" dateTime={item.updated_at}>{new Date(item.updated_at).toLocaleString()}</time>
            {!selecting && <ExperimentCardActions item={item} archived={archived} disabled={busy} onChanged={changed} />}
          </div>
        </article>
      })}</div>
      {!items.length && <EmptyState title={t(loading ? 'common.loading' : archived ? 'task_management.empty_archived' : 'review.empty_cards')} action={!loading && !archived ? <Button variant="outline" onClick={() => setPicker(true)}>{t('review.create_first')}</Button> : undefined} className="rounded-lg border border-dashed border-border p-8" />}
      <Pagination offset={offset} total={total} disabled={loading} onChange={value => { setOffset(value); exitSelection() }} />
    </> : <Suspense fallback={<p>{t('common.loading')}</p>}><ReviewTemplates manage disabled={busy} onChoose={template => void create(template)} /></Suspense>}
    <Dialog open={picker} onOpenChange={open => { if (!busy) setPicker(open) }}><DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t('review.choose_template')}</DialogTitle><DialogDescription>{t('review.choose_template_hint')}</DialogDescription></DialogHeader><Suspense fallback={<p>{t('common.loading')}</p>}><ReviewTemplates disabled={busy} onChoose={template => void create(template)} /></Suspense></DialogContent></Dialog>
  </div>
}

function Pagination({ offset, total, onChange, disabled = false }: { offset: number; total: number; onChange: (n: number) => void; disabled?: boolean }) {
  const { t } = useTranslation()
  return <div className="flex items-center gap-3 text-xs text-muted-foreground">
    <Button size="sm" variant="outline" disabled={disabled || offset === 0} onClick={() => onChange(Math.max(0, offset - 20))}>{t('correlation.previous')}</Button>
    <span>{Math.floor(offset / 20) + 1} / {Math.max(1, Math.ceil(total / 20))} · {total}</span>
    <Button size="sm" variant="outline" disabled={disabled || offset + 20 >= total} onClick={() => onChange(offset + 20)}>{t('correlation.next')}</Button>
  </div>
}

function ExperimentEditor({ experiment, onCancel, onSaved }: { experiment: Experiment; onCancel: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => structuredClone(experiment))
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [conflict, setConflict] = useState(false)
  const save = async () => {
    setBusy(true)
    try { await saveExperiment(draft); onSaved() }
    catch (reason) { const changed = reason instanceof ApiError && reason.status === 409; setConflict(changed); setError(changed ? t('trace_manager.conflict') : (reason as Error).message) }
    finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={open => { if (!open && !busy) onCancel() }}><DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-lg" onInteractOutside={event => event.preventDefault()} onEscapeKeyDown={event => { if (busy) event.preventDefault() }}>
    <DialogHeader><DialogTitle>{t(experiment.id ? 'experiments.edit' : 'experiments.create')}</DialogTitle><DialogDescription>{t('experiments.edit_hint')}</DialogDescription></DialogHeader>
    <Field label={t('experiments.name')}><Input autoFocus disabled={busy} value={draft.name} maxLength={200} onChange={e => setDraft(p => ({ ...p, name: e.target.value }))} /></Field>
    <Field label={t('experiments.prompt')}><Textarea disabled={busy} value={draft.task.prompt} rows={3} maxLength={65536} onChange={e => setDraft(p => ({ ...p, task: { ...p.task, prompt: e.target.value } }))} /></Field>
    <details className="text-xs text-muted-foreground"><summary className="cursor-pointer">{t('task_management.more_settings')}</summary><div className="mt-3 flex flex-wrap gap-5 text-xs">
      <label className="flex items-center gap-2"><input type="checkbox" disabled={busy} checked={draft.keep} onChange={e => setDraft(p => ({ ...p, keep: e.target.checked }))} />{t('experiments.keep')}</label>
    </div></details>
    <ErrorMessage error={error} />
    <DialogFooter><Button variant="outline" disabled={busy} onClick={onCancel}>{t('common.cancel')}</Button><Button disabled={busy || conflict || !draft.name.trim()} onClick={() => void save()}>{t('common.save')}</Button></DialogFooter>
  </DialogContent></Dialog>
}

function ExperimentDetail({ id }: { id: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [experiment, setExperiment] = useState<Experiment | null>(null)
  const [runs, setRuns] = useState<ExperimentRun[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [editingTask, setEditingTask] = useState(false)
  const [editingRun, setEditingRun] = useState<ExperimentRun | null>(null)
  const [importExisting, setImportExisting] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const selectionTouched = useRef(false)
  const restoreSelection = useCallback((ids: string[]) => { if (!selectionTouched.current) setSelected(ids.filter(id => runs.some(run => run.id === id)).slice(0, 8)) }, [runs])
  const reviewRunning = useReviewRunning(id)
  const [comparison, setComparison] = useState<ExperimentComparison | null>(null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [upstreams, setUpstreams] = useState<Upstream[]>([])
  const [upstream, setUpstream] = useState('')
  const load = useCallback(async () => {
    const data = await fetchExperiment(id, offset)
    if (offset > 0 && offset >= data.total) { setOffset(Math.max(0, Math.ceil(data.total / 20) - 1) * 20); return }
    setExperiment(data.experiment)
    setRuns(data.runs)
    setTotal(data.total)
    setError('')
  }, [id, offset])
  useEffect(() => {
    let cancelled = false
    void fetchExperiment(id, offset).then(data => {
      if (!cancelled) {
        if (offset > 0 && offset >= data.total) { setOffset(Math.max(0, Math.ceil(data.total / 20) - 1) * 20); return }
        setExperiment(data.experiment); setRuns(data.runs); setTotal(data.total); setError('')
      }
    }).catch(reason => { if (!cancelled) setError((reason as Error).message) })
    return () => { cancelled = true }
  }, [id, offset])
  useEffect(() => {
    let cancelled = false
    void fetchUpstreams().then(items => {
      if (!cancelled) { setUpstreams(items.filter(u => u.logging_enabled)); setUpstream(items.find(u => u.logging_enabled)?.name ?? '') }
    }).catch(() => {})
    return () => { cancelled = true }
  }, [])
  const action = async (callback: () => Promise<unknown>) => {
    setBusy(true)
    setComparison(null)
    try { await callback(); await load() }
    catch (reason) { toast.error((reason as Error).message) }
    finally { setBusy(false) }
  }
  const saved = () => {
    setEditingTask(false)
    setEditingRun(null)
    setComparison(null)
    void load().catch(reason => setError((reason as Error).message))
  }
  if (!experiment) return <div className="space-y-3"><Link to="/experiments">{t('experiments.back')}</Link><ErrorMessage error={error} /><p>{t('common.loading')}</p></div>

  return <div className="space-y-4">
    <Link to="/experiments" className="inline-flex items-center gap-1 text-sm text-muted-foreground"><ArrowLeft className="size-4" />{t('experiments.back')}</Link>
    <div className="task-item flex flex-wrap items-center justify-between gap-2">
      <div className="min-w-0"><h2 className="break-words text-lg font-semibold">{experiment.name}</h2>{experiment.archived && <p className="mt-1 text-xs text-muted-foreground">{t('task_management.archived')}</p>}</div>
      <TaskActionBar>
        <TaskIconButton label={t('experiments.refresh')} className="task-direct-action" disabled={busy} onClick={() => void action(async () => {})}><RefreshCw className="size-4" /></TaskIconButton>
        <TaskActions kind="experiment" items={[{ id: experiment.id, revision: experiment.revision }]} name={experiment.name} archived={experiment.archived} disabled={busy || reviewRunning || !!editingTask || !!editingRun} onEdit={() => setEditingTask(true)} editLabel={t('experiments.edit')} inline onChanged={changedAction => {
          if (changedAction === 'delete') navigate('/experiments')
          else saved()
        }} />
      </TaskActionBar>
    </div>
    <ErrorMessage error={error} />
    {editingTask && <ExperimentEditor key={experiment.revision} experiment={experiment} onCancel={() => setEditingTask(false)} onSaved={saved} />}
    <Suspense fallback={<p className="py-4 text-sm text-muted-foreground">{t('common.loading')}</p>}><ReviewPanel key={experiment.id} experiment={experiment} selected={selected} onSaved={setExperiment} onRestoreSelection={restoreSelection} preparationLocked={!!editingRun} disabled={busy} taskPicker={<section className="space-y-3">
    <div className="flex flex-wrap items-center justify-between gap-2"><h3 className="font-medium">{t('review.step_tasks')}</h3><div className="flex flex-wrap gap-2"><Button size="sm" variant="ghost" disabled={busy || reviewRunning} onClick={() => { setImportExisting(true); setEditingRun(emptyRun(experiment)) }}>{t('review.add_existing')}</Button><Button size="sm" variant="ghost" disabled={busy || reviewRunning} onClick={() => { setImportExisting(false); setEditingRun(emptyRun(experiment)) }}>{t('review.record_task')}</Button></div></div>
    {experiment.task.prompt && <details className="text-xs text-muted-foreground"><summary className="w-fit cursor-pointer">{t('experiments.prompt')}</summary><p className="mt-2 whitespace-pre-wrap break-words leading-relaxed">{experiment.task.prompt}</p></details>}
    {editingRun && <RunEditor key={(editingRun.id || 'new') + String(importExisting)} run={editingRun} importExisting={importExisting} onCancel={() => setEditingRun(null)} onSaved={run => { selectionTouched.current = true; setSelected(previous => previous.includes(run.id) || previous.length >= 8 ? previous : [...previous, run.id]); saved() }} />}
    {(editingRun && !importExisting || runs.some(run => run.state === 'planned' && !run.capture_run_id)) && <div className="flex flex-wrap items-center gap-3">
      <label className="text-xs">{t('experiments.record_upstream')} <select aria-label={t('experiments.record_upstream')} className={selectClass} value={upstream} onChange={e => setUpstream(e.target.value)}>
        <option value="">—</option>{upstreams.map(u => <option key={u.name} value={u.name}>{u.name}</option>)}
      </select></label>
    </div>}
    <p className="text-xs text-muted-foreground">{t('review.tasks_hint')}</p>
    <div className="divide-y divide-border border-y border-border">{runs.map(run => <div key={run.id} className="task-item flex flex-wrap items-center gap-3 py-3 text-xs">
      <TaskPreviewTrigger name={run.name} summary={runLabel(run)} details={t(`experiments.states.${run.state}`)}><label className="flex min-w-0 flex-1 cursor-pointer items-center gap-3 py-1"><input className="accent-primary" aria-label={t('experiments.select_run', { name: run.name })} type="checkbox" checked={selected.includes(run.id)} disabled={busy || reviewRunning || (!selected.includes(run.id) && selected.length >= 8)} onChange={e => {
        selectionTouched.current = true
        setSelected(previous => e.target.checked ? [...previous, run.id] : previous.filter(v => v !== run.id))
        setComparison(null)
      }} />
      <div className="min-w-0 flex-1">
        <p className="break-words font-medium">{run.name}</p>
        <p className="mt-1 break-words text-muted-foreground">{run.name !== runLabel(run) && run.conditions.agent !== t('review.unlabelled_agent') ? `${runLabel(run)} · ` : ''}{t(`experiments.states.${run.state}`)}</p>
      </div>
      </label></TaskPreviewTrigger><TaskActionBar>
      {run.state === 'planned' && !run.capture_run_id && <Button className="task-primary-action" size="xs" disabled={busy || !upstream} onClick={() => void action(() => startExperimentRun(run, upstream))}>{t('experiments.start')}</Button>}
      {run.state === 'running' && run.capture_run_id && <Button className="task-primary-action" size="xs" variant="outline" disabled={busy} onClick={() => void action(() => stopExperimentRun(run))}>{t('experiments.stop')}</Button>}
      <TaskActions kind="run" items={[{ id: run.id, revision: run.revision }]} experimentId={experiment.id} name={run.name} disabled={busy || reviewRunning || !!editingTask || !!editingRun} deleteDisabled={run.state === 'running'} onEdit={() => { setImportExisting(false); setEditingRun(run) }} editLabel={t('experiments.edit_run')} inline onChanged={() => {
        selectionTouched.current = true
        setSelected(previous => previous.filter(id => id !== run.id))
        saved()
      }} /></TaskActionBar>
    </div>)}</div>
    {total > 20 && <Pagination offset={offset} total={total} onChange={setOffset} />}
    <div className="flex flex-wrap items-center gap-2"><Button size="sm" variant="ghost" className="text-muted-foreground" disabled={busy || reviewRunning || selected.length < 2} onClick={() => void action(async () => setComparison(await fetchExperimentComparison(id, selected)))}>{t('review.basic_compare')} ({selected.length}/8)</Button>{selected.length > 0 && <Button variant="ghost" size="sm" className="text-muted-foreground" disabled={busy || reviewRunning} onClick={() => { selectionTouched.current = true; setSelected([]); setComparison(null) }}>{t('experiments.clear_selection')}</Button>}</div>
    {comparison && <ComparisonView comparison={comparison} />}
    </section>} /></Suspense>
  </div>
}

function RunEditor({ run, importExisting, onCancel, onSaved }: { run: ExperimentRun; importExisting: boolean; onCancel: () => void; onSaved: (run: ExperimentRun) => void }) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState(() => structuredClone(run))
  const [captures, setCaptures] = useState<CaptureRun[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let cancelled = false
    void fetchCaptureRuns().then(data => { if (!cancelled) setCaptures(data.runs) }).catch(() => {})
    return () => { cancelled = true }
  }, [])
  const save = async () => {
    setBusy(true)
    try {
      const saved = await saveExperimentRun({
        ...draft,
        conditions: { ...draft.conditions, agent: draft.conditions.agent.trim() || t('review.unlabelled_agent') },
        name: draft.name.trim() || (importExisting ? t('review.imported_task') : runLabel(draft)),
        state: !draft.capture_run_id && draft.trace_ids.length ? 'completed' : draft.state,
      })
      onSaved(saved)
    } catch (reason) { const message = (reason as Error).message; setError(message.includes('capture already belongs') ? t('review.capture_in_card') : message) }
    finally { setBusy(false) }
  }
  const sources = <div className="space-y-3">
    <TraceSearch onSelect={id => {
      setDraft(p => ({ ...p, trace_ids: p.trace_ids.includes(id) ? p.trace_ids : [...p.trace_ids, id] }))
      void fetchTraceTaskMetadata(id).then(metadata => setDraft(p => p.trace_ids.includes(id) ? { ...p, name: p.name || metadata.name || '' } : p)).catch(() => {})
    }} />
    {draft.trace_ids.map(id => <div key={id} className="flex items-center gap-2 text-xs">
      <Link className="truncate font-mono text-primary" to={`/traces/${encodeURIComponent(id)}`}>{id}</Link><CopyTraceIdButton traceId={id} />
      <Button variant="ghost" size="xs" onClick={() => setDraft(p => ({ ...p, trace_ids: p.trace_ids.filter(v => v !== id) }))}>{t('experiments.remove')}</Button>
    </div>)}
    <Field label={t('experiments.existing_capture')}><select className={selectClass} disabled={!!run.capture_run_id} value={draft.capture_run_id} onChange={e => {
      const capture = captures.find(c => c.id === e.target.value)
      setDraft(p => ({ ...p, capture_run_id: e.target.value, name: p.name || capture?.name || '', state: capture ? capture.stopped_at ? 'completed' : 'running' : 'planned' }))
    }}>
      <option value="">—</option>
      {run.capture_run_id && !captures.some(c => c.id === run.capture_run_id) && <option value={run.capture_run_id}>{t('experiments.linked_capture')}</option>}
      {captures.filter(c => c.stopped_at || c.id === run.capture_run_id).map(c => <option key={c.id} value={c.id}>{c.name} · {c.upstream}</option>)}
    </select></Field>
  </div>
  const identity = <>
    <Field label={t(importExisting ? 'review.agent_optional' : 'experiments.agent')}><Input value={draft.conditions.agent} onChange={e => setDraft(p => ({ ...p, conditions: { ...p.conditions, agent: e.target.value } }))} /></Field>
    <Field label={t('experiments.model')}><Input value={draft.conditions.model} onChange={e => setDraft(p => ({ ...p, conditions: { ...p.conditions, model: e.target.value } }))} /></Field>
  </>
  return <section className={panel}>
    <h3 className="font-medium">{t(run.id ? 'experiments.edit_run' : importExisting ? 'review.add_existing' : 'review.record_task')}</h3>
    {importExisting ? <>
      <p className="text-xs leading-relaxed text-muted-foreground">{t('review.import_hint')}</p>
      {sources}
      <Field label={t('experiments.run_name')}><Input value={draft.name} onChange={e => setDraft(p => ({ ...p, name: e.target.value }))} /></Field>
      <details className="space-y-2 text-xs text-muted-foreground"><summary className="cursor-pointer">{t('review.agent_details')}</summary><div className="grid gap-3 sm:grid-cols-2">{identity}</div></details>
    </> : <>
      <div className="grid gap-3 sm:grid-cols-3">{identity}<Field label={t('experiments.run_name')}><Input value={draft.name} onChange={e => setDraft(p => ({ ...p, name: e.target.value }))} /></Field></div>
      <details className="space-y-3 text-xs" open={!!run.capture_run_id || draft.trace_ids.length > 0}><summary className="cursor-pointer text-muted-foreground">{t('experiments.existing_records')}</summary>{sources}</details>
    </>}
    <details className="space-y-2 text-xs text-muted-foreground"><summary className="cursor-pointer">{t('review.task_notes')}</summary><Textarea rows={3} value={draft.review_notes ?? ''} maxLength={10000} placeholder={t('review.task_notes_placeholder')} onChange={e => setDraft(p => ({ ...p, review_notes: e.target.value }))} /></details>
    <ErrorMessage error={error} />
    <div className="flex gap-2"><Button disabled={busy || (!importExisting && !draft.conditions.agent.trim()) || (importExisting && !draft.trace_ids.length && !draft.capture_run_id)} onClick={() => void save()}>{t('common.save')}</Button><Button variant="outline" onClick={onCancel}>{t('common.cancel')}</Button></div>
  </section>
}

function ComparisonView({ comparison: c }: { comparison: ExperimentComparison }) {
  const { t } = useTranslation()
  const n = (v: number | null) => v == null ? '—' : v.toLocaleString()
  const rows: [string, (r: RunResult) => string][] = [
    ['span', r => r.metrics.span_ms == null ? '—' : formatLatency(r.metrics.span_ms)],
    ['total_tokens', r => n(r.metrics.total_tokens)],
    ['input_tokens', r => n(r.metrics.input_tokens)],
    ['output_tokens', r => n(r.metrics.output_tokens)],
    ['requests', r => n(r.metrics.requests)],
    ['errors', r => n(r.metrics.errors)],
  ]
  const report = () => [c.experiment.name, ...c.results.map(r => [
    `${r.run.name} (${r.run.conditions.agent} / ${r.run.conditions.model || '—'})`,
    ...rows.map(([key, value]) => `${t(`experiments.metrics.${key}`)}: ${value(r)}`),
    ...r.warnings.map(w => t(`experiments.warnings.${w}`)),
    ...r.metrics.trace_ids,
  ].join('\n')), ...c.warnings.map(w => t(`experiments.warnings.${w}`))].join('\n\n')

  return <section className="space-y-3 border-t border-border pt-4">
    <div className="flex items-center justify-between gap-3">
      <h3 className="font-semibold">{t('experiments.comparison')}</h3>
      <Button size="sm" variant="outline" onClick={async () => {
        if (await copyText(report())) toast.success(t('log_detail.copy_success'))
        else toast.error(t('log_detail.copy_failed'))
      }}>{t('experiments.copy_report')}</Button>
    </div>
    <p className="text-xs text-muted-foreground">{t('experiments.metrics_hint')}</p>
    {c.warnings.map(w => <p key={w} className="text-xs text-warning">{t(`experiments.warnings.${w}`)}</p>)}
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead><tr className="bg-muted/40">
          <th className="p-3 text-left">{t('experiments.metric')}</th>
          {c.results.map(r => <th key={r.run.id} className="min-w-44 p-3 text-left">
            <p>{r.run.name}</p>{r.run.name !== runLabel(r.run) && <p className="mt-1 text-xs font-normal text-muted-foreground">{runLabel(r.run)}</p>}
          </th>)}
        </tr></thead>
        <tbody>
          {rows.map(([key, value]) => <tr key={key} className="border-t border-border">
            <th className="whitespace-nowrap p-3 text-left font-medium text-muted-foreground">{t(`experiments.metrics.${key}`)}</th>
            {c.results.map(r => <td key={r.run.id} className="p-3">{value(r)}</td>)}
          </tr>)}
          <tr className="border-t border-border">
            <th className="p-3 text-left font-medium text-muted-foreground">{t('experiments.view_traces')}</th>
            {c.results.map(r => <td key={r.run.id} className="p-3 align-top">
              {r.metrics.trace_ids.length ? r.metrics.trace_ids.map((id, i) => <Link key={id} title={id} className="mr-3 inline-block text-primary underline" to={`/traces/${encodeURIComponent(id)}`}>{t('experiments.trace_link', { index: i + 1 })}</Link>) : '—'}
              {r.warnings.map(w => <p key={w} className="mt-2 text-xs text-warning">{t(`experiments.warnings.${w}`)}</p>)}
            </td>)}
          </tr>
        </tbody>
      </table>
    </div>
  </section>
}
