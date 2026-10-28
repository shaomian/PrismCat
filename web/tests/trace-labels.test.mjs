import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { createServer } from 'vite'

let server, collectTraceLabels, maxTraceLabels, TraceLabels, i18n
before(async () => {
  i18n = createInstance()
  await i18n.init({ lng: 'zh', resources: { zh: { translation: JSON.parse(readFileSync(new URL('../src/locales/zh.json', import.meta.url), 'utf8')) } } })
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  ;({ collectTraceLabels, maxTraceLabels } = await server.ssrLoadModule('/src/lib/trace-labels.ts'))
  ;({ TraceLabels } = await server.ssrLoadModule('/src/components/TraceLabels.tsx'))
})
after(async () => { await server?.close() })
const render = props => renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(TraceLabels, props)))

test('label entry accepts Chinese/English separators and saves the unfinished final label without duplicates', () => {
  const original = ['代码修复', 'literal,comma']
  assert.deepEqual(collectTraceLabels(original, '代码修复， 回归测试,CTF\n性能验证 '), ['代码修复', 'literal,comma', '回归测试', 'CTF', '性能验证'])
  assert.deepEqual(collectTraceLabels(original, ' 尚未按回车 '), [...original, '尚未按回车'])
  assert.deepEqual(collectTraceLabels(['A'], 'A，a,, '), ['A', 'a'])
  assert.deepEqual(original, ['代码修复', 'literal,comma'])
})

test('exceeding the existing label limit remains detectable instead of silently discarding input', () => {
  const labels = Array.from({ length: maxTraceLabels }, (_, i) => `label-${i}`)
  assert.equal(collectTraceLabels(labels, 'new-label').length, 21)
  assert.equal(collectTraceLabels(labels, 'label-0').length, 20)
})

test('compact labels expose only two chips and a more-labels control, with an active exact filter', () => {
  const html = render({ labels: ['代码修复', '回归测试', '性能验证', '接口兼容'], limit: 2, activeLabel: '代码修复', onSelect() {} })
  assert.ok(html.includes('>代码修复<'))
  assert.ok(html.includes('>回归测试<'))
  assert.ok(!html.includes('>性能验证<'))
  assert.ok(html.includes('查看其余 2 个标签'))
  assert.ok(html.includes('aria-pressed="true"'))
  assert.equal((html.match(/<button/g) ?? []).length, 3)
  const activeHidden = render({ labels: ['代码修复', '回归测试', '性能验证'], limit: 2, activeLabel: '性能验证', onSelect() {} })
  assert.ok(activeHidden.includes('>性能验证<'))
  assert.ok(activeHidden.includes('清除标签筛选「性能验证」'))
  const full = render({ labels: ['代码修复', '回归测试', '性能验证'] })
  assert.ok(full.includes('>性能验证<'))
  assert.ok(!full.includes('<button'))
})
