// Builds the frontend and the Go binaries, then starts an emulator and
// rtcv-ish (with the frontend embedded) on free ports. The emulator is
// rtcv-ish-fakeemu by default, or a real melonDS when RTCVISH_MELONDS is set
// (see README.md). Tests read E2E_BASE_URL, E2E_EMU_ADDR, E2E_ROM and
// E2E_REAL_EMU.
import { spawn, execFileSync, type ChildProcess } from 'node:child_process'
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const webDir = resolve(import.meta.dirname, '..')
const repoRoot = resolve(webDir, '..')

// Software renderer, no GL, and muted (Instance0.Audio.Volume ranges 0-256).
const melonDSConfig = '[3D]\nRenderer = 0\n[Screen]\nUseGL = false\n[Instance0.Audio]\nVolume = 0\n'

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

function start(
  name: string,
  bin: string,
  args: string[],
  opts: { env?: NodeJS.ProcessEnv; cwd?: string } = {},
): ChildProcess {
  const p = spawn(bin, args, { stdio: ['ignore', 'pipe', 'pipe'], ...opts })
  const prefix = (d: Buffer) =>
    process.env.E2E_VERBOSE && process.stderr.write(`[${name}] ${d.toString()}`)
  p.stdout?.on('data', prefix)
  p.stderr?.on('data', prefix)
  return p
}

// stop sends SIGTERM, then SIGKILL if the process is still alive after ms.
async function stop(p: ChildProcess, ms = 3_000): Promise<void> {
  if (p.exitCode !== null || p.signalCode !== null) return
  const exited = new Promise<void>((r) => p.once('exit', () => r()))
  p.kill('SIGTERM')
  const timedOut = await Promise.race([
    exited.then(() => false),
    new Promise<boolean>((r) => setTimeout(() => r(true), ms)),
  ])
  if (timedOut) {
    p.kill('SIGKILL')
    await Promise.race([exited, new Promise((r) => setTimeout(r, ms))])
  }
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

async function waitForPort(port: number, proc: ChildProcess, ms = 20_000): Promise<void> {
  const { connect } = await import('node:net')
  const until = Date.now() + ms
  while (Date.now() < until) {
    if (proc.exitCode !== null) throw new Error(`emulator exited with ${proc.exitCode}`)
    const ok = await new Promise<boolean>((r) => {
      const s = connect(port, '127.0.0.1')
      s.once('connect', () => {
        s.destroy()
        r(true)
      })
      s.once('error', () => r(false))
    })
    if (ok) return
    await new Promise((r) => setTimeout(r, 200))
  }
  throw new Error(`timed out waiting for the emulator on port ${port}`)
}

function melonDSPath(): string | undefined {
  let p = process.env.RTCVISH_MELONDS
  if (!p) return undefined
  p = p.replace(/\/+$/, '')
  if (p.endsWith('.app')) p = join(p, 'Contents', 'MacOS', 'melonDS')
  if (!existsSync(p)) throw new Error(`RTCVISH_MELONDS: ${p} does not exist`)
  return p
}

export default async function globalSetup() {
  const melonDS = melonDSPath()
  const rom = melonDS
    ? resolve(process.env.RTCVISH_ROM || join(repoRoot, 'test', 'roms', 'hello_world.nds'))
    : '/roms/e2e.nds'
  if (melonDS && !existsSync(rom)) {
    throw new Error(
      process.env.RTCVISH_ROM
        ? `RTCVISH_ROM: ${rom} does not exist`
        : `test ROM ${rom} is missing; run scripts/build-nds-examples.sh to build the test ROMs (or set RTCVISH_ROM)`,
    )
  }

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
  if (!melonDS) {
    execFileSync('go', ['build', '-o', join(bin, 'rtcv-ish-fakeemu'), './cmd/rtcv-ish-fakeemu'], {
      cwd: repoRoot,
      stdio: 'inherit',
    })
  }

  const emuPort = await freePort()
  const corePort = await freePort()
  const emuAddr = `127.0.0.1:${emuPort}`
  const base = `http://127.0.0.1:${corePort}`

  let emu: ChildProcess
  if (melonDS) {
    const configDir = join(tmp, 'melonds')
    mkdirSync(configDir)
    writeFileSync(join(configDir, 'melonDS.toml'), melonDSConfig)
    const env: NodeJS.ProcessEnv = { ...process.env, SDL_AUDIODRIVER: 'dummy' }
    if (process.platform === 'linux' && !env.DISPLAY && !env.WAYLAND_DISPLAY) {
      env.QT_QPA_PLATFORM = 'offscreen'
    }
    emu = start(
      'melonds',
      melonDS,
      ['--rtcvish-listen', emuAddr, '--rtcvish-config-dir', configDir],
      { env, cwd: configDir },
    )
  } else {
    emu = start('fakeemu', join(bin, 'rtcv-ish-fakeemu'), [
      '--listen',
      emuAddr,
      '--log-format',
      'text',
    ])
  }
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

  const stopAll = () => Promise.all([stop(core), stop(emu)])
  try {
    await waitFor(`${base}/api/status`, core)
    await waitForPort(emuPort, emu)
  } catch (e) {
    await stopAll()
    rmSync(tmp, { recursive: true, force: true })
    throw e
  }

  process.env.E2E_BASE_URL = base
  process.env.E2E_EMU_ADDR = emuAddr
  process.env.E2E_ROM = rom
  if (melonDS) process.env.E2E_REAL_EMU = '1'

  return async () => {
    await stopAll()
    rmSync(tmp, { recursive: true, force: true })
  }
}
