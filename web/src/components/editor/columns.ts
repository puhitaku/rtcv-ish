import type { Unit } from '@/api/types'
import type { HelpKey } from '@/lib/fieldHelp'
import { valueDisplay } from '@/lib/hex'
import { fieldBases, formatNum, type NumField } from '@/lib/numBase'

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
  | 'generatedUsingValueList'
  | 'note'

export interface Column {
  id: ColumnId
  label: string
  /** Width cap in characters; longer cells are cut with an ellipsis. */
  maxCh: number
  mono?: boolean
  bool?: boolean
  /** Numeric field shown in the user's chosen base. */
  num?: NumField
  text: (u: Unit) => string
}

const yn = (b: boolean) => (b ? 'yes' : 'no')
/** Formats a numeric field in its current base (addresses padded in hex). */
const num = (f: NumField, v: number | string, width = 0) =>
  formatNum(v, fieldBases[f], fieldBases[f] === 'hex' ? width : 0)

/** RTCV Blast Editor columns, in RTCV's order. */
export const COLUMNS: Column[] = [
  { id: 'enabled', label: 'Enabled', maxCh: 8, bool: true, text: (u) => yn(u.enabled) },
  { id: 'locked', label: 'Locked', maxCh: 7, bool: true, text: (u) => yn(u.locked) },
  { id: 'domain', label: 'Domain', maxCh: 12, text: (u) => u.domain },
  {
    id: 'address',
    label: 'Address',
    maxCh: 12,
    mono: true,
    num: 'address',
    text: (u) => num('address', u.address, 8),
  },
  {
    id: 'precision',
    label: 'Precision',
    maxCh: 9,
    mono: true,
    num: 'precision',
    text: (u) => num('precision', u.precision),
  },
  {
    id: 'value',
    label: 'Value',
    maxCh: 16,
    mono: true,
    text: (u) => (u.source === 'value' ? valueDisplay(u.value) : ''),
  },
  {
    id: 'tilt',
    label: 'Tilt',
    maxCh: 12,
    mono: true,
    num: 'tilt',
    text: (u) => num('tilt', u.tilt || '0'),
  },
  { id: 'source', label: 'Source', maxCh: 7, text: (u) => u.source.toUpperCase() },
  {
    id: 'executeFrame',
    label: 'Execute Frame',
    maxCh: 9,
    mono: true,
    num: 'executeFrame',
    text: (u) => num('executeFrame', u.executeFrame),
  },
  {
    id: 'lifetime',
    label: 'Lifetime',
    maxCh: 9,
    mono: true,
    num: 'lifetime',
    text: (u) => num('lifetime', u.lifetime),
  },
  { id: 'loop', label: 'Loop', maxCh: 5, bool: true, text: (u) => yn(u.loop) },
  {
    id: 'loopTiming',
    label: 'Loop Timing',
    maxCh: 9,
    mono: true,
    num: 'loopTiming',
    text: (u) => (u.loopTiming == null ? '' : num('loopTiming', u.loopTiming)),
  },
  {
    id: 'limiterTime',
    label: 'Limiter Time',
    maxCh: 10,
    text: (u) => u.limiterTime.toUpperCase(),
  },
  { id: 'limiterList', label: 'Limiter List', maxCh: 16, text: (u) => u.limiterList },
  {
    id: 'invertLimiter',
    label: 'Invert Limiter',
    maxCh: 8,
    bool: true,
    text: (u) => yn(u.invertLimiter),
  },
  { id: 'storeTime', label: 'Store Time', maxCh: 11, text: (u) => u.storeTime.toUpperCase() },
  { id: 'storeType', label: 'Store Type', maxCh: 11, text: (u) => u.storeType.toUpperCase() },
  { id: 'sourceDomain', label: 'Source Domain', maxCh: 12, text: (u) => u.sourceDomain },
  {
    id: 'sourceAddress',
    label: 'Source Address',
    maxCh: 12,
    mono: true,
    num: 'sourceAddress',
    text: (u) => (u.source === 'store' ? num('sourceAddress', u.sourceAddress, 8) : ''),
  },
  { id: 'bigEndian', label: 'Big Endian', maxCh: 7, bool: true, text: (u) => yn(u.bigEndian) },
  {
    id: 'generatedUsingValueList',
    label: 'From Value List',
    maxCh: 8,
    bool: true,
    text: (u) => yn(u.generatedUsingValueList),
  },
  { id: 'note', label: 'Note', maxCh: 24, text: (u) => u.note },
]

/** Help key of a column (every column id is a help key). */
export const columnHelp = (id: ColumnId): HelpKey => id

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
