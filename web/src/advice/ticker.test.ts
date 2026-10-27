import { afterEach, describe, expect, it } from 'vitest'
import { record, type Recorder } from '../test/recorder'
import { DEFAULT_GLOSS } from './defaults'
import {
  isMouseOnly,
  missingGlosses,
  selectTicker,
  tickerGap,
  tickerOnKey,
  tickerOnPrefix,
  TICKER_MAX,
  type TickerEntry,
} from './ticker'

let rec: Recorder | null = null
afterEach(() => rec?.dispose())

const key = (keys: string, cmd: string, extra: Record<string, unknown> = {}) =>
  ({
    t: 'key',
    keys,
    cmd,
    buf: 'main.rs',
    mode: 'rust-ts-mode',
    frame: 'terminal',
    ...extra,
  }) as const

describe('isMouseOnly', () => {
  it('matches clicks, presses, drags, wheels and area-prefixed clicks', () => {
    for (const k of [
      '<mouse-1>',
      '<down-mouse-1>',
      '<drag-mouse-1>',
      '<double-mouse-1>',
      '<wheel-down>',
      '<C-wheel-up>',
      '<S-down-mouse-3>',
      '<mode-line> <mouse-1>',
      '<vertical-line> <down-mouse-1>',
    ])
      expect(isMouseOnly(k), k).toBe(true)
  })
  it('keeps keys and key sequences that only end in a click', () => {
    for (const k of ['C-x', 'C-x <mouse-1>', '<f1>', '<mode-line>', 'RET', '<down>', ''])
      expect(isMouseOnly(k), k).toBe(false)
  })
})

