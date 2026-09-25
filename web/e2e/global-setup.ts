// Builds the frontend and the Go binaries, then starts the fake emulator and
// rtcv-ish (with the frontend embedded) on free ports. Tests read
// E2E_BASE_URL and E2E_EMU_ADDR.
import { spawn, execFileSync, type ChildProcess } from 'node:child_process'
import { mkdtempSync, rmSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const webDir = resolve(import.meta.dirname, '..')
const repoRoot = resolve(webDir, '..')

function freePort(): Promise<number> {
  return new Promise((ok, fail) => {
    const s = createServer()
    s.once('error', fail)
    s.listen(0, '127.0.0.1', () => {
      const addr = s.address()
      s.close(() => (addr && typeof addr === 'object' ? ok(addr.port) : fail(new Error('no port'))))
    })
  })
}

function start(name: string, bin: string, args: string[]): ChildProcess {
  const p = spawn(bin, args, { stdio: ['ignore', 'pipe', 'pipe'] })
  const prefix = (d: Buffer) =>
    process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d.toString()}`)
  p.stdout?.on('data', prefix)
  p.stderr?.on('data', prefix)
  return p
}

async function waitFor(url: string, proc: ChildProcess, ms = 20_000): Promise<void> {
  const until = Date.now() + ms
  while (Date.now() < until) {
    if (proc.exitCode !== null) throw new Error(`${url}: process exited with ${proc.exitCode}`)
    try {
      const r = await fetch(url)
      if (r.ok) return
    } catch {
      // Not up yet.
    }
    await new Promise((r) => setTimeout(r, 100))
  }
  throw new Error(`timed out waiting for ${url}`)
}

export default async function globalSetup() {
  const tmp = mkdtempSync(join(tmpdir(), 'rtcvish-e2e-'))
  const bin = join(tmp, 'bin')

  if (!process.env.E2E_SKIP_WEB_BUILD) {
    execFileSync('npx', ['vite', 'build'], { cwd: webDir, stdio: 'inherit' })
  }
  execFileSync(
    'go',
    ['build', '-tags', 'embedweb', '-o', join(bin, 'rtcv-ish'), './cmd/rtcv-ish'],
    {
      cwd: repoRoot,
      stdio: 'inherit',
    },
  )
  execFileSync('go', ['build', '-o', join(bin, 'rtcv-ish-fakeemu'), './cmd/rtcv-ish-fakeemu'], {
    cwd: repoRoot,
    stdio: 'inherit',
  })

  const emuPort = await freePort()
  const corePort = await freePort()
  const emuAddr = `127.0.0.1:${emuPort}`
  const base = `http://127.0.0.1:${corePort}`

  const emu = start('fakeemu', join(bin, 'rtcv-ish-fakeemu'), [
    '--listen',
    emuAddr,
    '--log-format',
    'text',
  ])
  const core = start('core', join(bin, 'rtcv-ish'), [
    '--listen',
    `127.0.0.1:${corePort}`,
    '--data-dir',
    join(tmp, 'data'),
    '--seed',
    '1',
    '--log-format',
    'text',
  ])

  const stop = () => {
    core.kill('SIGTERM')
    emu.kill('SIGTERM')
  }
  try {
    await waitFor(`${base}/api/status`, core)
  } catch (e) {
    stop()
    throw e
  }

  process.env.E2E_BASE_URL = base
  process.env.E2E_EMU_ADDR = emuAddr

  return async () => {
    stop()
    await new Promise((r) => setTimeout(r, 300))
    rmSync(tmp, { recursive: true, force: true })
  }
}
