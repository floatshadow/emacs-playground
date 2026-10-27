import { afterEach, describe, expect, it } from 'vitest'
import { record, type Recorder } from '../test/recorder'
import { selectNudges } from '../store/selectors'
import { initialSessionData, reduce, type SessionData } from '../store/session'
import type { Event } from '../protocol.gen'
import { proseKeys } from './mentions'
import { patternNudges } from './patterns'
import type { TickerEntry } from './ticker'
import { calmState } from './fixtures'

let rec: Recorder | null = null
afterEach(() => rec?.dispose())

const ids = (s: SessionData, now: number) => selectNudges(s, now).map((n) => n.id)

describe('habit cards (layer 2)', () => {
  it('C-z gets one card per session, titled with what VS Code users meant', () => {
    rec = record()
    rec.keys('C-z')
    let s = rec.replay()
    const cards = selectNudges(s, rec.now())
    expect(cards[0]?.id).toBe('habit:C-z')
    expect(cards[0]?.title).toBe('Coming from VS Code? C-z is not undo.')
    expect(cards[0]?.body).toMatch(/Undo is C-\/ \(also C-x u\)/)
    // A second C-z does not add another card.
    const before = s.cards.length
    s = reduce(
      s,
      {
        t: 'key',
        keys: 'C-z',
        cmd: 'suspend-frame',
        buf: 'main.rs',
        mode: 'rust-ts-mode',
        frame: 'terminal',
      },
      rec.now(),
    )
    expect(s.cards.length).toBe(before)
  })

  it('demo playback raises no habit cards and does not use them up', () => {
    const key: Event = {
      t: 'key',
      keys: 'C-s',
      cmd: 'isearch-forward',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    }
    // While a demo plays (and in its short tail) the C-s habit stays quiet...
    let s: SessionData = { ...initialSessionData(), quietHabitsUntil: 5000 }
    s = reduce(s, key, 4000)
    expect(s.cards).toEqual([])
    expect(s.habitsSeen.has('C-s')).toBe(false)
    // ...and fires when the learner presses C-s afterwards.
    s = reduce(s, key, 6000)
    expect(s.cards.map((c) => c.id)).toEqual(['habit:C-s'])
  })

  it('cards stay until dismissed: no command TTL (B9)', () => {
    rec = record()
    rec.keys('C-s')
    let s = rec.replay()
    expect(ids(s, rec.now())).toContain('habit:C-s')
    for (let i = 0; i < 30; i++) {
      s = reduce(
        s,
        {
          t: 'key',
          keys: 'C-n',
          cmd: 'next-line',
          buf: 'main.rs',
          mode: 'rust-ts-mode',
          frame: 'terminal',
        },
        rec.now(),
      )
    }
    expect(ids(s, rec.now())).toContain('habit:C-s')
  })

  it('dismissing one card leaves the others; the newest come first', () => {
    const key = (k: string, cmd: string): Event => ({
      t: 'key',
      keys: k,
      cmd,
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    })
    let s = [
      key('C-f', 'forward-char'),
      key('C-b', 'backward-char'),
      key('C-v', 'scroll-up-command'),
    ].reduce((acc, ev, i) => reduce(acc, ev, 1000 + i), initialSessionData())
    expect(ids(s, 2000)).toEqual(['habit:C-v', 'habit:C-b', 'habit:C-f'])
    s = { ...s, dismissed: new Set(['habit:C-f']) }
    expect(ids(s, 2000)).toEqual(['habit:C-v', 'habit:C-b'])
  })

  it('a key the lesson mentions raises no card and is not used up (contract d)', () => {
    const cs: Event = {
      t: 'key',
      keys: 'C-s',
      cmd: 'isearch-forward',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    }
    // 1.4 teaches C-s: no "C-s is not save".
    let s: SessionData = {
      ...initialSessionData(),
      activeStep: { lessonId: 'L', stepId: 'a' },
      mentions: { lessonId: 'L', lesson: ['C-s', 'C-r'], steps: {} },
    }
    s = reduce(s, cs, 1000)
    expect(s.cards).toEqual([])
    expect(s.habitsSeen.has('C-s')).toBe(false)
    // 0.1 mentions C-x C-s but not C-s: the card fires.
    s = { ...s, mentions: { lessonId: 'L', lesson: ['C-x C-s'], steps: {} } }
    s = reduce(s, cs, 2000)
    expect(s.cards.map((c) => c.id)).toEqual(['habit:C-s'])
  })

  it('a step mentions keys too: its goal, detail and hints', () => {
    const cb: Event = {
      t: 'key',
      keys: 'C-b',
      cmd: 'backward-char',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    }
    const mentions = { lessonId: 'L', lesson: [], steps: { a: ['C-b'] } }
    let s: SessionData = {
      ...initialSessionData(),
      activeStep: { lessonId: 'L', stepId: 'a' },
      mentions,
    }
    s = reduce(s, cb, 1000)
    expect(s.cards).toEqual([])
    s = { ...s, activeStep: { lessonId: 'L', stepId: 'b' } }
    s = reduce(s, cb, 2000)
    expect(s.cards.map((c) => c.id)).toEqual(['habit:C-b'])
  })

  it('each command of a mentioned span counts: C-a and C-k in 3.5 build (B8, R5)', () => {
    const key = (keys: string, cmd: string): Event => ({
      t: 'key',
      keys,
      cmd,
      buf: ' *Minibuf-1*',
      mode: 'minibuffer-mode',
      frame: 'terminal',
    })
    const detail = '`C-x p c`, clear the offered command (`C-a C-k`), type `sh build.sh`, `RET`.'
    let s: SessionData = {
      ...initialSessionData(),
      activeStep: { lessonId: 'L', stepId: 'build' },
      mentions: { lessonId: 'L', lesson: [], steps: { build: proseKeys(detail) } },
    }
    s = reduce(s, key('C-a', 'move-beginning-of-line'), 1000)
    s = reduce(s, key('C-k', 'kill-line'), 1200)
    expect(s.cards).toEqual([])
    expect(s.habitsSeen.size).toBe(0)
  })

  it('only single-key commands match; M-x invocations and sequences do not', () => {
    rec = record()
    rec.keys('C-x C-f C-g')
    const s = rec.replay()
    expect(s.cards.map((c) => c.key)).not.toContain('C-f')
  })

  it('dismissals last for the session', () => {
    rec = record()
    rec.keys('C-a')
    let s = rec.replay()
    expect(ids(s, rec.now())).toContain('habit:C-a')
    s = { ...s, dismissed: new Set(['habit:C-a']) }
    expect(ids(s, rec.now())).not.toContain('habit:C-a')
  })
})

