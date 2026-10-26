import { describe, expect, it } from 'vitest'
import { describeDemo, parseDemo, planDemo } from './demo'

describe('parseDemo (port of TestParseDemo)', () => {
  it('tokenises keys, quoted text and pauses', () => {
    expect(parseDemo('C-x C-f "ma" TAB ~400 RET "say \\"hi\\"\\n"')).toEqual([
      { kind: 'key', key: 'C-x' },
      { kind: 'key', key: 'C-f' },
      { kind: 'text', text: 'ma' },
      { kind: 'key', key: 'TAB' },
      { kind: 'pause', pauseMs: 400 },
      { kind: 'key', key: 'RET' },
      { kind: 'text', text: 'say "hi"\n' },
    ])
  })
  it('normalises keys (F3, modifier order)', () => {
    expect(parseDemo('F3 M-C-x')).toEqual([
      { kind: 'key', key: '<f3>' },
      { kind: 'key', key: 'C-M-x' },
    ])
  })
  for (const bad of ['', '"unterminated', '~x', 'C-x "a"b', 'Ctrl-x', '""', '"\\q"', '~70000']) {
    it(`rejects ${JSON.stringify(bad)}`, () => expect(() => parseDemo(bad)).toThrow())
  }
})

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
