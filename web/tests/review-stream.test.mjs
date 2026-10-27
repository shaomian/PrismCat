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
