import { afterEach, describe, expect, it } from 'vitest'
import { DEFAULT_EXITS, DEFAULT_TABLES, withDefaultTables } from './defaults'
import { evaluateExits, liveExits, minorLighter, whereAmI } from './exits'
import { apiState, calmState } from './fixtures'
import { record, type Recorder } from '../test/recorder'

let rec: Recorder | null = null
afterEach(() => rec?.dispose())

/** Where am I after a key sequence, through the recorded stream and the reducer. */
function whereAfter(keys: string, open?: string[]) {
  rec = record(open)
  rec.keys(keys)
  const s = rec.replay()
  if (!s.state) throw new Error('no state')
  return whereAmI(s.state, DEFAULT_EXITS, { prefix: s.prefix, overlay: s.emacs.overlay })
}

describe('exits table over real states (the demo whereAmI as data)', () => {
  it('is calm when nothing unusual is going on', () => {
    expect(evaluateExits(calmState(), DEFAULT_EXITS)).toEqual([])
    const w = whereAmI(calmState(), DEFAULT_EXITS)
    expect(w.exits).toEqual([])
    expect(w.windows).toBe(1)
    expect(w.minors).toEqual(['Eglot', 'Corfu', 'Flymake'])
  })

  it('names the special buffer in the other window', () => {
    const rows = evaluateExits(apiState(), DEFAULT_EXITS)
    expect(rows.map((r) => r.id)).toEqual(['other-window-special'])
    expect(rows[0]?.why).toBe('A second window shows *compilation*.')
    expect(rows[0]?.how).toBe('Press q inside it, or C-x 1 here to keep only this window.')
  })

  it('C-x C-b: a second window shows *Buffer List*', () => {
    const w = whereAfter('C-x C-b')
    expect(w.windows).toBe(2)
    expect(w.exits.map((e) => e.why)).toEqual(['A second window shows *Buffer List*.'])
  })

  it('C-x C-f: a prompt is open, with its prompt text', () => {
    const w = whereAfter('C-x C-f')
    expect(w.prompt).toBe('Find file:')
    expect(w.flags).toContain('prompt open')
    expect(w.exits[0]?.why).toBe('A prompt is open: Find file:')
    expect(w.exits[0]?.how).toBe('RET accepts, C-g cancels. TAB completes names.')
  })

  it('C-s: searching', () => {
    const w = whereAfter('C-s')
    expect(w.flags).toContain('searching')
    expect(w.exits.map((e) => e.id)).toContain('isearch')
  })

  it('C-x 2: the frame is split, no special window', () => {
    const w = whereAfter('C-x 2')
    expect(w.exits.map((e) => e.why)).toEqual(['The frame is split into 2 windows.'])
  })

  it('narrowing, overwrite, macros and read-only each show their flag and exit', () => {
    expect(whereAfter('C-x n d').flags).toContain('Narrow')
    expect(whereAfter('<insert>').flags).toContain('Ovwrt')
    expect(whereAfter('<f3>').flags).toContain('Def')
    const ro = whereAfter('C-x C-q')
    expect(ro.flags).toContain('read-only')
    expect(ro.exits.find((e) => e.id === 'read-only-file')?.how).toBe('C-x C-q makes it writable.')
  })

  it('inside *Help*: q closes it, and the frame is split', () => {
    const w = whereAfter('C-h m C-x o')
    expect(w.buffer).toBe('*Help*')
    expect(w.exits.map((e) => e.id)).toEqual(['split', 'special-buffer'])
    expect(w.exits[1]?.why).toBe('You are inside *Help*.')
  })

  it('Dired: you opened a folder', () => {
    rec = record(['hello/src/main.rs'])
    rec.keys('C-x C-f')
    rec.keys('DEL DEL DEL DEL RET')
    const s = rec.replay()
    const w = whereAmI(s.state!, DEFAULT_EXITS)
    expect(w.mode).toBe('dired-mode')
    expect(w.exits.map((e) => e.id)).toContain('dired')
  })

  it('C-u: an argument is pending', () => {
    const w = whereAfter('C-u')
    expect(w.exits.map((e) => e.id)).toContain('prefix-arg')
  })

  it('a pending prefix from the tap is a live row with a flag', () => {
    const w = whereAfter('C-x')
    expect(w.exits[0]?.why).toBe('Prefix C-x is waiting for the next key.')
    expect(w.flags).toContain('prefix C-x')
  })

  it('live rows for a closed frame and a suspended Emacs come first', () => {
    expect(liveExits({ prefix: null, overlay: 'frame-closed' })[0]?.why).toMatch(
      /closed with C-x C-c/,
    )
    expect(liveExits({ prefix: null, overlay: 'suspended' })[0]?.how).toMatch(/fg/)
    expect(liveExits({ prefix: null, overlay: null, busy: true })[0]?.id).toBe('live:busy')
  })

  it('rows with broken predicates never match', () => {
    expect(
      evaluateExits(calmState(), [{ id: 'x', when: { mode: { regex: '(' } }, why: 'w', how: 'h' }]),
    ).toEqual([])
  })

  it('uses index tables when present and defaults for empty ones', () => {
    const t = withDefaultTables({
      exits: [],
      habits: [{ key: 'C-z', vscode: 'undo', say: 'x' }],
      gloss: {},
      cheats: {},
      glossary: {},
    })
    expect(t.exits).toBe(DEFAULT_TABLES.exits)
    expect(t.habits).toHaveLength(1)
  })

  it('derives minor-mode lighters', () => {
    expect(minorLighter('eglot--managed-mode')).toBe('Eglot')
    expect(minorLighter('global-hl-line-mode')).toBe('Hl-line')
    expect(minorLighter('which-key-mode')).toBe('Which-key')
  })

  it('leaves housekeeping minor modes out of the Mode row (B16)', () => {
    const w = whereAmI(
      calmState({ minors: ['ws-butler-mode', 'flymake-mode', 'global-ws-butler-mode'] }),
      DEFAULT_EXITS,
    )
    expect(w.minors).toEqual(['Flymake'])
  })
})

