import assert from 'node:assert/strict'
import { before, after, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createInstance } from 'i18next'
import { createServer } from 'vite'

let server, reportTools, mergeSuggestedPoints, t
before(async () => {
  const zh = JSON.parse(readFileSync(new URL('../src/locales/zh.json', import.meta.url), 'utf8'))
  const i18n = createInstance()
  await i18n.init({ lng: 'zh', resources: { zh: { translation: zh } } })
  t = i18n.t.bind(i18n)
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  reportTools = await server.ssrLoadModule('/src/lib/review-report.ts')
  ;({ mergeSuggestedPoints } = await server.ssrLoadModule('/src/lib/review.ts'))
})
after(async () => { await server?.close() })

function report() {
  return {
    id: 'report-old', experiment_id: 'experiment', parent_id: '', question: '', status: 'completed',
    model: 'review-model', created_at: '2026-10-27T10:00:00Z', background: 'Original task background',
    config: { template_id: 'builtin:blank', template_name: 'Snapshot template', instructions: 'Original instructions', points: [{ id: 'tokens', name: 'Token', instructions: 'Recorded values only', metric: 'total_tokens' }], selected: ['tokens'] },
    tasks: [{ run_id: 'run-old', name: 'Original task', agent: 'Original agent', model: 'Original task model', revision: 3, prompt: 'Original prompt', notes: 'Original evidence notes', metrics: { requests: 2, errors: 0, span_ms: 1000, total_tokens: 120, input_tokens: 100, output_tokens: 20, usage_known: 2, incomplete: 0, sampled: false } }],
    output: { summary: 'Original summary', observations: [{ point_id: 'tokens', comparison: 'Original comparison', findings: [{ run_id: 'run-old', value: 'AI incorrectly says 999', explanation: 'Original evidence explanation', source: 'inference', evidence: ['log-old'] }] }], recommendations: ['Original recommendation'] },
    usage: { input_tokens: 1000, output_tokens: 300 },
  }
}

test('Markdown exports the report snapshot, recorded metrics and separate review usage without fetching logs', () => {
  const value = report()
  const draft = structuredClone(value.config)
  draft.points[0].name = 'Unsaved replacement'
  const markdown = reportTools.reviewReportMarkdown(value, 'Review title', t, 'en', 'http://localhost:8711')
  for (const content of ['Original task background', 'Original task model', 'Original prompt', 'Original evidence notes', 'Original summary', 'Original comparison', 'Original recommendation', 'report-old', 'log-old', '| Original task | 120 | 1.00s | 2 | 0 |', '本次 AI 复盘用量', '1,300']) assert.ok(markdown.includes(content), content)
  assert.ok(!markdown.includes('Unsaved replacement'))
  assert.ok(!markdown.includes('AI incorrectly says 999'))
  assert.ok(markdown.includes('/api/experiments/experiment/review/reports/report-old/logs/log-old'))
  assert.equal(value.output.observations[0].findings[0].value, 'AI incorrectly says 999')
})

test('unknown statistical values stay unknown and genuine zero values remain zero in display and export', () => {
  const value = report()
  const finding = value.output.observations[0].findings[0]
  value.tasks[0].metrics.total_tokens = null
  finding.value = '0'
  finding.source = 'statistics'
  assert.deepEqual(reportTools.reviewFindingDisplay(value, finding, 'total_tokens', t, 'en'), { value: '未记录', source: 'unknown' })
  assert.ok(reportTools.reviewReportMarkdown(value, 'Review', t).includes('| Original task | 未记录 | 1.00s | 2 | 0 |'))
  value.tasks[0].metrics.total_tokens = 0
  assert.deepEqual(reportTools.reviewFindingDisplay(value, finding, 'total_tokens', t), { value: '0', source: 'statistics' })
  assert.equal(reportTools.reviewMetricValue(undefined, 'span_ms', t), '未记录')
  assert.equal(reportTools.reviewMetricValue({ span_ms: 0 }, 'span_ms', t), '0ms')
  assert.deepEqual(reportTools.reviewUsage({ usage: { input_tokens: 0, output_tokens: 0 } }), { input: 0, output: 0, total: 0 })
})

test('Markdown safely escapes names, multiline content, HTML and fenced code; evidence URLs cannot escape their path', () => {
  const value = report()
  value.tasks[0].name = 'Task | **bold**\nsecond line'
  value.background = '# fake heading\n<script>alert(1)</script>\n```\n![image](https://example.test/image)'
  value.output.observations[0].findings[0].evidence = ['a)/../../?token=x#fragment']
  const markdown = reportTools.reviewReportMarkdown(value, 'Title [link]', t, 'en', 'http://user:secret@localhost:8711/old?query=x')
  assert.ok(markdown.includes('Task \\| \\*\\*bold\\*\\*<br>second line'))
  assert.ok(markdown.includes('\\# fake heading'))
  assert.ok(markdown.includes('&lt;script&gt;'))
  assert.ok(!markdown.includes('<script>'))
  assert.ok(!markdown.includes('\n```'))
  assert.ok(!markdown.includes('![image]'))
  assert.ok(!markdown.includes('user:secret'))
  assert.ok(markdown.includes('a%29%2F..%2F..%2F%3Ftoken%3Dx%23fragment'))
  assert.equal(reportTools.reviewEvidenceUrl(value, 'log', 'javascript:alert(1)'), undefined)
})

test('only completed reports export; filenames are portable and bounded for Chinese and emoji names', () => {
  const value = report()
  assert.throws(() => reportTools.reviewReportMarkdown({ ...value, status: 'running' }, 'Review', t))
  assert.throws(() => reportTools.reviewReportMarkdown({ ...value, output: undefined }, 'Review', t))
  const filename = reportTools.reviewReportFilename('测试🧪'.repeat(100) + '/:\n?.txt', value)
  assert.ok(filename.endsWith('_20261027_100000_report-o.md'))
  assert.ok(Buffer.byteLength(filename) < 255)
  assert.ok(!filename.includes('/'))
  assert.ok(!filename.includes(':'))
})

test('AI suggestions deduplicate against existing and incoming points without changing the user selection', () => {
  const value = report().config
  const next = mergeSuggestedPoints(value, [{ id: 'duplicate-name', name: ' token ', instructions: '' }, { id: 'new', name: 'Strategy', instructions: '' }, { id: 'new-again', name: 'STRATEGY', instructions: '' }, { id: 'tokens', name: 'ID collision', instructions: '' }])
  assert.deepEqual(next.selected, ['tokens'])
  assert.deepEqual(next.points.map(point => point.name), ['Token', 'Strategy'])
  assert.equal(value.points.length, 1)
  const full = { ...value, points: Array.from({ length: 30 }, (_, i) => ({ id: String(i), name: String(i), instructions: '' })) }
  assert.equal(mergeSuggestedPoints(full, [{ id: '31', name: 'Extra', instructions: '' }]).points.length, 30)
})
