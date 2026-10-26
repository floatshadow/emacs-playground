import { beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useSessionStore } from '../../store/session'
import { Markdown } from './Markdown'
import { StepsSlotContext } from './stepsSlot'
import { hasStepsMarker } from './directives'

const rpcMock = vi.hoisted(() => vi.fn(async () => ({})))
vi.mock('../../api/session/rpc', () => ({
  rpc: rpcMock,
  sessionBase: (id: string) => `/s/${id}`,
  wsUrl: (p: string) => `ws://localhost${p}`,
}))

beforeEach(() => {
  useSessionStore.getState().reset()
  rpcMock.mockClear()
})

describe('lesson Markdown directives (CURRICULUM.md Part B.2)', () => {
  it('renders backticked key sequences as keycaps and other code as code', () => {
    const { container } = render(
      <Markdown markdown={'Press `C-x C-f`, type `ma`, open `src/main.rs`, press `TAB`.'} />,
    )
    const kbds = [...container.querySelectorAll('kbd')].map((k) => k.textContent)
    expect(kbds).toEqual(['C-x C-f', 'TAB'])
    const codes = [...container.querySelectorAll('code')].map((c) => c.textContent)
    expect(codes).toEqual(['ma', 'src/main.rs'])
  })

  it(':kbd renders a keycap and :try sends the keys to Emacs', async () => {
    const { container } = render(
      <Markdown markdown={'Split with :kbd[C-x 2], or :try[C-x C-f].'} />,
    )
    expect([...container.querySelectorAll('kbd')].map((k) => k.textContent)).toEqual([
      'C-x 2',
      'C-x C-f',
    ])
    await userEvent.click(screen.getByRole('button', { name: /try it/ }))
    expect(rpcMock).toHaveBeenCalledWith('keys', { keys: 'C-x C-f' })
  })

  it(':jargon shows the glossary definition as a tooltip', () => {
    render(<Markdown markdown={'Files you open are :jargon[buffers].'} />)
    const term = screen.getByRole('button', { name: 'buffers' })
    expect(term).toHaveAccessibleDescription(/Text held in Emacs/)
  })

  it(':jargon without a glossary entry is plain text', () => {
    const { container } = render(<Markdown markdown={'A :jargon[flibbertigibbet] here.'} />)
    expect(container.textContent).toBe('A flibbertigibbet here.')
    expect(container.querySelector('button')).toBeNull()
  })

  it(':::vscode and :::aside render boxes', () => {
    render(
      <Markdown
        markdown={
          ':::vscode\nCtrl+P becomes `C-x C-f`.\n:::\n\n:::aside{title="Why?"}\nBecause.\n:::\n'
        }
      />,
    )
    expect(screen.getByRole('complementary', { name: 'Coming from VS Code.' })).toHaveTextContent(
      'Ctrl+P becomes C-x C-f.',
    )
    expect(screen.getByText('Why?')).toBeInTheDocument()
  })

  it(':::gui-only shows only in the GUI frame', () => {
    const md = 'Before.\n\n:::gui-only\nOnly with pixels.\n:::\n'
    const { rerender } = render(<Markdown markdown={md} />)
    expect(screen.queryByText('Only with pixels.')).toBeNull()
    act(() => useSessionStore.getState().applyEvent({ t: 'mode', mode: 'gui' }))
    rerender(<Markdown markdown={md + ' '} />)
    expect(screen.getByText('Only with pixels.')).toBeInTheDocument()
  })

  it('::steps places the step list', () => {
    const md = 'Intro.\n\n::steps\n\nOutro.'
    expect(hasStepsMarker(md)).toBe(true)
    expect(hasStepsMarker('No marker here ::steps inline')).toBe(false)
    const { container } = render(
      <StepsSlotContext.Provider value={<ol data-testid="steps" />}>
        <Markdown markdown={md} />
      </StepsSlotContext.Provider>,
    )
    const html = container.innerHTML
    expect(html.indexOf('Intro.')).toBeLessThan(html.indexOf('data-testid="steps"'))
    expect(html.indexOf('data-testid="steps"')).toBeLessThan(html.indexOf('Outro.'))
  })

  it('unknown directives keep their source text', () => {
    const { container } = render(<Markdown markdown={'Keys:none new, and :foo[bar] stays.'} />)
    expect(container.textContent).toBe('Keys:none new, and :foo[bar] stays.')
  })

  it('without directives (preview Markdown) the syntax is not interpreted', () => {
    const { container } = render(<Markdown markdown={':kbd[C-x] and `C-x`'} directives={false} />)
    expect(container.textContent).toBe(':kbd[C-x] and C-x')
    expect(container.querySelectorAll('kbd')).toHaveLength(1)
  })

  it('rewrites lesson image paths to the assets endpoint', () => {
    const { container } = render(
      <Markdown
        markdown={'![screen](assets/mode-line.png)'}
        lessonId="00-orientation/01-reading-the-screen"
      />,
    )
    expect(container.querySelector('img')?.getAttribute('src')).toBe(
      '/api/v1/curriculum/assets/00-orientation/01-reading-the-screen/mode-line.png',
    )
  })

  it('renders GFM tables', () => {
    const { container } = render(
      <Markdown markdown={'| Keys | Does |\n|---|---|\n| `C-x 2` | split |\n'} />,
    )
    expect(container.querySelector('table td kbd')?.textContent).toBe('C-x 2')
  })
})
