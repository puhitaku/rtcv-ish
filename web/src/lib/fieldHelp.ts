/**
 * Help texts for blast unit fields, shown as tooltips in the Blast Editor
 * (property editor labels and table headers). Semantics follow RTCV's
 * BlastUnit (design/rtcv-reference.md 2.2).
 */
export type HelpKey =
  | 'enabled'
  | 'locked'
  | 'bigEndian'
  | 'domain'
  | 'address'
  | 'precision'
  | 'source'
  | 'value'
  | 'tilt'
  | 'executeFrame'
  | 'lifetime'
  | 'loop'
  | 'loopTiming'
  | 'storeTime'
  | 'storeType'
  | 'sourceDomain'
  | 'sourceAddress'
  | 'limiterTime'
  | 'limiterList'
  | 'invertLimiter'
  | 'storeComparison'
  | 'generatedUsingValueList'
  | 'note'

export const FIELD_HELP: Record<HelpKey, string> = {
  enabled: 'Disabled units are kept in the layer but skipped when it is applied.',
  locked:
    'Locked units are left alone by batch tools: reroll, Disable 50%, Invert Disabled and Sanitize duplicates.',
  bigEndian:
    'Reverse the value bytes before writing, for big-endian memory. Set from the domain when the unit is generated.',
  domain: 'The memory domain the unit writes to.',
  address: 'Byte offset in the domain where the write starts.',
  precision: 'How many bytes the unit writes. Changing it pads or truncates the value on the left.',
  source:
    'VALUE writes a fixed byte string. STORE copies bytes read from the source domain and address.',
  value: 'The bytes a VALUE unit writes, shown most significant byte first (hex).',
  tilt: 'A signed number added to the value before it is written, wrapping around at the precision. For STORE units it is added to the sampled bytes.',
  executeFrame: 'Delay: frames to wait after the layer is applied before the unit starts writing.',
  lifetime:
    'How many frames the unit keeps writing once it starts. 0 means forever (a freeze or cheat).',
  loop: 'When the lifetime runs out, apply the unit again (after Loop Timing or Execute Frame).',
  loopTiming: 'Delay in frames before each looped re-apply. Empty uses Execute Frame instead.',
  storeTime:
    'When a STORE unit samples its source: IMMEDIATE samples when the layer is applied, PREEXECUTE when the unit starts writing.',
  storeType:
    'ONCE samples the source once and keeps writing that value. CONTINUOUS samples every frame, so the target follows the source live.',
  sourceDomain: 'The domain a STORE unit reads its bytes from.',
  sourceAddress: 'Byte offset in the source domain that a STORE unit reads from.',
  limiterTime:
    'When the limiter list is checked. rtcv-ish checks it at generation time; NONE disables the limiter.',
  limiterList:
    'List of values the memory at the target must match (or, inverted, must not match) for the unit to be kept.',
  invertLimiter: 'Keep the unit only if the memory does not match the limiter list.',
  storeComparison:
    'Which side of a STORE unit the limiter checks: the target address, the source address, or both.',
  generatedUsingValueList:
    'The value came from a value list, so a reroll draws a new value from that list instead of a random one.',
  note: 'Free text for your own notes. It does not affect the corruption.',
}
