import type { Capabilities, FreezeMode } from '@/api/types'

export const FREEZE_MODES: { value: FreezeMode; label: string }[] = [
  { value: 'frame', label: 'per frame' },
  { value: 'scanline', label: 'per scanline' },
  { value: 'hard', label: 'hard' },
]

/**
 * The mode the core uses for infinite value units: the setting, falling
 * back from hard to scanline to frame when the emulator lacks it. Without
 * capabilities (disconnected) the setting is returned unchanged.
 */
export function effectiveFreezeMode(
  mode: FreezeMode,
  caps: Pick<Capabilities, 'scanlineUnits' | 'hardUnits'> | undefined,
): FreezeMode {
  if (!caps) return mode
  let m = mode
  if (m === 'hard' && !caps.hardUnits) m = 'scanline'
  if (m === 'scanline' && !caps.scanlineUnits) m = 'frame'
  return m
}