describe('the virtual root session (protocol.ExitSession)', () => {
  const rows = [
    {
      id: 'prefix-pending',
      when: { 'session.prefix': { regex: '.' } },
      why: '{session.prefix} is a prefix.',
      how: 'C-g cancels it.',
    },
    {
      id: 'frame-closed',
      when: { 'session.emacs': 'frame-closed' },
      why: 'The frame was closed.',
      how: 'Wait a moment.',
    },
    {
      id: 'describe-key',
      when: { 'session.echo': { regex: '^Describe the following key' } },
      why: 'C-h k is waiting for a key.',
      how: 'Press the key.',
    },
  ]

  it('evaluates session.* paths and templates', () => {
    const w = whereAmI(calmState(), rows, {
      prefix: 'C-x',
      overlay: null,
      echo: 'Describe the following key, mouse click, or menu item:',
    })
    expect(w.exits.map((e) => e.id)).toEqual(['prefix-pending', 'describe-key'])
    expect(w.exits[0]?.why).toBe('C-x is a prefix.')
  })

  it('lets table rows replace the built-in live rows for the same facts', () => {
    const w = whereAmI(calmState(), rows, { prefix: 'C-x', overlay: 'frame-closed' })
    expect(w.exits.map((e) => e.id)).toEqual(['prefix-pending', 'frame-closed'])
  })

  it('keeps the live rows when the table has no session rows', () => {
    const w = whereAmI(calmState(), [], { prefix: 'C-x', overlay: null })
    expect(w.exits.map((e) => e.id)).toEqual(['live:prefix'])
  })

  it('reports unresponsive as session.emacs', () => {
    const busyRows = [
      { id: 'busy', when: { 'session.emacs': 'unresponsive' }, why: 'busy', how: 'wait' },
    ]
    const w = whereAmI(calmState(), busyRows, { prefix: null, overlay: null, busy: true })
    expect(w.exits.map((e) => e.id)).toEqual(['busy'])
  })
})
