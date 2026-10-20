// 一次性脚本：找出 locales 中定义但从未在源码中引用的 i18n key。
// 用法：node scripts/find-unused-i18n-keys.mjs
//
// 判定规则（保守优先，宁可漏报不可误删）：
//   1. key 以带引号的完整字面量出现在源码任意位置（含常量数组、映射表）→ 已使用
//   2. key 以某个模板字符串动态前缀开头（如 `nav.${x}` 的 "nav."）→ 已使用
//   3. 以上都不满足 → 未使用候选
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join } from 'node:path'

const SRC = 'src'

function walk(dir) {
  const out = []
  for (const name of readdirSync(dir)) {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) out.push(...walk(full))
    else if (/\.(tsx?|jsx?)$/.test(name)) out.push(full)
  }
  return out
}

const files = walk(SRC)
const sources = files.map(f => readFileSync(f, 'utf8'))
const allText = sources.join('\n')

// 规则 1：完整字面量 key（'x.y' / "x.y" / `x.y`，段内允许下划线）
const exactKeys = new Set()
for (const m of allText.matchAll(/(['"`])([a-z0-9_]+(?:\.[a-z0-9_]+)+)\1/g)) {
  exactKeys.add(m[2])
}

// 规则 2：模板字符串动态前缀（`x.${...}` → "x."）
const dynamicPrefixes = new Set()
for (const m of allText.matchAll(/`([^`${}]*)\$\{/g)) {
  const prefix = m[1]
  if (prefix) dynamicPrefixes.add(prefix)
}

// t() 字面量调用（含 i18n.t），用于反向检查「使用但缺失」
const tLiteralKeys = new Set()
for (const text of sources) {
  for (const m of text.matchAll(/[^a-zA-Z0-9_]t\(\s*(['"`])([^'"`]+)\1/g)) {
    if (!m[2].includes('${') && m[2].includes('.')) tLiteralKeys.add(m[2])
  }
}

function flatten(obj, prefix = '') {
  const keys = []
  for (const [k, v] of Object.entries(obj)) {
    const full = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) keys.push(...flatten(v, full))
    else keys.push(full)
  }
  return keys
}

const zhKeys = new Set(flatten(JSON.parse(readFileSync('src/locales/zh.json', 'utf8'))))
const enKeys = new Set(flatten(JSON.parse(readFileSync('src/locales/en.json', 'utf8'))))

function used(key) {
  if (exactKeys.has(key)) return true
  for (const p of dynamicPrefixes) {
    if (p && key.startsWith(p)) return true
  }
  return false
}

const unused = [...zhKeys].filter(k => !used(k)).sort()
const missingUsed = [...tLiteralKeys].filter(k => !zhKeys.has(k) || !enKeys.has(k)).sort()

console.log(`zh keys: ${zhKeys.size}, en keys: ${enKeys.size}`)
console.log(`--- unused candidates (${unused.length}) ---`)
console.log(unused.join('\n'))
console.log(`--- used in code but missing from locales (${missingUsed.length}) ---`)
console.log(missingUsed.join('\n') || '(none)')
