import assert from 'node:assert/strict'
import test from 'node:test'
import { createServer } from 'vite'

async function withReview(run) {
  const originalFetch = globalThis.fetch
  const server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  try { await run(await server.ssrLoadModule('/src/lib/review-stream.ts')) }
  finally { globalThis.fetch = originalFetch; await server.close() }
}
const event = value => `data: ${JSON.stringify(value)}\n\n`
function response(text, chunk = 7) {
  const bytes = new TextEncoder().encode(text)
  return new Response(new ReadableStream({ start(controller) {
    for (let i = 0; i < bytes.length; i += chunk) controller.enqueue(bytes.slice(i, i + chunk))
    controller.close()
  } }), { headers: { 'Content-Type': 'text/event-stream' } })
}
test('review stream handles fragmented Unicode and avoids re-rendering for every progress event', async () => {
  await withReview(async ({ startReview, getReviewSession, subscribeReview }) => {
    const report = { id: 'report', status: 'completed', output: { summary: '复盘完成，证据可查。' } }
    const body = event({ type: 'ready', report_id: 'report' }) + event({ type: 'progress', phase: 'reading' }).repeat(1000) + event({ type: 'progress', phase: 'writing' }).repeat(1000) + event({ type: 'done', report })
    globalThis.fetch = async () => response(body)
    let renders = 0
    const unsubscribe = subscribeReview('card', () => renders++)
    await startReview('card', {})
    const session = getReviewSession('card')
    assert.equal(session.running, false)
    assert.equal(session.error, '')
    assert.equal(session.report.output.summary, report.output.summary)
    assert.ok(renders <= 7, `received ${renders} notifications for 2000 progress events`)
    unsubscribe()
  })
})
test('review stream records tool calls and results for the activity view', async () => {
  await withReview(async ({ startReview, getReviewSession }) => {
    const body = event({ type: 'ready', report_id: 'r' })
      + event({ type: 'progress', phase: 'reading' })
      + event({ type: 'tool', name: 'get_task_requests', args: '{"run_id":"run-a","query":"flag"}' })
      + event({ type: 'tool_result', name: 'get_task_requests', chars: 120, preview: '#1 log_id=flag-log match: …flag captured…', remaining: 23 })
      + event({ type: 'tool', name: 'get_request_detail', args: '{"log_id":"flag-log"}' })
      + event({ type: 'tool_result', name: 'get_request_detail', error: 'log not in selected tasks', remaining: 22 })
      + event({ type: 'progress', phase: 'writing' })
      + event({ type: 'done', report: { id: 'r', status: 'completed' } })
    globalThis.fetch = async () => response(body)
    await startReview('tools', {})
    const session = getReviewSession('tools')
    assert.equal(session.running, false)
    assert.equal(session.calls.length, 2)
    assert.equal(session.calls[0].name, 'get_task_requests')
    assert.equal(session.calls[0].chars, 120)
    assert.equal(session.calls[0].remaining, 23)
    assert.ok(session.calls[0].preview.includes('flag'))
    assert.equal(session.calls[1].error, 'log not in selected tasks')
    assert.equal(session.calls[1].remaining, 22)
    assert.equal(session.phase, 'writing')
  })
})
test('review stream reports truncation and supports cancellation without leaving a busy session', async () => {
  await withReview(async ({ startReview, getReviewSession, stopReview }) => {
    globalThis.fetch = async () => response(event({ type: 'ready', report_id: 'incomplete' }))
    await startReview('truncated', {})
    assert.equal(getReviewSession('truncated').error, 'review_analysis_failed')
    globalThis.fetch = async (_url, init) => new Response(new ReadableStream({ start(controller) {
      const abort = () => controller.error(new DOMException('Aborted', 'AbortError'))
      if (init.signal.aborted) abort()
      else init.signal.addEventListener('abort', abort, { once: true })
    } }))
    const pending = startReview('cancel', {})
    stopReview('cancel')
    await pending
    assert.equal(getReviewSession('cancel').running, false)
    assert.equal(getReviewSession('cancel').error, 'review_canceled')
  })
})
