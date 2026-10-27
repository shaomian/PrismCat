#!/usr/bin/env node
// 两个平台共用本地构建流程；平台入口只负责环境和双击运行体验。
import { spawn } from 'node:child_process'
import { createHash } from 'node:crypto'
import { constants } from 'node:fs'
import { access, cp, mkdir, mkdtemp, readFile, readdir, realpath, rename, rm, writeFile } from 'node:fs/promises'
import { delimiter, dirname, join, resolve } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import { fileURLToPath } from 'node:url'

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const webDir = join(projectRoot, 'web')
const cacheDir = join(projectRoot, '.cache', 'local-debug')
const pidFile = join(cacheDir, 'server.pid')
const windows = process.platform === 'win32'
const binary = join(projectRoot, windows ? 'prismcat.exe' : 'prismcat')
let activeChild
let interrupted

function checkInterrupted() {
    if (interrupted) throw new Error(`收到 ${interrupted}，已停止。`)
}

function forwardSignal(signal) {
    interrupted = signal
    activeChild?.kill(signal)
}

async function exists(path) {
    try { await access(path); return true }
    catch (error) { if (error.code === 'ENOENT') return false; throw error }
}

async function findCommand(name) {
    for (const dir of (process.env.PATH ?? '').split(delimiter).filter(Boolean)) {
        for (const extension of windows ? ['.exe', '.cmd', '.bat'] : ['']) {
            const candidate = join(dir, name + extension)
            try {
                await access(candidate, windows ? constants.F_OK : constants.X_OK)
                return candidate
            } catch (error) {
                if (!['ENOENT', 'EACCES', 'ENOTDIR'].includes(error.code)) throw error
            }
        }
    }
    throw new Error(`缺少命令 ${name}，请安装 Go（版本见 go.mod）和 Node.js（建议 24）。`)
}

async function run(command, args, { cwd = projectRoot, env = process.env, capture = false } = {}) {
    checkInterrupted()
    const child = spawn(command, args, { cwd, env, shell: false, stdio: capture ? ['ignore', 'pipe', 'inherit'] : 'inherit', windowsHide: true })
    activeChild = child
    let output = ''
    if (capture) child.stdout.on('data', chunk => { output += chunk })
    try {
        await new Promise((resolvePromise, reject) => {
            child.once('error', reject)
            child.once('close', (code, signal) => {
                if (code === 0) resolvePromise()
                else reject(new Error(`${command} 执行失败（${signal ?? code}）。`))
            })
        })
        checkInterrupted()
        return output.trim()
    } finally {
        if (activeChild === child) activeChild = undefined
    }
}

