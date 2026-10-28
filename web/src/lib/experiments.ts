import { apiRequest, jsonInit } from './api'
import type { ReviewConfig, ReviewSummary } from './review'

export interface TraceTaskMetadata {
  origin?: TraceOrigin
  trace_id: string; name: string; notes: string; labels: string[]; pinned: boolean; top_at?: number; archived: boolean; status: string; revision: number
}
export interface TraceOrigin { kind: 'manual_split'; source_trace_ids: string[]; request_count: number; created_at: number }
export interface Criterion { id: string; name: string; weight: number }
export interface ExperimentTask { prompt: string; repository: string; base_commit: string; environment: string; acceptance: string; test_command: string; rubric: Criterion[] }
export interface Experiment { review?: ReviewConfig; review_summary?: ReviewSummary; id: string; name: string; notes: string; task: ExperimentTask; keep: boolean; archived: boolean; revision: number; created_at: string; updated_at: string; run_count: number }
export interface RunConditions { agent: string; agent_version: string; model: string; configuration: string; environment: string; base_commit: string; seed: string }
export interface RunAssessment {
  verdict: string; scores: Record<string, number>; tests?: { passed: number; failed: number; skipped: number }; changes?: { files: number; added: number; deleted: number }; interventions?: number; result_commit: string; artifacts: string; verification: string; notes: string; diff: string
}
export interface ExperimentRun { review_notes?: string; id: string; experiment_id: string; name: string; conditions: RunConditions; task: ExperimentTask; task_hash: string; state: string; capture_run_id: string; trace_ids: string[]; assessment: RunAssessment; revision: number; created_at: string; updated_at: string; started_at?: string; finished_at?: string }
export interface RunMetrics {
  requests: number; errors: number; incomplete: number; span_ms: number | null; input_tokens: number | null; output_tokens: number | null; total_tokens: number | null; usage_known: number; trace_ids: string[]; log_ids: string[]; sampled: boolean
}
export interface RunResult { run: ExperimentRun; metrics: RunMetrics; warnings: string[] }
export interface ExperimentComparison { experiment: Experiment; results: RunResult[]; warnings: string[] }

const base = '/api/experiments'
function editable(value: object): Record<string, unknown> {
  const payload = { ...value } as Record<string, unknown>
  for (const key of ['created_at', 'updated_at', 'started_at', 'finished_at', 'run_count', 'review_summary']) delete payload[key]
  return payload
}
async function request<T>(url: string, init: RequestInit = {}): Promise<T> { const response = await apiRequest(url, init, 'experiments.failed'); return response.json() }
export const fetchExperimentList = (q = '', archived = false, offset = 0) => request<{ experiments: Experiment[]; total: number }>(`${base}?${new URLSearchParams({ q, archived: String(archived), offset: String(offset), limit: '20' })}`)
export const fetchExperiment = (id: string, offset = 0) => request<{ experiment: Experiment; runs: ExperimentRun[]; total: number }>(`${base}/${encodeURIComponent(id)}?offset=${offset}&limit=20`)
export const saveExperiment = (e: Experiment) => request<Experiment>(e.id ? `${base}/${encodeURIComponent(e.id)}` : base, jsonInit(e.id ? 'PUT' : 'POST', editable(e)))
export const saveExperimentRun = (r: ExperimentRun) => { const payload = editable(r); delete payload.task; delete payload.task_hash; return request<ExperimentRun>(`${base}/${encodeURIComponent(r.experiment_id)}/runs${r.id ? '/' + encodeURIComponent(r.id) : ''}`, jsonInit(r.id ? 'PUT' : 'POST', payload)) }
export const startExperimentRun = (r: ExperimentRun, upstream: string) => request<ExperimentRun>(`${base}/${encodeURIComponent(r.experiment_id)}/runs/${r.id}/start`, jsonInit('POST', { revision: r.revision, upstream }))
export const stopExperimentRun = (r: ExperimentRun) => request<ExperimentRun>(`${base}/${encodeURIComponent(r.experiment_id)}/runs/${r.id}/stop`, jsonInit('POST', { revision: r.revision }))
const comparisonURL = (id: string, ids: string[]) => `${base}/${encodeURIComponent(id)}/compare?runs=${ids.map(encodeURIComponent).join(',')}`
export const fetchExperimentComparison = (id: string, ids: string[]) => request<ExperimentComparison>(comparisonURL(id, ids))
export const fetchTraceTaskMetadata = (id: string) => request<TraceTaskMetadata>(`/api/traces/${encodeURIComponent(id)}/metadata`)
export const saveTraceTaskMetadata = (m: TraceTaskMetadata) => request<TraceTaskMetadata>(`/api/traces/${encodeURIComponent(m.trace_id)}/metadata`, jsonInit('PUT', { ...m, origin: undefined }))
export const emptyTraceMetadata = (id: string): TraceTaskMetadata => ({ trace_id: id, name: '', notes: '', labels: [], pinned: false, archived: false, status: 'none', revision: 0 })
export const emptyExperiment = (): Experiment => ({ id: '', name: '', notes: '', keep: true, archived: false, revision: 0, created_at: '', updated_at: '', run_count: 0, task: { prompt: '', repository: '', base_commit: '', environment: '', acceptance: '', test_command: '', rubric: [] } })
export const emptyRun = (experiment: Experiment): ExperimentRun => ({ id: '', experiment_id: experiment.id, name: '', conditions: { agent: '', agent_version: '', model: '', configuration: '', environment: '', base_commit: '', seed: '' }, task: experiment.task, task_hash: '', state: 'planned', capture_run_id: '', trace_ids: [], assessment: { verdict: 'unknown', scores: {}, result_commit: '', artifacts: '', verification: '', notes: '', diff: '' }, revision: 0, created_at: '', updated_at: '' })
