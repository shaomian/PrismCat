import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { createServer } from 'vite'

let server, Timeline, i18n
before(async () => {
  i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en: { translation: {} } } })
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  ;({ TraceTimelineView: Timeline } = await server.ssrLoadModule('/src/components/TraceTimeline.tsx'))
})
after(async () => { await server?.close() })

const event = content => ({ kind: 'message', role: 'user', content })
const request = (id, events = [event(`input-${id}`)]) => ({
  log_id: `${id}`, seq: id + 1, upstream: 'test', status_code: 200, latency_ms: 100,
  created_at: 0, new_events: events, response_events: [event(`response-${id}`)],
})
const render = requests => renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(Timeline, { requests })))

test('long traces show an initial batch and keep the remaining requests available on demand', () => {
  const html = render(Array.from({ length: 1000 }, (_, id) => request(id)))
  assert.ok(html.includes('input-0<'))
  assert.ok(html.includes('response-0<'))
  assert.ok(!html.includes('input-999<'))
  assert.ok(html.includes('timeline.show_more_requests'))
  assert.ok(html.length < 150_000, `initial DOM grew to ${html.length} characters`)
})

test('one request with a large imported history also has a bounded initial render', () => {
  const html = render([request(0, Array.from({ length: 1000 }, (_, id) => event(`history-${id}`)))])
  assert.ok(html.includes('history-0<'))
  assert.ok(!html.includes('history-999<'))
  assert.ok(html.includes('timeline.show_more_events'))
  assert.ok(html.length < 60_000, `one request rendered ${html.length} characters`)
})

test('small conversations still show both input and response without an extra step', () => {
  const html = render([request(5)])
  assert.ok(html.includes('input-5<'))
  assert.ok(html.includes('response-5<'))
  assert.ok(!html.includes('timeline.show_more_'))
})
