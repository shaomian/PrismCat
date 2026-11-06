import assert from 'node:assert/strict'
import { after, before, test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { createInstance } from 'i18next'
import { I18nextProvider } from 'react-i18next'
import { MemoryRouter } from 'react-router-dom'
import { createServer } from 'vite'

let server, api, SystemBackups, SystemTab, StoragePolicySetting, BackupImport, policy
before(async () => {
  server = await createServer({ appType: 'custom', logLevel: 'silent', server: { middlewareMode: true } })
  api = await server.ssrLoadModule('/src/lib/api.ts')
  ;({ SystemBackups } = await server.ssrLoadModule('/src/components/SystemBackups.tsx'))
  ;({ SystemTab } = await server.ssrLoadModule('/src/pages/settings/SystemTab.tsx'))
  ;({ StoragePolicySetting } = await server.ssrLoadModule('/src/components/StoragePolicySetting.tsx'))
  ;({ BackupImport } = await server.ssrLoadModule('/src/components/BackupImport.tsx'))
  policy = await server.ssrLoadModule('/src/lib/storage-policy.ts')
})
after(async () => { await server?.close() })

test('backup tasks use authenticated API requests while downloads stay browser-native', async context => {
  const requests = []
  const job = { id: 'backup-1', state: 'running', progress: { phase: 'snapshot', files: 0, bytes: 0 } }
  context.mock.method(globalThis, 'fetch', async (url, init) => {
    requests.push({ url, init })
    return new Response(JSON.stringify(init?.method === 'POST' ? job : { backups: [job], cleanup_paused: true }))
  })
  const controller = new AbortController()
  const list = await api.fetchBackups(controller.signal)
  assert.equal(list.cleanup_paused, true)
  assert.equal(requests[0].init.signal, controller.signal)
  assert.equal((await api.createBackup()).id, job.id)
  assert.equal(requests[1].init.headers['Content-Type'], 'application/json')
  assert.equal(requests[1].init.method, 'POST')
  await api.deleteBackup('id/with spaces')
  assert.equal(requests[2].url, '/api/system/backups/id%2Fwith%20spaces')
  assert.equal(requests[2].init.method, 'DELETE')
  await api.resumeStorageCleanup()
  assert.equal(requests[3].url, '/api/system/cleanup/resume')
  assert.equal(api.backupDownloadUrl('id/with spaces'), '/api/system/backups/id%2Fwith%20spaces/download')
  assert.equal(requests.length, 4, 'download URL must not fetch the archive into memory')
})

test('expired authentication reaches the shared login handler and failed creation is rejected', async context => {
  let expired = 0
  api.onAuthExpired(() => { expired++ })
  try {
    context.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ error: 'unauthorized' }), { status: 401 }))
    await assert.rejects(api.createBackup(), error => error.status === 401)
    assert.equal(expired, 1)
  } finally { api.onAuthExpired(null) }
})

test('backup import streams the file and confirmation only schedules next-start restoration', async context => {
  const calls = []
  const job = { id: 'import-1', state: 'checking' }
  context.mock.method(globalThis, 'fetch', async (url, init) => {
    calls.push({ url, init })
    return new Response(JSON.stringify(job))
  })
  const file = new File(['test-zip'], 'backup.zip', { type: 'application/zip' })
  const controller = new AbortController()
  assert.equal((await api.uploadBackup(file, controller.signal)).id, job.id)
  assert.equal(calls[0].url, '/api/system/backup-import')
  assert.equal(calls[0].init.body, file, 'do not read a large ZIP into memory')
  assert.equal(calls[0].init.signal, controller.signal)
  assert.equal(calls[0].init.headers['Content-Type'], 'application/zip')
  await api.confirmBackupImport('id/with space')
  assert.equal(calls[1].url, '/api/system/backup-import/id%2Fwith%20space/confirm')
  assert.deepEqual(JSON.parse(calls[1].init.body), { confirmed: true })
  await api.cancelBackupImport('id/with space')
  assert.equal(calls[2].init.method, 'DELETE')
  assert.equal(calls[2].url, '/api/system/backup-import/id%2Fwith%20space')
})

test('pending import is visible after returning to settings, with localized restart state', async () => {
  for (const lang of ['zh', 'en']) {
    const locale = JSON.parse(readFileSync(new URL(`../src/locales/${lang}.json`, import.meta.url), 'utf8'))
    const i18n = createInstance()
    await i18n.init({ lng: lang, resources: { [lang]: { translation: locale } } })
    const html = renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(BackupImport, {
      job: { id: 'pending-1', state: 'pending' }, supported: true, open: false, onOpenChange() {}, async onChanged() {},
    })))
    assert.ok(html.includes(locale.backups.import_states.pending))
    assert.ok(html.includes(locale.backups.import_view))
    assert.ok(!html.includes('backups.import_states.'))
  }
})

