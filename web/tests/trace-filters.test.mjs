import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import ts from 'typescript'

const source = readFileSync(new URL('../src/lib/trace-filters.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(source, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText
const module = { exports: {} }
new Function('module', 'exports', compiled)(module, module.exports)
const { defaultTraceFilters, countTraceFilters, traceListQuery } = module.exports

test('trace defaults show all unarchived history without a silent time cutoff', () => {
  const filters = defaultTraceFilters(' task-id ')
  const query = traceListQuery(filters)
  assert.equal(query.q, 'task-id')
  assert.equal(query.archived, false)
  assert.equal(query.start_time, undefined)
  assert.equal(query.has_error, undefined)
  assert.equal(query.pinned, undefined)
  assert.equal(query.status, undefined)
  assert.equal(query.limit, 20)
  assert.equal(countTraceFilters(filters), 0)
})

test('time, entry, archive and task conditions remain independent and combine in one query', () => {
  const filters = { ...defaultTraceFilters('login'), period: '7d', upstream: 'agent-a', archived: true, hasError: true, pinned: true, todo: true, offset: 20 }
  const query = traceListQuery(filters, new Date('2026-10-27T06:30:00Z'))
  assert.equal(query.start_time, '2026-10-20T06:30:00.000Z')
  assert.equal(query.upstream, 'agent-a')
  assert.equal(query.archived, true)
  assert.equal(query.has_error, true)
  assert.equal(query.pinned, true)
  assert.equal(query.status, 'todo')
  assert.equal(query.offset, 20)
  assert.equal(countTraceFilters(filters), 6)
  const reset = defaultTraceFilters(filters.query)
  assert.equal(traceListQuery(reset).q, 'login')
  assert.equal(traceListQuery(reset).archived, false)
  assert.equal(countTraceFilters(reset), 0)
})

test('today uses the browser local day boundary, including the previous UTC date', () => {
  const previousTZ = process.env.TZ
  process.env.TZ = 'Asia/Shanghai'
  try {
    const query = traceListQuery({ ...defaultTraceFilters(), period: 'today' }, new Date('2026-10-27T02:30:00Z'))
    assert.equal(query.start_time, '2026-10-26T16:00:00.000Z')
  } finally {
    if (previousTZ === undefined) delete process.env.TZ
    else process.env.TZ = previousTZ
  }
})

test('the thirty-day option retains rolling duration without restricting other conditions', () => {
  const query = traceListQuery({ ...defaultTraceFilters(), period: '30d', hasError: true }, new Date('2026-10-27T12:00:00Z'))
  assert.equal(query.start_time, '2026-09-27T12:00:00.000Z')
  assert.equal(query.has_error, true)
  assert.equal(query.archived, false)
})
