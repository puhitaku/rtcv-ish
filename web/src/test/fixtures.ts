import type { Settings, Status } from '@/api/types'

const full = {
  '1': { min: '0', max: '255' },
  '2': { min: '0', max: '65535' },
  '4': { min: '0', max: '4294967295' },
  '8': { min: '0', max: '18446744073709551615' },
}

export function settingsFixture(): Settings {
  return {
    engine: 'nightmare',
    intensity: 1,
    errorDelay: 1,
    radius: 'spread',
    precision: 1,
    alignment: 0,
    autoCorrupt: false,
    maxInfiniteUnits: 50,
    lockUnits: false,
    freezeMode: 'hard',
    addressRange: { enabled: false, start: 0, end: 0x400000 },
    nightmare: { algo: 'random', ranges: structuredClone(full) },
    hellgenie: { ranges: structuredClone(full) },
    distortion: { delay: 50 },
    vector: { limiterList: '', valueList: '', unlockPrecision: false },
    cluster: {
      limiterList: '',
      chunkSize: 3,
      method: 'random',
      modifier: 1,
      direction: 'forwards',
      splitUnits: true,
      filterAll: false,
    },
    custom: {
      source: 'value',
      valueSource: 'random',
      ranges: structuredClone(full),
      valueList: '',
      storeAddress: 'same',
      storeTime: 'immediate',
      storeType: 'once',
      tilt: '0',
      delay: 0,
      lifetime: 1,
      loop: false,
      limiterList: '',
      limiterTime: 'none',
      limiterInverted: false,
    },
    reroll: {
      address: false,
      sourceAddress: true,
      domain: false,
      sourceDomain: true,
      followCustomEngine: false,
    },
    gameProtection: { enabled: false, intervalSeconds: 5, keep: 10 },
  }
}

export function statusFixture(over: Partial<Status> = {}): Status {
  return {
    version: 'test',
    dataDir: '/tmp/data',
    connected: true,
    unresponsive: false,
    address: '127.0.0.1:42069',
    emulator: {
      name: 'fake',
      version: '1',
      system: 'nds',
      protocolVersion: 1,
      capabilities: {
        savestates: true,
        screenshot: true,
        input: false,
        loadRom: true,
        reset: true,
        maxPayload: 1 << 20,
        scanlineUnits: true,
        hardUnits: true,
      },
    },
    game: {
      state: 'running',
      frame: 10,
      romPath: '/r/game.nds',
      title: 'GAME',
      code: 'ABCD',
      console: 'nds',
    },
    protectionBackups: 0,
    blastLayer: { available: false, on: false },
    ...over,
  }
}