describe('ticker reducer', () => {
  it('a prefix adds a pending entry, the completed command replaces it', () => {
    let t: TickerEntry[] = []
    t = tickerOnPrefix(t, { t: 'prefix', keys: 'C-x', source: 'tap' }, 1)
    expect(t.map((e) => e.kind)).toEqual(['prefix'])
    t = tickerOnPrefix(t, { t: 'prefix', keys: 'C-x n', source: 'tap' }, 2)
    expect(t).toHaveLength(1)
    expect(t[0]?.keys).toEqual(['C-x', 'n'])
    t = tickerOnKey(t, key('C-x n w', 'widen'), 3)
    expect(t.map((e) => [e.kind, e.cmd])).toEqual([['ok', 'widen']])
  })

  it('an empty prefix clears the pending entry', () => {
    let t = tickerOnPrefix([], { t: 'prefix', keys: 'C-x', source: 'tap' }, 1)
    t = tickerOnPrefix(t, { t: 'prefix', keys: '', source: 'tap' }, 2)
    expect(t).toEqual([])
  })

  it('classifies unbound and quit commands', () => {
    let t = tickerOnKey([], key('C-x C-y', 'undefined'), 1)
    t = tickerOnKey(t, key('C-g', 'keyboard-quit'), 2)
    t = tickerOnKey(t, key('M-x', 'compile', { mx: true }), 3)
    expect(t.map((e) => e.kind)).toEqual(['unbound', 'quit', 'ok'])
    expect(t[2]?.mx).toBe(true)
    expect(t[2]?.keys).toEqual(['M-x'])
  })

  it('keeps at most 40 entries and shows six, newest first', () => {
    let t: TickerEntry[] = []
    for (let i = 0; i < 50; i++) t = tickerOnKey(t, key('C-f', 'forward-char'), i)
    expect(t).toHaveLength(TICKER_MAX)
    t = tickerOnKey(t, key('C-x C-f', 'find-file'), 99)
    const v = selectTicker(t, DEFAULT_GLOSS, {})
    expect(v).toHaveLength(6)
    expect(v[0]?.last).toBe(true)
    expect(v[0]?.label).toBe('find-file')
    expect(v[0]?.gloss).toBe('Open a file (asks for a path)')
  })

  it('glosses come from the table, then from describe', () => {
    let t = tickerOnKey([], key('C-c C-k', 'my-command'), 1)
    expect(missingGlosses(t, DEFAULT_GLOSS, {}).map((e) => e.cmd)).toEqual(['my-command'])
    expect(selectTicker(t, DEFAULT_GLOSS, { 'my-command': 'Does my thing' })[0]?.gloss).toBe(
      'Does my thing',
    )
    t = tickerOnKey(t, key('C-x C-y', 'undefined'), 2)
    expect(selectTicker(t, DEFAULT_GLOSS, {})[0]?.gloss).toBe('Nothing is bound to this key here')
  })

  it('collapses a run of typing into one entry with the last 12 characters', () => {
    let t: TickerEntry[] = tickerOnKey([], key('C-e', 'move-end-of-line'), 1)
    for (const [i, ch] of [...'hello world!!'].entries())
      t = tickerOnKey(t, key(ch === ' ' ? 'SPC' : ch, 'self-insert-command'), 2 + i)
    expect(t).toHaveLength(2)
    const run = t[1]
    expect(run?.count).toBe(13)
    expect(run?.typed).toBe('ello world!!')
    // The entry grows in place: the same id, no new card per letter.
    const id = run?.id
    t = tickerOnKey(t, key('a', 'self-insert-command'), 30)
    expect(t[1]?.id).toBe(id)
    expect(t[1]?.count).toBe(14)
    // A command ends the run; the next typing starts a new one.
    t = tickerOnKey(t, key('C-a', 'move-beginning-of-line'), 31)
    t = tickerOnKey(t, key('x', 'self-insert-command'), 32)
    expect(t.map((e) => e.count ?? 0)).toEqual([0, 14, 0, 1])
    // Search typing collapses too, separately from buffer typing.
    t = tickerOnKey(t, key('z', 'isearch-printing-char'), 33)
    expect(t.at(-1)?.typed).toBe('z')
    expect(t).toHaveLength(5)
  })

  it('a C-g that cancels a prefix shows as cancelled, not unbound', () => {
    const t = tickerOnKey([], key('C-x C-g', 'undefined'), 1)
    expect(t[0]?.kind).toBe('quit')
    const v = selectTicker(t, DEFAULT_GLOSS, {})
    expect(v[0]?.label).toBe('cancelled')
    expect(missingGlosses(t, DEFAULT_GLOSS, {})).toEqual([])
  })

  it('folds a prefix argument into the command it is for', () => {
    let t = tickerOnKey([], key('C-u', 'universal-argument'), 1)
    t = tickerOnKey(t, key('4', 'digit-argument'), 2)
    t = tickerOnKey(t, key('0', 'digit-argument'), 3)
    t = tickerOnKey(t, key('=', 'self-insert-command', { prefixArg: '40' }), 4)
    expect(t.map((e) => [e.keys.join(' '), e.cmd])).toEqual([['C-u 4 0 =', 'self-insert-command']])
    expect(t[0]?.typed).toBeUndefined()
    // Keys that already include the argument are not doubled.
    t = tickerOnKey(t, key('C-u', 'universal-argument'), 5)
    t = tickerOnKey(t, key('C-u C-SPC', 'set-mark-command', { prefixArg: '(4)' }), 6)
    expect(t.at(-1)?.keys).toEqual(['C-u', 'C-SPC'])
    // The next typing starts a run of its own.
    t = tickerOnKey(t, key('a', 'self-insert-command'), 7)
    expect(t.at(-1)?.typed).toBe('a')
  })

  it("marks replayed keys and keeps them out of the learner's typing runs", () => {
    let t = tickerOnKey([], key('a', 'self-insert-command'), 1, true)
    t = tickerOnKey(t, key('b', 'self-insert-command'), 2)
    expect(t.map((e) => [e.typed, !!e.replayed])).toEqual([
      ['a', true],
      ['b', false],
    ])
  })

  it('marks gaps once', () => {
    let t = tickerGap([], 1)
    t = tickerGap(t, 2)
    expect(t.map((e) => e.kind)).toEqual(['gap'])
  })

  it('a recorded C-x C-f session reads like the demo ticker', () => {
    rec = record(['hello/README.md'])
    rec.keys('C-x C-f').type('src/ma').keys('TAB RET')
    const s = rec.replay()
    const v = selectTicker(s.ticker, DEFAULT_GLOSS, {})
    expect(v.map((x) => x.label).slice(0, 3)).toEqual([
      'exit-minibuffer',
      'minibuffer-complete',
      'self-insert-command',
    ])
    expect(s.ticker.some((e) => e.kind === 'prefix')).toBe(false)
    expect(s.ticker.find((e) => e.cmd === 'find-file')?.keys).toEqual(['C-x', 'C-f'])
  })
})
