import { describe, expect, it } from 'vitest'
import { captionItems, describeDemo, parseDemo, planDemo, WATCH_PACING } from './demo'

describe('planDemo (DemoPlayer pacing)', () => {
  it('sends one chord per 170 ms and one character per 70 ms', () => {
    const plan = planDemo(parseDemo('C-x C-f "ma" ~400 RET'))
    expect(plan).toEqual([
      { kind: 'keys', keys: 'C-x', delayMs: 170 },
      { kind: 'keys', keys: 'C-f', delayMs: 170 },
      { kind: 'text', text: 'm', delayMs: 70 },
      { kind: 'text', text: 'a', delayMs: 70 },
      { kind: 'pause', delayMs: 400 },
      { kind: 'keys', keys: 'RET', delayMs: 170 },
    ])
  })
  it('plays "Watch it" at human pace: 600 ms per chord, 120 ms per character', () => {
    const plan = planDemo(parseDemo('C-s ~400 "zz" C-g'), WATCH_PACING)
    expect(plan.map((a) => a.delayMs)).toEqual([600, 400, 120, 120, 600])
  })
  it('captions the keys sent so far, typed text as one growing string', () => {
    const plan = planDemo(parseDemo('C-x C-f ~300 "src" TAB'))
    expect(captionItems(plan, 0)).toEqual([])
    expect(captionItems(plan, 5)).toEqual([
      { kind: 'keys', keys: 'C-x' },
      { kind: 'keys', keys: 'C-f' },
      { kind: 'text', text: 'sr' },
    ])
    expect(captionItems(plan, plan.length).at(-1)).toEqual({ kind: 'keys', keys: 'TAB' })
  })
  it('turns newlines and tabs in typed text into RET and TAB', () => {
    expect(
      planDemo(parseDemo('"a\\n\\tb"')).map((a) =>
        a.kind === 'keys' ? a.keys : a.kind === 'text' ? a.text : '~',
      ),
    ).toEqual(['a', 'RET', 'TAB', 'b'])
  })
  it('describes a demo for the "exact keys" hint', () => {
    expect(describeDemo(parseDemo('C-x C-f ~300 "src/ma" TAB RET'))).toBe(
      'C-x C-f "src/ma" TAB RET',
    )
  })
})
