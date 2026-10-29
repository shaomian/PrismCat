import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createInstance } from 'i18next'
import { createServer } from 'vite'

let server, TaskPreviewLoader, preview, t
before(async () => {
  const i18n = createInstance()
  const zh = JSON.parse(readFileSync(new URL('../src/locales/zh.json', import.meta.url), 'utf8'))
  await i18n.init({ lng: 'zh', resources: { zh: { translation: zh } } })
  t = i18n.t.bind(i18n)
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  ;({ TaskPreviewLoader } = await server.ssrLoadModule('/src/lib/task-preview-loader.ts'))
  preview = await server.ssrLoadModule('/src/lib/task-preview.ts')
})
after(async () => { await server?.close() })

const settle = () => new Promise(resolve => setImmediate(resolve))
const timers = context => context.mock.timers.enable({ apis: ['setTimeout', 'Date'], now: 1000 })

test('brief pointer visits do not load anything; an intentional visit waits 550 ms', async context => {
  timers(context)
  const loader = new TaskPreviewLoader()
  let calls = 0
  const results = []
  const load = () => { calls++; return 'preview' }
  loader.schedule(load, value => results.push(value))
  context.mock.timers.tick(500)
  loader.cancel()
  context.mock.timers.tick(1000)
  await settle()
  assert.equal(calls, 0)
  loader.schedule(load, value => results.push(value))
  context.mock.timers.tick(549)
  await settle()
  assert.equal(calls, 0)
  context.mock.timers.tick(1)
  await settle()
  assert.equal(calls, 1)
  assert.deepEqual(results, ['preview'])
})

test('leaving aborts the request and an obsolete response cannot replace the next preview', async context => {
  timers(context)
  const loader = new TaskPreviewLoader()
  let signal, resolveOld
  const results = []
  loader.schedule(value => { signal = value; return new Promise(resolve => { resolveOld = resolve }) }, value => results.push(value), 'old')
  context.mock.timers.tick(550)
  await settle()
  loader.schedule(() => 'new', value => results.push(value), 'new')
  assert.equal(signal.aborted, true)
  context.mock.timers.tick(550)
  await settle()
  resolveOld('obsolete')
  await settle()
  assert.deepEqual(results, ['new'])
})

test('same item reuses cache, updated keys reload, expiry and route reset refresh data', async context => {
  timers(context)
  const loader = new TaskPreviewLoader()
  let calls = 0
  const show = async key => {
    loader.schedule(() => ++calls, () => {}, key, 1000)
    context.mock.timers.tick(550)
    await settle()
  }
  await show('revision-1')
  await show('revision-1')
  assert.equal(calls, 1)
  await show('revision-2')
  assert.equal(calls, 2)
  context.mock.timers.tick(1001)
  await show('revision-2')
  assert.equal(calls, 3)
  loader.reset()
  await show('revision-2')
  assert.equal(calls, 4)
})

test('empty results are cached and failed requests remain silent and can be retried', async context => {
  timers(context)
  const loader = new TaskPreviewLoader()
  let calls = 0
  const results = []
  const empty = () => { calls++; return null }
  for (let i = 0; i < 2; i++) {
    loader.schedule(empty, value => results.push(value), 'empty')
    context.mock.timers.tick(550)
    await settle()
  }
  assert.equal(calls, 1)
  assert.deepEqual(results, [null, null])
  loader.schedule(() => { throw new Error('failure') }, value => results.push(value), 'failed')
  context.mock.timers.tick(550)
  await settle()
  assert.equal(results.length, 2)
  loader.schedule(() => 'recovered', value => results.push(value), 'failed')
  context.mock.timers.tick(550)
  await settle()
  assert.equal(results.at(-1), 'recovered')
})

test('capture previews add contents and missing usage rather than repeating list metrics', () => {
  const run = { name: 'Capture', upstream: 'route', started_at: '2026-10-29T00:00:00Z', stopped_at: '2026-10-29T00:02:00Z', single_task: false, request_count: 10, wall_time_ms: 9999, usage_missing_count: 3 }
  const content = preview.capturePreviewContent(run, { traces: [{ id: 'a', name: 'Login task' }], trace_count: 4, error_count: 2, latest_error: { status_code: 0, path: '/v1/responses' } }, t, 'zh')
  const text = JSON.stringify(content)
  assert.ok(text.includes('Login task'))
  assert.ok(text.includes('另有 3 条调用链'))
  assert.ok(text.includes('2 条请求错误'))
  assert.ok(text.includes('未返回 HTTP 状态'))
  assert.ok(text.includes('3 条请求缺少完整用量'))
  assert.ok(text.includes('普通录制'))
  assert.ok(!text.includes('9999'))
  assert.ok(!text.includes('route'))
  const fallback = preview.capturePreviewContent({ ...run, usage_missing_count: 0, single_task: true, stopped_at: undefined }, null, t, 'zh')
  assert.ok(JSON.stringify(fallback).includes('正在录制'))
  assert.ok(JSON.stringify(fallback).includes('合成单任务'))
  assert.ok(!fallback.facts.some(fact => fact.label === t('task_preview.errors')))
})

test('review previews show actual tasks and observations, omitting missing model and empty content', () => {
  const content = preview.experimentPreviewContent('Review', { background: 'Task background', tasks: [{ id: 'a', name: 'Run A', agent: 'Agent A', model: '' }], task_count: 3, points: ['Token', 'Duration'], point_count: 4 }, t, 'zh')
  const text = JSON.stringify(content)
  assert.ok(text.includes('Run A · Agent A'))
  assert.ok(text.includes('另有 2 个任务'))
  assert.ok(text.includes('Token、Duration、另有 2 个观测点'))
  assert.ok(!text.includes('—'))
  assert.equal(content.summary, 'Task background')
  assert.equal(preview.experimentPreviewContent('Only a title', { background: '', tasks: [], task_count: 0, points: [], point_count: 0 }, t, 'zh'), null)
  assert.equal(preview.hasPreviewContent({ name: 'Only a title' }), false)
})