async function npmCLI() {
    const command = await findCommand('npm')
    const resolved = await realpath(command)
    // Unix 通常链接至 npm-cli.js；Windows npm.cmd 与 node_modules 同目录。
    const candidates = [
        ...(resolved.endsWith('.js') ? [resolved] : []),
        join(dirname(command), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
        join(dirname(resolved), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
        join(dirname(process.execPath), 'node_modules', 'npm', 'bin', 'npm-cli.js'),
    ]
    for (const candidate of candidates) if (await exists(candidate)) return candidate
    throw new Error('找不到 npm-cli.js，请修复 Node.js/npm 安装。')
}

async function buildWeb(npm) {
    console.log('[1/4] 构建前端…')
    const hash = createHash('sha256')
    for (const file of ['package.json', 'package-lock.json']) hash.update(await readFile(join(webDir, file)))
    hash.update(`${process.platform}/${process.arch}/${process.version}`)
    const checksum = hash.digest('hex')
    const checksumFile = join(cacheDir, 'npm.sha256')
    const previous = await exists(checksumFile) ? await readFile(checksumFile, 'utf8') : ''
    if (!await exists(join(webDir, 'node_modules')) || checksum !== previous) {
        // npm ci 可能先清空 node_modules；安装失败后不能再信任旧校验值。
        await rm(checksumFile, { force: true })
        await run(process.execPath, [npm, 'ci'], { cwd: webDir })
        await writeFile(checksumFile, checksum)
    }
    await run(process.execPath, [npm, 'run', 'build'], { cwd: webDir })
}

async function syncUI() {
    console.log('\n[2/4] 同步嵌入页面…')
    const source = join(webDir, 'dist')
    const destination = join(projectRoot, 'internal', 'server', 'ui')
    if (!await exists(join(source, 'index.html')) || !await exists(join(destination, 'README.md'))) {
        throw new Error('缺少前端构建结果 index.html 或嵌入页面占位 README.md。')
    }
    for (const entry of await readdir(destination)) {
        if (entry !== 'README.md') await rm(join(destination, entry), { recursive: true, force: true })
    }
    for (const entry of await readdir(source)) {
        if (entry !== 'README.md') await cp(join(source, entry), join(destination, entry), { recursive: true })
    }
}

function isRunning(pid) {
    try { process.kill(pid, 0); return true }
    catch (error) { if (error.code === 'ESRCH') return false; throw error }
}

async function stopOldServer() {
    if (windows) {
        // 完整路径通过环境传递，避免空格/引号参与 PowerShell 代码拼接。
        await run('powershell.exe', ['-NoProfile', '-NonInteractive', '-Command', `
            $ErrorActionPreference = 'Stop'
            $targets = @(Get-CimInstance Win32_Process -Filter "Name='prismcat.exe'" |
                Where-Object { $_.ExecutablePath -eq $env:PRISMCAT_BUILD_BINARY })
            foreach ($target in $targets) {
                $instance = Get-Process -Id $target.ProcessId -ErrorAction SilentlyContinue
                if ($instance -and $instance.Path -eq $env:PRISMCAT_BUILD_BINARY) {
                    $instance.Kill()
                    if (-not $instance.WaitForExit(30000)) { throw 'Old PrismCat process did not exit.' }
                }
            }
        `], { env: { ...process.env, PRISMCAT_BUILD_BINARY: binary } })
        return
    }
    if (!await exists(pidFile)) return
    const recorded = (await readFile(pidFile, 'utf8')).trim()
    if (!/^\d+$/.test(recorded)) return
    const pid = Number(recorded)
    if (!Number.isSafeInteger(pid) || pid <= 1 || !isRunning(pid)) return

    // 兼容旧 .command 的 PID 文件，并核对实际可执行路径以避免 PID 复用误停。
    try {
        const executable = process.platform === 'linux'
            ? await realpath(`/proc/${pid}/exe`)
            : await run('ps', ['-p', String(pid), '-o', 'comm='], { capture: true })
        // /var 和 /private/var 等别名必须归一化后再比较。
        const expected = await exists(binary) ? await realpath(binary) : binary
        if (await realpath(executable) !== expected) return
        process.kill(pid, 'SIGTERM')
    } catch (error) {
        if (!isRunning(pid)) return // 校验期间旧实例可能已经自行退出。
        throw error
    }
    for (let attempt = 0; attempt < 120; attempt++) {
        checkInterrupted()
        if (!isRunning(pid)) return
        await delay(250)
    }
    throw new Error('旧服务在 30 秒内未退出，请检查其终端日志后重试。')
}

async function launch(args) {
    console.log('默认控制台：http://localhost:8711（实际地址以配置和启动日志为准）')
    console.log('默认配置：data/config.yaml')
    if (windows) {
        const child = spawn(binary, args, { cwd: projectRoot, detached: true, stdio: 'ignore' })
        await new Promise((resolvePromise, reject) => {
            child.once('error', reject)
            child.once('spawn', resolvePromise)
        })
        child.unref()
        await writeFile(pidFile, String(child.pid))
        console.log('已启动 Windows 托盘程序；可通过托盘菜单退出。')
        return
    }
    console.log('运行日志显示在当前终端，按 Ctrl+C 停止。\n')
    const child = spawn(binary, args, { cwd: projectRoot, stdio: 'inherit' })
    activeChild = child
    const completion = new Promise((resolvePromise, reject) => {
        child.once('error', reject)
        child.once('close', (code, signal) => {
            if (code === 0 || interrupted) resolvePromise()
            else reject(new Error(`PrismCat 已退出（${signal ?? code}）。`))
        })
    })
    try {
        await Promise.all([completion, writeFile(pidFile, String(child.pid ?? ''))])
    } finally {
        if (child.pid && child.exitCode === null && child.signalCode === null) child.kill('SIGTERM')
        await completion.catch(() => {})
        activeChild = undefined
        if (await exists(pidFile) && (await readFile(pidFile, 'utf8')).trim() === String(child.pid)) {
            await rm(pidFile, { force: true })
        }
    }
}

async function main() {
    let args = process.argv.slice(2)
    if (args[0] === '--help') {
        console.log('用法：node scripts/build-and-run.mjs [--build-only] [后端参数]')
        console.log('构建并重启本地服务；--build-only 仅构建，不停止或替换正在运行的程序。')
        console.log('仅构建的程序保存在 .cache/local-debug/，成功后会输出路径。')
        console.log('例如：node scripts/build-and-run.mjs -config "data/my config.yaml"')
        return
    }
    const buildOnly = args[0] === '--build-only'
    if (buildOnly) args = args.slice(1)
    const go = await findCommand('go')
    const npm = await npmCLI()
    if (windows && !buildOnly) await findCommand('powershell')
    await mkdir(cacheDir, { recursive: true })
    let buildDir
    try {
        await buildWeb(npm)
        await syncUI()
        console.log('\n[3/4] 编译后端…')
        buildDir = await mkdtemp(join(cacheDir, 'build-'))
        const output = join(buildDir, windows ? 'prismcat.exe' : 'prismcat')
        const flags = windows ? ['-ldflags=-H windowsgui -s -w'] : []
        await run(go, ['build', ...flags, '-o', output, './cmd/prismcat'], {
            env: windows ? { ...process.env, CGO_ENABLED: '0' } : process.env,
        })
        checkInterrupted()
        if (buildOnly) {
            console.log(`\n[4/4] 仅构建完成：${output}`)
            buildDir = undefined
            return
        }
        console.log('\n[4/4] 重启本地服务…')
        await stopOldServer()
        checkInterrupted()
        await rename(output, binary)
        await launch(args)
    } finally {
        if (buildDir) await rm(buildDir, { recursive: true, force: true })
    }
}

const onInterrupt = () => forwardSignal('SIGINT')
const onTerminate = () => forwardSignal('SIGTERM')
process.on('SIGINT', onInterrupt)
process.on('SIGTERM', onTerminate)
try {
    await main()
} catch (error) {
    console.error(`\n${error.message}`)
    process.exitCode = 1
} finally {
    if (interrupted) process.exitCode = interrupted === 'SIGINT' ? 130 : 143
    process.removeListener('SIGINT', onInterrupt)
    process.removeListener('SIGTERM', onTerminate)
}
