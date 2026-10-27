import { describe, expect, it } from 'vitest'
import { extendedKeySequence, type KeyLike } from './extkeys'

const key = (k: string, mods: Partial<KeyLike> = {}): KeyLike => ({
  type: 'keydown',
  key: k,
  ctrlKey: false,
  altKey: false,
  shiftKey: false,
  metaKey: false,
  ...mods,
})

describe('extendedKeySequence', () => {
  it('leaves legacy chords to xterm.js', () => {
    expect(extendedKeySequence(key('x', { ctrlKey: true }))).toBeNull()
    expect(extendedKeySequence(key('x', { ctrlKey: true, altKey: true }))).toBeNull()
    expect(extendedKeySequence(key('/', { ctrlKey: true }))).toBeNull()
    expect(extendedKeySequence(key('_', { ctrlKey: true, shiftKey: true }))).toBeNull()
    expect(extendedKeySequence(key(' ', { ctrlKey: true }))).toBeNull()
    expect(extendedKeySequence(key('a'))).toBeNull()
    expect(extendedKeySequence(key('Enter'))).toBeNull()
    expect(extendedKeySequence(key('x', { altKey: true }))).toBeNull()
  })
  it('encodes chords legacy cannot express as CSI u', () => {
    expect(extendedKeySequence(key(';', { ctrlKey: true }))).toBe('\x1b[59;5u')
    expect(extendedKeySequence(key('=', { ctrlKey: true }))).toBe('\x1b[61;5u')
    expect(extendedKeySequence(key('1', { ctrlKey: true }))).toBe('\x1b[49;5u')
    expect(extendedKeySequence(key('?', { ctrlKey: true, shiftKey: true }))).toBe('\x1b[63;5u')
    expect(extendedKeySequence(key('>', { ctrlKey: true, shiftKey: true }))).toBe('\x1b[62;5u')
    expect(extendedKeySequence(key('%', { ctrlKey: true, altKey: true, shiftKey: true }))).toBe(
      '\x1b[37;7u',
    )
    expect(extendedKeySequence(key('A', { ctrlKey: true, shiftKey: true }))).toBe('\x1b[97;6u')
    expect(extendedKeySequence(key('Enter', { ctrlKey: true }))).toBe('\x1b[13;5u')
    expect(extendedKeySequence(key('Enter', { shiftKey: true }))).toBe('\x1b[13;2u')
    expect(extendedKeySequence(key('Backspace', { ctrlKey: true }))).toBe('\x1b[127;5u')
  })
  it('keeps Ctrl in Ctrl+Alt chords on symbols legacy encodes without it', () => {
    // C-M-_ (undo-redo) is Ctrl+Alt+Shift+Minus; e.key is the shifted character.
    expect(extendedKeySequence(key('_', { ctrlKey: true, altKey: true, shiftKey: true }))).toBe(
      '\x1b[95;7u',
    )
    expect(extendedKeySequence(key('/', { ctrlKey: true, altKey: true }))).toBe('\x1b[47;7u')
    expect(extendedKeySequence(key('\\', { ctrlKey: true, altKey: true }))).toBe('\x1b[92;7u')
    expect(extendedKeySequence(key(' ', { ctrlKey: true, altKey: true }))).toBeNull()
  })
  it('ignores key releases, Cmd and AltGr', () => {
    expect(extendedKeySequence({ ...key(';', { ctrlKey: true }), type: 'keyup' })).toBeNull()
    expect(extendedKeySequence(key(';', { ctrlKey: true, metaKey: true }))).toBeNull()
    expect(
      extendedKeySequence(key('@', { ctrlKey: true, altKey: true, altGraph: true })),
    ).toBeNull()
  })
})
