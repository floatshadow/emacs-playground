import { describe, expect, it } from 'vitest'
import { cheatRows } from './cheats'
import { DEFAULT_CHEATS } from './defaults'

describe('cheat rows', () => {
  const cheats = {
    common: [['C-g', 'cancel']] as [string, string][],
    'magit-status-mode': [['s', 'stage']] as [string, string][],
    'smerge-mode': [['C-c ^ u', 'keep upper']] as [string, string][],
  }

  it('lists the major mode, then the common rows', () => {
    expect(cheatRows(cheats, 'magit-status-mode', [], 'mode keys')).toEqual([
      ['s', 'stage'],
      ['C-g', 'cancel'],
    ])
  })

  it('puts the rows of enabled minor modes first', () => {
    expect(cheatRows(cheats, 'rust-ts-mode', ['eglot--managed-mode', 'smerge-mode'], 'x')).toEqual([
      ['C-c ^ u', 'keep upper'],
      ['C-h m', 'x'],
      ['C-g', 'cancel'],
    ])
  })

  it('a mode the table does not know points at C-h m', () => {
    expect(cheatRows(cheats, 'dape-repl-mode', [], 'keys of this mode')).toEqual([
      ['C-h m', 'keys of this mode'],
      ['C-g', 'cancel'],
    ])
  })

  it('the built-in table covers the special modes of the later parts', () => {
    for (const m of [
      'magit-status-mode',
      'org-mode',
      'org-agenda-mode',
      'eshell-mode',
      'eat-mode',
      'vterm-mode',
      'wdired-mode',
      'smerge-mode',
      'ediff-mode',
      'calc-mode',
      'pdf-view-mode',
      'inferior-emacs-lisp-mode',
    ])
      expect(DEFAULT_CHEATS[m]?.length, m).toBeGreaterThan(0)
  })
})
