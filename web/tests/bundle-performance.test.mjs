import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'vite'

test('entry and conversation routes keep diagram, Markdown and date-picker code lazy', async () => {
  const result = await build({ logLevel: 'silent', build: { write: false } })
  const output = Array.isArray(result) ? result.flatMap(item => item.output) : result.output
  const chunks = new Map(output.filter(item => item.type === 'chunk').map(item => [item.fileName, item]))
  assert.ok(output.some(item => item.type === 'chunk' && item.name === 'TraceDetail'), 'missing conversation route')
  for (const route of output.filter(item => item.type === 'chunk' && (item.isEntry || item.name === 'TraceDetail'))) {
    const visited = new Set()
    const visit = file => {
      if (visited.has(file)) return
      visited.add(file)
      const chunk = chunks.get(file)
      assert.ok(chunk, `missing static dependency ${file}`)
      for (const id of Object.keys(chunk.modules)) {
        assert.ok(!/node_modules\/(mermaid|react-markdown|react-datepicker)\//.test(id), `${route.name} eagerly loaded ${id}`)
      }
      for (const imported of chunk.imports) visit(imported)
    }
    visit(route.fileName)
  }
})
