import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { createServer } from 'vite'

let server, TraceSourceBadges, i18n
before(async () => {
  i18n = createInstance()
  await i18n.init({ lng: 'zh', resources: { zh: { translation: JSON.parse(readFileSync(new URL('../src/locales/zh.json', import.meta.url), 'utf8')) } } })
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  ;({ TraceSourceBadges } = await server.ssrLoadModule('/src/components/TraceOriginBadge.tsx'))
})
after(async () => { await server?.close() })
const render = props => renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(TraceSourceBadges, props)))

test('recorded splits show one system badge instead of repeated assignment-source badges', () => {
  const props = { sources: ['explicit', 'manual', 'auto'], origin: { kind: 'manual_split', source_trace_ids: ['source'], request_count: 2, created_at: 1793100000000 } }
  const html = render(props)
  assert.ok(html.includes('手动拆分'))
  assert.ok(html.includes('aria-expanded="false"'))
  assert.equal((html.match(/<button/g) ?? []).length, 1)
  for (const redundant of ['显式标识', '人工指定', '自动推断']) assert.ok(!html.includes(redundant), redundant)
  assert.deepEqual(props.sources, ['explicit', 'manual', 'auto'])
})

test('ordinary traces retain their assignment badges and unknown provenance does not claim a split', () => {
  for (const origin of [undefined, { kind: 'future-origin' }]) {
    const html = render({ sources: ['explicit', 'manual'], origin })
    assert.ok(html.includes('显式标识'))
    assert.ok(html.includes('人工指定'))
    assert.ok(!html.includes('手动拆分'))
  }
})
