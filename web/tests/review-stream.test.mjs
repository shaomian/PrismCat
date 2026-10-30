import assert from 'node:assert/strict'
import test from 'node:test'
import { createServer } from 'vite'

async function withReview(run) {
  const originalFetch = globalThis.fetch
  const server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  try { await run(await server.ssrLoadModule('/src/lib/review-stream.ts'), await server.ssrLoadModule('/src/lib/review.ts')) }
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

test('deleting the cached report clears its result and activity and notifies subscribers', async () => {
  await withReview(async ({ startReview, getReviewSession, subscribeReview, deleteReviewReport }) => {
    globalThis.fetch = async (_url, init) => init.method === 'DELETE'
      ? new Response('{"status":"ok"}')
      : response(event({ type: 'ready', report_id: 'report' })
        + event({ type: 'tool', name: 'get_task_requests', args: '{}' })
        + event({ type: 'tool_result', name: 'get_task_requests', chars: 5 })
        + event({ type: 'done', report: { id: 'report', status: 'completed' } }))
    await startReview('delete', {})
    const previousVersion = getReviewSession('delete').version
    let notifications = 0
    const unsubscribe = subscribeReview('delete', () => notifications++)
    await deleteReviewReport('delete', 'report')
    const session = getReviewSession('delete')
    assert.equal(session.report, null)
    assert.equal(session.reportId, '')
    assert.deepEqual(session.calls, [])
    assert.equal(session.error, '')
    assert.equal(session.version, previousVersion + 1)
    assert.equal(notifications, 1)
    unsubscribe()
    // Returning to the card uses the same cache, now without the deleted result.
    assert.equal(getReviewSession('delete').report, null)
  })
})

test('deleting another historical report or failing deletion preserves the current report', async () => {
  await withReview(async ({ startReview, getReviewSession, deleteReviewReport }) => {
    globalThis.fetch = async () => response(event({ type: 'done', report: { id: 'current', status: 'completed' } }))
    await startReview('history', {})
    const current = getReviewSession('history')
    globalThis.fetch = async () => new Response('{"status":"ok"}')
    await deleteReviewReport('history', 'old')
    assert.equal(getReviewSession('history'), current)
    globalThis.fetch = async () => new Response('{"error":"delete failed"}', { status: 500 })
    await assert.rejects(deleteReviewReport('history', 'current'))
    assert.equal(getReviewSession('history'), current)
  })
})

test('a delayed deletion cannot clear a newer completed analysis', async () => {
  await withReview(async ({ startReview, getReviewSession, deleteReviewReport }) => {
    globalThis.fetch = async () => response(event({ type: 'done', report: { id: 'old', status: 'completed' } }))
    await startReview('race', {})
    let finishDelete
    globalThis.fetch = async (_url, init) => init.method === 'DELETE'
      ? new Promise(resolve => { finishDelete = resolve })
      : response(event({ type: 'ready', report_id: 'new' })
        + event({ type: 'tool', name: 'get_request_detail', args: '{}' })
        + event({ type: 'done', report: { id: 'new', status: 'completed' } }))
    const deleting = deleteReviewReport('race', 'old')
    await startReview('race', {})
    const current = getReviewSession('race')
    finishDelete(new Response('{"status":"ok"}'))
    await deleting
    assert.equal(getReviewSession('race'), current)
    assert.equal(current.report.id, 'new')
    assert.equal(current.calls.length, 1)
  })
})

test('deletion leaves an analysis that is still starting untouched', async () => {
  await withReview(async ({ startReview, getReviewSession, deleteReviewReport }) => {
    let finishDelete, startResponse
    globalThis.fetch = async (_url, init) => new Promise(resolve => {
      if (init.method === 'DELETE') finishDelete = resolve
      else startResponse = resolve
    })
    const deleting = deleteReviewReport('starting', 'old')
    const running = startReview('starting', {})
    const current = getReviewSession('starting')
    assert.equal(current.running, true)
    finishDelete(new Response('{"status":"ok"}'))
    await deleting
    assert.equal(getReviewSession('starting'), current)
    startResponse(response(event({ type: 'done', report: { id: 'new', status: 'completed' } })))
    await running
    assert.equal(getReviewSession('starting').report.id, 'new')
  })
})

test('deleting a failed report clears its cached error and tool activity', async () => {
  await withReview(async ({ startReview, getReviewSession, deleteReviewReport }) => {
    globalThis.fetch = async (_url, init) => init.method === 'DELETE'
      ? new Response('{"status":"ok"}')
      : response(event({ type: 'ready', report_id: 'failed' })
        + event({ type: 'tool', name: 'get_task_requests', args: '{}' })
        + event({ type: 'error', error: 'review_analysis_failed' }))
    await startReview('failed-card', {})
    assert.ok(getReviewSession('failed-card').error)
    await deleteReviewReport('failed-card', 'failed')
    assert.equal(getReviewSession('failed-card').error, '')
    assert.equal(getReviewSession('failed-card').reportId, '')
    assert.deepEqual(getReviewSession('failed-card').calls, [])
  })
})

test('history offset returns to an existing page after deleting its last report', async () => {
  await withReview(async (_stream, { reviewHistoryOffset }) => {
    assert.equal(reviewHistoryOffset(10, 11), 10)
    assert.equal(reviewHistoryOffset(10, 10), 0)
    assert.equal(reviewHistoryOffset(20, 20), 10)
    assert.equal(reviewHistoryOffset(10, 1), 0)
    assert.equal(reviewHistoryOffset(0, 0), 0)
    assert.equal(reviewHistoryOffset(10, 0), 0)
    assert.equal(reviewHistoryOffset(10, 19), 10)
  })
})