describe('stuck patterns', () => {
  it('three C-g within 8 s is a calm card', () => {
    rec = record()
    rec.keys('C-g C-g C-g')
    expect(ids(rec.replay(), rec.now())).toContain('panic')
  })

  it('three C-g spread over more than 8 s is not', () => {
    rec = record()
    rec.keys('C-g').wait(5000).keys('C-g').wait(5000).keys('C-g')
    expect(ids(rec.replay(), rec.now())).not.toContain('panic')
  })

  it('the same unbound key twice asks about C-h k', () => {
    rec = record()
    rec.keys('C-x C-y C-x C-y')
    const n = selectNudges(rec.replay(), rec.now())
    expect(n.map((x) => x.id)).toContain('unbound:C-x C-y')
    expect(n.find((x) => x.id === 'unbound:C-x C-y')?.title).toBe('C-x C-y is not bound here.')
  })

  it('typing into a read-only buffer', () => {
    rec = record()
    rec.keys('C-x C-q').type('a')
    const n = selectNudges(rec.replay(), rec.now())
    const ro = n.find((x) => x.id.startsWith('ro:'))
    expect(ro?.title).toBe('You typed into main.rs, which is read-only.')
    expect(ro?.body).toBe('C-x C-q makes this buffer writable.')
  })

  it('typing into a special buffer says q leaves it', () => {
    rec = record()
    rec.keys('C-x C-b C-x o').type('z')
    const ro = selectNudges(rec.replay(), rec.now()).find((x) => x.id.startsWith('ro:'))
    expect(ro?.body).toMatch(/Press q to leave it/)
  })

  it('a prompt open for more than 20 s', () => {
    rec = record()
    rec.keys('C-x C-f')
    const s = rec.replay()
    expect(ids(s, rec.now() + 5000)).not.toContain('mini-stuck')
    expect(ids(s, rec.now() + 21000)).toContain('mini-stuck')
  })

  it('three windows is usually an accident', () => {
    rec = record()
    rec.keys('C-x 2 C-x 2')
    expect(ids(rec.replay(), rec.now())).toContain('many-windows')
  })

  it('unless the lesson teaches splitting (C-x 2 or C-x 3 mentioned)', () => {
    rec = record()
    rec.keys('C-x 2 C-x 3')
    const s = {
      ...rec.replay(),
      mentions: { lessonId: 'L', lesson: ['C-x 2', 'C-x o'], steps: {} },
    }
    expect(ids(s, rec.now())).not.toContain('many-windows')
  })

  it('windows that popped up are no accident: no split, no card (2.7 build output)', () => {
    const three = calmState({
      windows: [
        {
          id: 3,
          buf: 'README.md',
          mode: 'markdown-mode',
          selected: true,
          dedicated: false,
          side: null,
        },
        {
          id: 5,
          buf: '*xref*',
          mode: 'xref--xref-buffer-mode',
          selected: false,
          dedicated: false,
          side: null,
        },
        {
          id: 7,
          buf: '*compilation*',
          mode: 'compilation-mode',
          selected: false,
          dedicated: false,
          side: null,
        },
      ],
    })
    const at = 1000
    let id = 0
    const entry = (keys: string[], cmd: string): TickerEntry => ({
      id: ++id,
      keys,
      cmd,
      kind: 'ok',
      at,
    })
    const ticker = [entry(['C-x', 'p', 'c'], 'project-compile'), entry(['RET'], 'exit-minibuffer')]
    expect(
      patternNudges({ ticker, state: three, minibufferSince: null, now: at }).map((n) => n.id),
    ).not.toContain('many-windows')
    const split = [entry(['C-x', '2'], 'split-window-below'), ...ticker]
    expect(
      patternNudges({ ticker: split, state: three, minibufferSince: null, now: at }).map(
        (n) => n.id,
      ),
    ).toContain('many-windows')
  })

  it('a calm state yields nothing', () => {
    expect(
      patternNudges({ ticker: [], state: calmState(), minibufferSince: null, now: 0 }),
    ).toEqual([])
  })

  it('server nudges merge into the same list', () => {
    rec = record()
    let s = rec.replay()
    s = reduce(
      s,
      {
        t: 'nudge',
        id: 'open/stuck/1',
        kind: 'calm',
        title: 'Still in the prompt?',
        body: 'RET accepts.',
      },
      rec.now(),
    )
    expect(ids(s, rec.now())).toEqual(['open/stuck/1'])
  })
})
