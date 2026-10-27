// 隔离执行启动脚本，验证文件替换与进程管理；不接触仓库的程序和运行数据。
import assert from 'node:assert/strict'
import { spawn, execFile } from 'node:child_process'
import { once } from 'node:events'
import { access, chmod, copyFile, mkdir, mkdtemp, readFile, readdir, realpath, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { promisify } from 'node:util'

const exec = promisify(execFile)
const source = dirname(fileURLToPath(import.meta.url))
const posix = { skip: process.platform === 'win32' }

async function fixture(t) {
    const root = await mkdtemp(join(tmpdir(), 'prismcat build 中文 '))
    t.after(() => rm(root, { recursive: true, force: true }))
    for (const dir of ['scripts', 'tools', 'web', 'internal/server/ui', '.cache/local-debug']) {
        await mkdir(join(root, dir), { recursive: true })
    }
    await copyFile(join(source, 'build-and-run.mjs'), join(root, 'scripts/build-and-run.mjs'))
    await writeFile(join(root, 'web/package.json'), '{}')
    await writeFile(join(root, 'web/package-lock.json'), '{}')
    await writeFile(join(root, 'internal/server/ui/README.md'), 'tracked placeholder')
    await writeFile(join(root, 'internal/server/ui/stale.js'), 'old asset')
    await writeFile(join(root, 'tools/npm-cli.js'), `
        const fs = require('node:fs')
        fs.appendFileSync('npm-calls', process.argv.slice(2).join(' ') + '\\n')
        if (process.argv[2] === 'ci') {
            fs.mkdirSync('node_modules', {recursive:true})
            if (process.env.FAIL_CI) process.exit(8)
        } else {
            fs.mkdirSync('dist', {recursive:true})
            fs.writeFileSync('dist/index.html', '<html>new UI</html>')
            fs.writeFileSync('dist/README.md', 'must not overwrite placeholder')
        }
    `)
    await symlink('npm-cli.js', join(root, 'tools/npm'))
    await chmod(join(root, 'tools/npm-cli.js'), 0o755)
    const server = `#!${process.execPath}\nconst fs = require('node:fs'); fs.writeFileSync('server.json', JSON.stringify({cwd:process.cwd(), args:process.argv.slice(2)})); if(process.env.SERVER_WAIT) setInterval(()=>{},1000);\n`
    await writeFile(join(root, 'tools/go'), `#!/usr/bin/env node
        const fs = require('node:fs')
        if (process.env.FAIL_GO) process.exit(9)
        const output = process.argv[process.argv.indexOf('-o') + 1]
        fs.writeFileSync(output, ${JSON.stringify(server)}, {mode:0o755})
    `)
    await chmod(join(root, 'tools/go'), 0o755)
    return {
        root,
        run: (args = [], env = {}) => exec(process.execPath, [join(root, 'scripts/build-and-run.mjs'), ...args], {
            cwd: tmpdir(), timeout: 15000,
            env: { ...process.env, PATH: `${join(root, 'tools')}:${process.env.PATH}`, ...env },
        }),
    }
}

async function oldServer(t, root) {
    const binary = join(root, 'prismcat')
    const program = join(root, 'old-server.go')
    await writeFile(program, 'package main\nimport "time"\nfunc main() { time.Sleep(time.Minute) }\n')
    await exec('go', ['build', '-o', binary, program], {
        cwd: root, timeout: 30000,
        env: { ...process.env, GO111MODULE: 'off', GOCACHE: join(source, '..', '.gocache') },
    })
    const original = await readFile(binary)
    const child = spawn(binary, ['60'], { stdio: 'ignore' })
    await once(child, 'spawn')
    t.after(() => { if (child.exitCode === null) child.kill() })
    await writeFile(join(root, '.cache/local-debug/server.pid'), String(child.pid))
    return { child, binary, original }
}

test('help works without Go or npm installed', async () => {
    const { stdout } = await exec(process.execPath, [join(source, 'build-and-run.mjs'), '--help'], { env: { ...process.env, PATH: '' } })
    assert.match(stdout, /--build-only/)
})

test('build-only preserves a running executable, removes stale assets and reuses dependencies', posix, async t => {
    const { root, run } = await fixture(t)
    const { child, binary, original } = await oldServer(t, root)
    const first = await run(['--build-only'])
    assert.match(first.stdout, /仅构建完成/)
    assert.equal(child.exitCode, null)
    assert.deepEqual(await readFile(binary), original)
    assert.equal(await readFile(join(root, 'internal/server/ui/README.md'), 'utf8'), 'tracked placeholder')
    assert.equal(await readFile(join(root, 'internal/server/ui/index.html'), 'utf8'), '<html>new UI</html>')
    await assert.rejects(access(join(root, 'internal/server/ui/stale.js')))
    await run(['--build-only'])
    assert.equal(await readFile(join(root, 'web/npm-calls'), 'utf8'), 'ci\nrun build\nrun build\n')
    await writeFile(join(root, 'web/package-lock.json'), '{"changed":true}')
    await assert.rejects(run(['--build-only'], { FAIL_CI: '1' }))
    await run(['--build-only'])
    assert.equal(await readFile(join(root, 'web/npm-calls'), 'utf8'), 'ci\nrun build\nrun build\nci\nci\nrun build\n')
})

test('Go build failure keeps old server, executable and PID; temporary output is cleaned', posix, async t => {
    const { root, run } = await fixture(t)
    const { child, binary, original } = await oldServer(t, root)
    await assert.rejects(run([], { FAIL_GO: '1' }), error => error.code === 1)
    assert.equal(child.exitCode, null)
    assert.deepEqual(await readFile(binary), original)
    assert.equal(await readFile(join(root, '.cache/local-debug/server.pid'), 'utf8'), String(child.pid))
    assert.equal((await readdir(join(root, '.cache/local-debug'))).filter(name => name.startsWith('build-')).length, 0)
})

test('restart verifies executable path, replaces the old process and forwards arguments from another cwd', posix, async t => {
    const { root, run } = await fixture(t)
    const { child } = await oldServer(t, root)
    const exited = once(child, 'exit')
    await run(['-config', 'data/a b.yaml', 'literal;$value'])
    await exited
    assert.equal(child.signalCode, 'SIGTERM')
    const server = JSON.parse(await readFile(join(root, 'server.json'), 'utf8'))
    assert.deepEqual(server, { cwd: await realpath(root), args: ['-config', 'data/a b.yaml', 'literal;$value'] })
    await assert.rejects(access(join(root, '.cache/local-debug/server.pid')))
})

test('a PID belonging to an unrelated process is left alone', posix, async t => {
    const { root, run } = await fixture(t)
    await writeFile(join(root, '.cache/local-debug/server.pid'), String(process.pid))
    await run()
    assert.equal(process.kill(process.pid, 0), true)
    await access(join(root, 'server.json'))
})

test('terminating the launcher stops its foreground server and cleans the PID file', posix, async t => {
    const { root } = await fixture(t)
    const launcher = spawn(process.execPath, [join(root, 'scripts/build-and-run.mjs')], {
        cwd: tmpdir(), stdio: 'ignore',
        env: { ...process.env, PATH: `${join(root, 'tools')}:${process.env.PATH}`, SERVER_WAIT: '1' },
    })
    const closed = once(launcher, 'close')
    t.after(async () => {
        if (launcher.exitCode === null && launcher.signalCode === null) launcher.kill('SIGTERM')
        await closed
    })
    let ready = false
    for (let attempt = 0; attempt < 200; attempt++) {
        try { await access(join(root, 'server.json')); ready = true; break }
        catch { await delay(50) }
    }
    assert.equal(ready, true, 'foreground test server should start')
    const serverPid = Number(await readFile(join(root, '.cache/local-debug/server.pid'), 'utf8'))
    launcher.kill('SIGTERM')
    const [code] = await closed
    assert.equal(code, 143)
    await assert.rejects(access(join(root, '.cache/local-debug/server.pid')))
    assert.throws(() => process.kill(serverPid, 0), { code: 'ESRCH' })
})
