import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { useUiStore } from '../../store/ui'
import { Workspace } from './Workspace'

function renderWork() {
  render(
    <Workspace
      left={<aside data-testid="left" />}
      center={<main className="stage">stage</main>}
      right={<div>rail</div>}
    />,
  )
  // jsdom lays nothing out: the side pane is 400 px wide.
  vi.spyOn(screen.getByTestId('left'), 'getBoundingClientRect').mockReturnValue({
    width: 400,
  } as DOMRect)
  return screen.getByRole('separator', { name: /Resize the lesson pane/ })
}

const widthVar = () =>
  (document.querySelector('[style]') as HTMLElement | null)?.style.getPropertyValue(
    '--lesson-width',
  ) ?? ''

beforeEach(() => {
  localStorage.clear()
  useUiStore.getState().setPrefs({ lessonWidth: null })
  HTMLElement.prototype.setPointerCapture = vi.fn()
})
afterEach(() => vi.restoreAllMocks())

describe('Workspace splitter', () => {
  it('drags the side pane wider and remembers the width', () => {
    const sep = renderWork()
    fireEvent.pointerDown(sep, { clientX: 400, pointerId: 1 })
    fireEvent.pointerMove(sep, { clientX: 520, pointerId: 1 })
    fireEvent.pointerUp(sep, { pointerId: 1 })
    fireEvent.lostPointerCapture(sep, { pointerId: 1 })
    fireEvent.pointerMove(sep, { clientX: 600, pointerId: 1 })
    expect(useUiStore.getState().lessonWidth).toBe(520)
    expect(widthVar()).toBe('520px')
    expect(JSON.parse(localStorage.getItem('pg.prefs') ?? '{}').lessonWidth).toBe(520)
  })

  it('a cancelled drag ends with the lost capture', () => {
    const sep = renderWork()
    fireEvent.pointerDown(sep, { clientX: 400, pointerId: 1 })
    fireEvent.pointerMove(sep, { clientX: 450, pointerId: 1 })
    fireEvent.pointerCancel(sep, { pointerId: 1 })
    fireEvent.lostPointerCapture(sep, { pointerId: 1 })
    fireEvent.pointerMove(sep, { clientX: 600, pointerId: 1 })
    expect(useUiStore.getState().lessonWidth).toBe(450)
  })

  it('arrow keys move it 24 px and Home resets it', () => {
    const sep = renderWork()
    fireEvent.keyDown(sep, { key: 'ArrowRight' })
    expect(useUiStore.getState().lessonWidth).toBe(424)
    fireEvent.keyDown(sep, { key: 'ArrowLeft' })
    expect(useUiStore.getState().lessonWidth).toBe(376)
    fireEvent.keyDown(sep, { key: 'Home' })
    expect(useUiStore.getState().lessonWidth).toBeNull()
    expect(widthVar()).toBe('')
  })
})
