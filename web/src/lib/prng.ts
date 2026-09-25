/** A random source returning floats in [0, 1). */
export type Rand = () => number

/** mulberry32: small seeded PRNG for deterministic tests. */
export function mulberry32(seed: number): Rand {
  let a = seed >>> 0
  return () => {
    a = (a + 0x6d2b79f5) >>> 0
    let t = a
    t = Math.imul(t ^ (t >>> 15), t | 1)
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61)
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

/** Random permutation of 0..n-1 (Fisher-Yates). */
export function perm(n: number, rand: Rand): number[] {
  const p = Array.from({ length: n }, (_, i) => i)
  for (let i = n - 1; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1))
    ;[p[i], p[j]] = [p[j]!, p[i]!]
  }
  return p
}
