/** Slider positions are 0..SLIDER_STEPS. */
export const SLIDER_STEPS = 1000

/**
 * Non-linear slider scale (like RTCV's MultiTrackBar): low values get most of
 * the travel. value = min + (max - min) * t^3.
 */
export function sliderToValue(pos: number, min: number, max: number): number {
  const t = Math.min(Math.max(pos / SLIDER_STEPS, 0), 1)
  return Math.round(min + (max - min) * t ** 3)
}

export function valueToSlider(value: number, min: number, max: number): number {
  if (value <= min) return 0
  if (value >= max) return SLIDER_STEPS
  return Math.round(Math.cbrt((value - min) / (max - min)) * SLIDER_STEPS)
}