test('backup screen explains restore boundaries and offline commands in both languages', async () => {
  for (const lang of ['zh', 'en']) {
    const i18n = createInstance()
    const locale = JSON.parse(readFileSync(new URL(`../src/locales/${lang}.json`, import.meta.url), 'utf8'))
    await i18n.init({ lng: lang, resources: { [lang]: { translation: locale } } })
    const html = renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(SystemBackups, {
      status: { jobs: [], error: '', running: false },
    })))
    assert.ok(html.includes(locale.backups.title))
    assert.ok(html.includes(locale.backups.scope))
    assert.ok(html.includes('backup restore --file backup.zip --target data --dry-run'))
    assert.ok(html.includes(locale.backups.empty))
    assert.ok(!html.includes('backups.description'))
  }
})

test('storage policy preserves exact byte limits and keeps zero policies independent', () => {
  for (const bytes of [0, 1, 123456789, 3 * 1024 ** 3 + 17]) {
    const original = { retention_days: 30, max_storage_bytes: bytes }
    const draft = policy.storagePolicyDraft(original)
    assert.deepEqual(policy.parseStoragePolicy(draft), original)
    assert.equal(policy.storagePolicyChanged(draft, original), false)
    draft.days = '60'
    assert.deepEqual(policy.parseStoragePolicy(draft), { ...original, retention_days: 60 })
  }
  assert.deepEqual(policy.parseStoragePolicy({ days: '0', limitGB: '5' }), { retention_days: 0, max_storage_bytes: 5 * 1024 ** 3 })
  assert.deepEqual(policy.parseStoragePolicy({ days: '30', limitGB: '0' }), { retention_days: 30, max_storage_bytes: 0 })
  assert.deepEqual(policy.parseStoragePolicy({ days: '30', limitGB: '0.125' }), { retention_days: 30, max_storage_bytes: 128 * 1024 ** 2 })
  for (const days of ['', '-1', '1.5', 'Infinity', '106752']) assert.equal(policy.parseStoragePolicy({ days, limitGB: '0' }), null)
  for (const limitGB of ['', '-1', 'Infinity', '1e100', '1e-20']) assert.equal(policy.parseStoragePolicy({ days: '30', limitGB }), null)
})

test('system screen groups retention with storage and keeps protection above backups in both languages', async () => {
  for (const lang of ['zh', 'en']) {
    const locale = JSON.parse(readFileSync(new URL(`../src/locales/${lang}.json`, import.meta.url), 'utf8'))
    const i18n = createInstance()
    await i18n.init({ lng: lang, resources: { [lang]: { translation: locale } } })
    const props = {
      t: i18n.t.bind(i18n), metrics: null, metricsLoading: false, metricsError: '', memoryUsedPercent: null,
      metricsUpdatedAt: '-', storageUsage: null, storageLoading: false, storageError: '', storageCalculatedAt: '-',
      updateInfo: null, updateLoading: false, updateError: '',
      config: { version: 'dev', storage: { retention_days: 30, max_storage_bytes: 0, cleanup_paused: true } },
      onStoragePolicySaved() {}, loadMetrics() {}, handleCalculateStorage() {}, handleCheckUpdate() {},
    }
    const html = renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(MemoryRouter, null, createElement(SystemTab, props))))
    const titles = [locale.settings.system_status_title, locale.settings.storage_management_title, locale.settings.storage_policy_title,
      locale.task_cleanup.title, locale.backups.title, locale.settings.update_title]
    for (let index = 1; index < titles.length; index++) assert.ok(html.indexOf(titles[index - 1]) < html.indexOf(titles[index]), titles[index])
    assert.ok(html.includes(locale.backups.cleanup_paused))
    assert.ok(html.indexOf(locale.backups.cleanup_paused) < html.indexOf(locale.backups.title))
    assert.ok(html.includes('value="30"'))
    assert.ok(html.includes('value="0"'))
    assert.ok(!html.includes('settings.storage_policy_'))
  }
})

test('disabling day-based retention disables manual expired-log cleanup', async () => {
  const locale = JSON.parse(readFileSync(new URL('../src/locales/en.json', import.meta.url), 'utf8'))
  const i18n = createInstance()
  await i18n.init({ lng: 'en', resources: { en: { translation: locale } } })
  const html = renderToStaticMarkup(createElement(I18nextProvider, { i18n }, createElement(MemoryRouter, null, createElement(StoragePolicySetting, {
    policy: { retention_days: 0, max_storage_bytes: 5 * 1024 ** 3 }, onSaved() {}, cleanupPaused: false,
    async onResumeCleanup() {}, onCleanupCompleted() {},
  }))))
  assert.ok(html.includes(locale.task_cleanup.disabled))
  assert.match(html, /<button[^>]*disabled=""[^>]*>[\s\S]*?Preview cleanup<\/button>/)
})
