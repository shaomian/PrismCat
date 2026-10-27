import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { createServer } from 'vite'

let server
let capture
before(async () => {
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  capture = await server.ssrLoadModule('/src/lib/trace-capture.ts')
})
after(async () => { await server?.close() })

test('capture names fit the existing UTF-8 storage limit, including Chinese and emoji entries', () => {
  const date = new Date(2026, 9, 26, 9, 5)
  assert.equal(capture.defaultCaptureName('codex', date), 'codex · 10-26 09:05')
  const long = capture.defaultCaptureName('渗透🧪入口'.repeat(60), date)
  assert.ok(capture.captureNameBytes(long) <= 200)
  assert.ok(long.endsWith(' · 10-26 09:05'))
  assert.ok(!long.includes('\uFFFD'))
  assert.equal(capture.captureNameBytes('  中文  '), 6)
  assert.ok(capture.captureNameBytes('中'.repeat(67)) > 200)
})

test('capture token display preserves unknown usage and real zero values', () => {
  assert.equal(capture.captureTokens({}), undefined)
  assert.equal(capture.captureTokens({ usage_input_tokens: 0, usage_output_tokens: 0 }), 0)
  assert.equal(capture.captureTokens({ usage_input_tokens: 120, usage_output_tokens: 30 }), 150)
  assert.equal(capture.captureTokens({ usage_output_tokens: 30 }), 30)
})
