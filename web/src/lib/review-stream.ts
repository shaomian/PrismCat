import { apiRequest, jsonInit } from './api'
import { deleteReport, reviewBase, type ReviewReport } from './review'

/** 一次工具查证的执行明细,供"分析过程"展开视图使用 */
export interface ReviewToolCall {
    name: string
    args: string
    chars?: number
    preview?: string
    error?: string
    remaining?: number
    at: number
}
interface Session { running: boolean; phase: string; report: ReviewReport | null; reportId: string; error: string; version: number; calls: ReviewToolCall[] }
const sessions = new Map<string, Session>()
const listeners = new Map<string, Set<() => void>>()
const controllers = new Map<string, AbortController>()
export function getReviewSession(id: string): Session {
  let value = sessions.get(id)
  if (!value) {
    value = { running: false, phase: 'preparing', report: null, reportId: '', error: '', version: 0, calls: [] }
    sessions.set(id, value)
    // Keep a small cache; reports themselves are persisted on the server.
    if (sessions.size > 8) for (const [key, item] of sessions) {
      if (key !== id && !item.running && !listeners.get(key)?.size) { sessions.delete(key); listeners.delete(key); break }
    }
  }
  return value
}
export function subscribeReview(id: string, callback: () => void) {
  const values = listeners.get(id) ?? new Set<() => void>()
  values.add(callback); listeners.set(id, values)
  return () => { values.delete(callback) }
}
function update(id: string, value: Partial<Session>) {
  sessions.set(id, { ...getReviewSession(id), ...value })
  listeners.get(id)?.forEach(fn => fn())
}
export function stopReview(id: string) { controllers.get(id)?.abort() }
export async function deleteReviewReport(id: string, reportId: string) {
  await deleteReport(id, reportId)
  const session = getReviewSession(id)
  // A new analysis may have started while DELETE was in flight.
  if (session.running || (session.report?.id ?? session.reportId) !== reportId) return
  update(id, { report: null, reportId: '', calls: [], error: '', phase: 'preparing', version: session.version + 1 })
}
export async function startReview(id: string, payload: object) {
  if (getReviewSession(id).running) return
  const controller = new AbortController()
  controllers.set(id, controller)
  update(id, { running: true, phase: 'preparing', error: '', report: null, reportId: '', calls: [] })
  let completed = false
  try {
    const response = await apiRequest(`${reviewBase(id)}/analyze`, { ...jsonInit('POST', payload), signal: controller.signal }, 'review.failed')
    if (!response.body) throw new Error('review_analysis_failed')
    const reader = response.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''
    try {
      for (;;) {
        const { done, value } = await reader.read()
        buffer += decoder.decode(value, { stream: !done })
        if (buffer.length > 2 * 1024 * 1024) throw new Error('review_output_too_long')
        let boundary: number
        while ((boundary = buffer.indexOf('\n\n')) >= 0) {
          const block = buffer.slice(0, boundary); buffer = buffer.slice(boundary + 2)
          const raw = block.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trimStart()).join('\n')
          if (!raw) continue
          const event = JSON.parse(raw) as { type: string; phase?: string; report_id?: string; report?: ReviewReport; error?: string; name?: string; args?: string; chars?: number; preview?: string; remaining?: number }
          if (event.type === 'ready') update(id, { reportId: event.report_id ?? '' })
          if (event.type === 'progress' && event.phase !== getReviewSession(id).phase) update(id, { phase: event.phase ?? 'writing' })
          if (event.type === 'tool') {
            const calls = getReviewSession(id).calls
            update(id, { calls: [...calls, { name: event.name ?? '', args: event.args ?? '', at: Date.now() }] })
          }
          if (event.type === 'tool_result') {
            const calls = [...getReviewSession(id).calls]
            for (let i = calls.length - 1; i >= 0; i--) {
              // 工具按调用-返回成对串行执行,回填最近一条未完成的同名调用
              if (calls[i].name === event.name && calls[i].chars === undefined && calls[i].error === undefined) {
                calls[i] = { ...calls[i], chars: event.chars, preview: event.preview, error: event.error, remaining: event.remaining }
                break
              }
            }
            update(id, { calls })
          }
          if (event.type === 'done') { completed = true; update(id, { report: event.report ?? null }) }
          if (event.type === 'error') { update(id, { report: event.report ?? null }); throw new Error(event.error ?? 'review_analysis_failed') }
        }
        if (done) break
      }
    } finally { await reader.cancel().catch(() => {}) }
    if (!completed) throw new Error('review_analysis_failed')
  } catch (error) {
    update(id, { error: controller.signal.aborted ? 'review_canceled' : error instanceof SyntaxError ? 'review_invalid_output' : (error as Error).message })
  } finally {
    controllers.delete(id)
    update(id, { running: false, version: getReviewSession(id).version + 1 })
  }
}
