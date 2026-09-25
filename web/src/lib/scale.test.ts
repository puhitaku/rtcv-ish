import { describe, expect, it } from 'vitest'
import { SLIDER_STEPS, sliderToValue, valueToSlider } from './scale'
import { maxForPrecision, validateRange } from './decimal'

describe('non-linear slider scale', () => {
  it('maps the ends to min and max', () => {
    expect(sliderToValue(0, 1, 65535)).toBe(1)
    expect(sliderToValue(SLIDER_STEPS, 1, 65535)).toBe(65535)
  })

  it('gives low values most of the travel', () => {
    expect(sliderToValue(SLIDER_STEPS / 2, 1, 65535)).toBeLessThan(65535 / 4)
  })

  it('round-trips and clamps values above the slider max', () => {
    for (const v of [1, 2, 10, 100, 5000, 65535]) {
      expect(Math.abs(sliderToValue(valueToSlider(v, 1, 65535), 1, 65535) - v)).toBeLessThanOrEqual(
        Math.max(1, v * 0.01),
      )
    }
    expect(valueToSlider(1e9, 1, 65535)).toBe(SLIDER_STEPS)
  })
})

describe('decimal ranges', () => {
  it('validates min/max per precision', () => {
    expect(maxForPrecision(8)).toBe(18446744073709551615n)
    expect(validateRange('0', '255', 1)).toBe('')
    expect(validateRange('0', '256', 1)).toMatch(/max/)
    expect(validateRange('5', '4', 1)).toMatch(/min/)
    expect(validateRange('-1', '4', 1)).toMatch(/min/)
    expect(validateRange('0', '18446744073709551615', 8)).toBe('')
  })
})
