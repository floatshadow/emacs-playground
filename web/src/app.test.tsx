import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { App } from './app'
import { CloseReplaced, FrameTerminal, type KeyEvent } from './protocol.gen'

describe('App placeholder', () => {
  it('renders the brand', () => {
    render(<App />)
    expect(screen.getByRole('heading', { name: 'Emacs Playground' })).toBeInTheDocument()
  })

  it('uses the generated protocol types', () => {
    const ev: KeyEvent = {
      t: 'key',
      keys: 'C-x C-f',
      cmd: 'find-file',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: FrameTerminal,
    }
    expect(ev.frame).toBe('terminal')
    expect(CloseReplaced).toBe(4000)
  })
})
