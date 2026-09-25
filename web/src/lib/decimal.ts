/** Largest unsigned value for a precision in bytes. */
export function maxForPrecision(precision: number): bigint {
  return (1n << BigInt(8 * precision)) - 1n
}

export function isUnsignedDecimal(s: string): boolean {
  return /^[0-9]+$/.test(s)
}

export function isSignedDecimal(s: string): boolean {
  return /^-?[0-9]+$/.test(s)
}

/** Returns an error message, or '' when min/max are valid for the precision. */
export function validateRange(min: string, max: string, precision: number): string {
  if (!isUnsignedDecimal(min)) return 'min must be an unsigned decimal'
  if (!isUnsignedDecimal(max)) return 'max must be an unsigned decimal'
  const lim = maxForPrecision(precision)
  if (BigInt(max) > lim) return `max must be <= ${lim}`
  if (BigInt(min) > BigInt(max)) return 'min must be <= max'
  return ''
}

/** Canonical decimal form ("007" -> "7"). */
export function normalizeDecimal(s: string): string {
  return BigInt(s).toString()
}
