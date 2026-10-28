import { describe, expect, it } from 'vitest'
import { formatLap } from './time'

describe('formatLap', () => {
  it('reads like a stopwatch', () => {
    expect(formatLap(0)).toBe('0:00')
    expect(formatLap(65_400)).toBe('1:05')
    expect(formatLap(754_000)).toBe('12:34')
    expect(formatLap(3_723_000)).toBe('1:02:03')
    expect(formatLap(-5)).toBe('0:00')
  })
})
