import { describe, expect, it } from 'vitest'
import { emitTerminalInput, isLearnerKeydown, onTerminalInput, resetFocusRegistry } from './focus'

describe('isLearnerKeydown', () => {
  it('counts keys the learner types', () => {
    for (const key of ['a', 'x', 'Enter', 'Escape', 'ArrowUp', 'F1', ' ', 'Process', 'Dead']) {
      expect(isLearnerKeydown({ type: 'keydown', key })).toBe(true)
    }
  })

  it('ignores lone modifiers and other event types', () => {
    for (const key of ['Shift', 'Control', 'Alt', 'Meta', 'AltGraph', 'CapsLock']) {
      expect(isLearnerKeydown({ type: 'keydown', key })).toBe(false)
    }
    expect(isLearnerKeydown({ type: 'keyup', key: 'a' })).toBe(false)
    expect(isLearnerKeydown({ type: 'keypress', key: 'a' })).toBe(false)
  })
})

describe('terminal input subscribers', () => {
  it('notifies until unsubscribed', () => {
    resetFocusRegistry()
    let n = 0
    const off = onTerminalInput(() => n++)
    emitTerminalInput()
    off()
    emitTerminalInput()
    expect(n).toBe(1)
  })
})
