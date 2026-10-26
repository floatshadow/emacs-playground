import { afterEach, describe, expect, it } from 'vitest'
import { record, type Recorder } from '../test/recorder'
import { selectNudges } from '../store/selectors'
import { initialSessionData, reduce, type SessionData } from '../store/session'
import type { Event } from '../protocol.gen'
import { HABIT_TTL_COMMANDS } from './habits'
import { patternNudges } from './patterns'
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

  it('cards expire after 12 commands', () => {
    rec = record()
    rec.keys('C-s')
    let s = rec.replay()
    expect(ids(s, rec.now())).toContain('habit:C-s')
    for (let i = 0; i < HABIT_TTL_COMMANDS + 1; i++) {
      s = reduce(
        s,
        {
          t: 'key',
          keys: 'C-f',
          cmd: 'forward-char',
          buf: 'main.rs',
          mode: 'rust-ts-mode',
          frame: 'terminal',
        },
        rec.now(),
      )
    }
    expect(ids(s, rec.now())).not.toContain('habit:C-s')
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
