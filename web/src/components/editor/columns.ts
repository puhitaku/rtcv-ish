import type { Unit } from '@/api/types'
import { hex, valueDisplay } from '@/lib/hex'

export type ColumnId =
  | 'enabled'
  | 'locked'
  | 'domain'
  | 'address'
  | 'precision'
  | 'value'
  | 'tilt'
  | 'source'
  | 'executeFrame'
  | 'lifetime'
  | 'loop'
  | 'loopTiming'
  | 'limiterTime'
  | 'limiterList'
  | 'invertLimiter'
  | 'storeTime'
  | 'storeType'
  | 'sourceDomain'
  | 'sourceAddress'
  | 'bigEndian'
  | 'note'

export interface Column {
  id: ColumnId
  label: string
  mono?: boolean
  bool?: boolean
  text: (u: Unit) => string
}

const yn = (b: boolean) => (b ? 'yes' : 'no')

/** RTCV Blast Editor columns, in RTCV's order. */
export const COLUMNS: Column[] = [
  { id: 'enabled', label: 'Enabled', bool: true, text: (u) => yn(u.enabled) },
  { id: 'locked', label: 'Locked', bool: true, text: (u) => yn(u.locked) },
  { id: 'domain', label: 'Domain', text: (u) => u.domain },
  { id: 'address', label: 'Address', mono: true, text: (u) => hex(u.address, 8) },
  { id: 'precision', label: 'Precision', mono: true, text: (u) => String(u.precision) },
  {
    id: 'value',
    label: 'Value',
    mono: true,
    text: (u) => (u.source === 'value' ? valueDisplay(u.value) : ''),
  },
  { id: 'tilt', label: 'Tilt', mono: true, text: (u) => u.tilt || '0' },
  { id: 'source', label: 'Source', text: (u) => u.source.toUpperCase() },
  { id: 'executeFrame', label: 'Execute Frame', mono: true, text: (u) => String(u.executeFrame) },
  { id: 'lifetime', label: 'Lifetime', mono: true, text: (u) => String(u.lifetime) },
  { id: 'loop', label: 'Loop', bool: true, text: (u) => yn(u.loop) },
  {
    id: 'loopTiming',
    label: 'Loop Timing',
    mono: true,
    text: (u) => (u.loopTiming == null ? '' : String(u.loopTiming)),
  },
  { id: 'limiterTime', label: 'Limiter Time', text: (u) => u.limiterTime.toUpperCase() },
  { id: 'limiterList', label: 'Limiter List', text: (u) => u.limiterList },
  { id: 'invertLimiter', label: 'Invert Limiter', bool: true, text: (u) => yn(u.invertLimiter) },
  { id: 'storeTime', label: 'Store Time', text: (u) => u.storeTime.toUpperCase() },
  { id: 'storeType', label: 'Store Type', text: (u) => u.storeType.toUpperCase() },
  { id: 'sourceDomain', label: 'Source Domain', text: (u) => u.sourceDomain },
  {
    id: 'sourceAddress',
    label: 'Source Address',
    mono: true,
    text: (u) => (u.source === 'store' ? hex(u.sourceAddress, 8) : ''),
  },
  { id: 'bigEndian', label: 'Big Endian', bool: true, text: (u) => yn(u.bigEndian) },
  { id: 'note', label: 'Note', text: (u) => u.note },
]

export const DEFAULT_COLUMNS: ColumnId[] = [
  'enabled',
  'locked',
  'domain',
  'address',
  'precision',
  'value',
  'source',
  'executeFrame',
  'lifetime',
  'loop',
  'storeType',
  'sourceDomain',
  'sourceAddress',
  'note',
]
