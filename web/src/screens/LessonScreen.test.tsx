import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import WS from 'vitest-websocket-mock'
import type { CurriculumIndex, Event, LessonChange, LessonSnapshot } from '../protocol.gen'
import { DEFAULT_TABLES } from '../advice/defaults'
import { disconnectSession } from '../api/session/connection'
import { useSessionStore } from '../store/session'
import { useUiStore } from '../store/ui'
import indexJson from '../../dev/fixtures/index.json'
import { LESSONS } from '../../dev/lessons.ts'
import { App } from '../app'

// xterm.js and noVNC need a real canvas; the panes are stubbed (their hosts are tested in the browser).
vi.mock('../components/screen/TerminalPane', () => ({
  default: () => <div data-testid="terminal" />,
}))
vi.mock('../components/screen/VncPane', () => ({ default: () => <div data-testid="vnc" /> }))
vi.mock('../components/screen/hosts', () => ({
  getTerminalHost: () => ({ reattach: vi.fn(), focus: vi.fn() }),
  getVncHost: () => ({ ensure: vi.fn(), focus: vi.fn() }),
}))

const LESSON = '02-buffers-and-files/03-opening-files'
const EVENTS_URL = 'ws://localhost:3000/s/local/events'

const rpcCalls: { op: string; body: unknown }[] = []
const index = { ...(indexJson as unknown as CurriculumIndex), tables: DEFAULT_TABLES }

const server = setupServer(
  http.get('/api/v1/me', () =>
    HttpResponse.json({
      user: { id: 'local', displayName: 'Local learner', kind: 'local' },
      mode: 'local',
      authMode: 'none',
      features: { gui: true, remoteBox: false, stuckReports: true, authorMode: false },
    }),
  ),
  http.post('/api/v1/sessions', () =>
    HttpResponse.json({ id: 'local', state: 'ready', createdAt: 1, gui: true }, { status: 201 }),
  ),
  http.get('/api/v1/curriculum', () => HttpResponse.json(index)),
  http.get('/api/v1/curriculum/lessons/*', ({ request }) => {
    const id = decodeURIComponent(
      new URL(request.url).pathname.replace('/api/v1/curriculum/lessons/', ''),
    )
    const b = LESSONS[id]
    return b
      ? HttpResponse.json(b)
      : HttpResponse.json(
          { status: 404, code: 'not_found', message: 'no such lesson' },
          { status: 404 },
        )
  }),
  http.get('/api/v1/progress', () => HttpResponse.json({ lessons: {}, updatedAt: 1 })),
  http.post('/s/local/rpc/:op', async ({ params, request }) => {
    const body = await request.json()
    rpcCalls.push({ op: String(params['op']), body })
    if (params['op'] === 'scenario') {
      return HttpResponse.json({
        ok: true,
        result: {
          step: { lessonId: LESSON, stepId: 'open' },
          index: 0,
          count: 4,
          modules: [],
          restarting: false,
        },
      })
    }
    if (params['op'] === 'describe')
      return HttpResponse.json({ ok: true, result: { cmd: '', doc: '' } })
    if (params['op'] === 'demo')
      return HttpResponse.json({ ok: true, result: { restored: false, attempt: 1 } })
    return HttpResponse.json({ ok: true, result: {} })
  }),
)

// The mock events server must exist before MSW patches WebSocket: MSW then
// passes the (unhandled) events connection through to it.
const ws = new WS(EVENTS_URL, { jsonProtocol: true })

beforeAll(() => server.listen({ onUnhandledFrame: 'bypass' }))
afterAll(() => {
  server.close()
  WS.clean()
})

beforeEach(() => {
  rpcCalls.length = 0
  useSessionStore.getState().reset()
  useUiStore.setState({ hintLevels: {}, toasts: [], demo: null, lessonTab: {} })
  window.history.pushState({}, '', `/lesson/${LESSON}`)
})
afterEach(() => {
  disconnectSession()
  server.resetHandlers()
})

function renderApp() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<App queryClient={qc} />)
}

function send(ev: Event) {
  act(() => ws.send(ev))
}

const STEPS = ['open', 'edit', 'save', 'oops']

/** The lesson snapshot with step of LESSON active (null: none). */
const at = (step: string | null, over: Partial<LessonSnapshot> = {}): LessonSnapshot => ({
  rev: 1,
  lessonId: LESSON,
  step: step ? { id: step, index: STEPS.indexOf(step), count: 4 } : null,
  phase: step ? 'active' : 'idle',
  attempt: step ? 1 : 0,
  outcomes: {},
  ...over,
})

/** A lesson event; its rev is its seq. */
const lessonEv = (
  seq: number,
  change: LessonChange,
  lesson: LessonSnapshot,
  stepId?: string,
): Event => ({
  t: 'lesson',
  seq,
  change,
  ...(stepId ? { stepId } : {}),
  lesson: { ...lesson, rev: seq },
})

const hello: Event = {
  t: 'hello',
  seq: 1,
  protocol: 2,
  sessionId: 'local',
  mode: 'terminal',
  emacs: { version: '31.1', uptimeMs: 1000 },
  frames: { terminal: true, gui: false },
  modules: ['base', 'agent'],
  replayed: false,
  lesson: { rev: 1, step: null, phase: 'idle', attempt: 0, outcomes: {} },
}

const state = (over: Record<string, unknown> = {}): Event => ({
  t: 'state',
  seq: 2,
  buf: 'README.md',
  file: '/work/hello/README.md',
  mode: 'markdown-mode',
  modeName: 'Markdown',
  minors: [],
  modified: false,
  readonly: false,
  narrowed: false,
  overwrite: false,
  line: 1,
  col: 0,
  point: 1,
  markActive: false,
  windows: [
    {
      id: 3,
      buf: 'README.md',
      mode: 'markdown-mode',
      selected: true,
      dedicated: false,
      side: null,
    },
  ],
  layout: 3,
  frame: 'terminal',
  frames: 1,
  tabs: { count: 1, current: 'main' },
  minibuffer: null,
  isearch: false,
  recursive: 0,
  macro: false,
  prefixArg: null,
  readOnlyReason: null,
  diagAtPoint: null,
  waiting: null,
  ...over,
})

/** Opens the lesson with step open loaded; tab do switches to the steps. */
async function openLesson(tab: 'read' | 'do' = 'do') {
  renderApp()
  await waitFor(() => expect(ws.server.clients().length).toBe(1))
  send(hello)
  send(state())
  await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
  expect(rpcCalls.find((c) => c.op === 'scenario')?.body).toEqual({ lessonId: LESSON })
  send(lessonEv(3, 'loaded', at('open'), 'open'))
  await screen.findByRole('heading', { level: 1, name: 'Opening files' })
  if (tab === 'do') await userEvent.click(screen.getByRole('button', { name: 'Do 1/4' }))
}

describe('LessonScreen against a fake events stream', () => {
  it('loads the scenario once the session says hello and renders the steps', async () => {
    await openLesson('read')
    // An unstarted lesson opens in Read: the prose with its directives.
    expect(screen.getByRole('complementary', { name: 'Coming from VS Code.' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /try it/ }).length).toBeGreaterThan(0)
    expect(screen.queryByRole('list', { name: 'Steps' })).toBeNull()
    await userEvent.click(screen.getByRole('button', { name: 'Do 1/4' }))
    const steps = screen.getByRole('list', { name: 'Steps' })
    const items = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)
    expect(items).toHaveLength(4)
    expect(items[0]).toHaveAttribute('aria-current', 'step')
    expect(within(items[0] as HTMLElement).getByText('Open src/main.rs')).toBeInTheDocument()
  })

  it('Do shows one expanded step: the others show their goal until clicked', async () => {
    await openLesson()
    const steps = screen.getByRole('list', { name: 'Steps' })
    const items = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps) as HTMLElement[]
    expect(within(items[0] as HTMLElement).getByText('the Find file prompt was used')).toBeVisible()
    const later = items[1] as HTMLElement
    expect(later.querySelector('ul')).not.toBeVisible()
    await userEvent.click(within(later).getByRole('button', { name: 'Change the greeting' }))
    expect(later.querySelector('ul')).toBeVisible()
  })

  it('a completion while reading changes neither the tab nor the scroll', async () => {
    await openLesson('read')
    const scrolled = vi.fn()
    Element.prototype.scrollIntoView = scrolled
    const done = { outcomes: { open: 'done' as const } }
    send(lessonEv(6, 'complete', at('open', done), 'open'))
    send(lessonEv(7, 'loaded', at('edit', { ...done, attempt: 2 }), 'edit'))
    expect(screen.getByText('Step done: Open src/main.rs')).toBeInTheDocument()
    await new Promise((r) => setTimeout(r, 800))
    expect(screen.getByRole('button', { name: 'Read' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: 'Do 2/4' })).toBeInTheDocument()
    expect(screen.queryByRole('list', { name: 'Steps' })).toBeNull()
    expect(scrolled).not.toHaveBeenCalled()
  })

  it('reveals hints level by level and records them', async () => {
    const user = userEvent.setup()
    await openLesson()
    send({
      t: 'check',
      seq: 4,
      lessonId: LESSON,
      stepId: 'open',
      attempt: 1,
      conditions: [
        { id: 'open/1', ok: false },
        { id: 'open/2', ok: false },
      ],
      complete: false,
    })
    await user.click(screen.getByRole('button', { name: 'Show a hint' }))
    const note = await screen.findByRole('note')
    expect(note).toHaveTextContent('hint for: the Find file prompt was used')
    expect(note).toHaveTextContent('C-x then C-f.')
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'hint', body: { stepId: 'open', level: 1 } }),
    )

    await user.click(screen.getByRole('button', { name: 'Show the exact keys' }))
    expect(screen.getByRole('note')).toHaveTextContent('C-x C-f "src/ma" TAB RET')
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'hint', body: { stepId: 'open', level: 2 } }),
    )
    expect(screen.queryByRole('button', { name: 'Show the exact keys' })).toBeNull()

    // The first condition is met: the hint moves to the next one.
    send({
      t: 'check',
      seq: 5,
      lessonId: LESSON,
      stepId: 'open',
      attempt: 1,
      conditions: [
        { id: 'open/1', ok: true },
        { id: 'open/2', ok: false },
      ],
      complete: false,
    })
    expect(screen.getByRole('note')).toHaveTextContent('hint for: main.rs is the current buffer')
  })

  it('marks a completed step done and moves to the next', async () => {
    await openLesson()
    const done = { outcomes: { open: 'done' as const } }
    send(lessonEv(6, 'complete', at('open', done), 'open'))
    send(lessonEv(7, 'loaded', at('edit', { ...done, attempt: 2 }), 'edit'))
    const steps = screen.getByRole('list', { name: 'Steps' })
    const items = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)
    expect(items[0]).toHaveTextContent(/^Done:/)
    expect(items[1]).toHaveAttribute('aria-current', 'step')
    // The finished step is highlighted for a moment and announced.
    expect(items[0]).toHaveClass('justDone')
    expect(screen.getByText('Step done: Open src/main.rs')).toBeInTheDocument()
    await waitFor(() => expect(items[0]).not.toHaveClass('justDone'), { timeout: 2000 })
  })

  it('shows the ticker, Where am I and the cheat sheet from events', async () => {
    await openLesson()
    send({ t: 'prefix', seq: 8, keys: 'C-x', source: 'tap' })
    expect(screen.getByText('waiting for the next key')).toBeInTheDocument()
    send({
      t: 'key',
      seq: 9,
      keys: 'C-x C-f',
      cmd: 'find-file',
      buf: 'README.md',
      mode: 'markdown-mode',
      frame: 'terminal',
    })
    send(
      state({
        seq: 10,
        minibuffer: { depth: 1, prompt: 'Find file: ', contents: '~/hello/', kind: 'prompt' },
      }),
    )
    expect(screen.getByText('find-file')).toBeInTheDocument()
    expect(screen.getByText('Open a file (asks for a path)')).toBeInTheDocument()
    expect(screen.getByText('A prompt is open: Find file:')).toBeInTheDocument()
    expect(screen.getByText('prompt open')).toBeInTheDocument()
    expect(screen.getByRole('cell', { name: 'preview' })).toBeInTheDocument()
  })

  it('shows a habit card once and lets the learner dismiss it', async () => {
    const user = userEvent.setup()
    await openLesson()
    send({
      t: 'key',
      seq: 11,
      keys: 'C-z',
      cmd: 'suspend-frame',
      buf: 'README.md',
      mode: 'markdown-mode',
      frame: 'terminal',
    })
    expect(screen.getByText(/Coming from VS Code\?/)).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: 'Dismiss' })[0] as HTMLElement)
    expect(screen.queryByText(/Coming from VS Code\?/)).toBeNull()
  })

  it('overlays: frame closed, restarting, busy banner', async () => {
    await openLesson()
    send({ t: 'emacs', seq: 12, event: 'frame-closed', frame: 'terminal', reason: 'C-x C-c' })
    expect(screen.getByRole('alert', { name: 'The Emacs frame closed.' })).toBeInTheDocument()
    send({ t: 'emacs', seq: 13, event: 'frame-opened', frame: 'terminal' })
    expect(screen.queryByRole('alert', { name: 'The Emacs frame closed.' })).toBeNull()
    send({ t: 'emacs', seq: 14, event: 'restarting' })
    expect(screen.getByRole('alert', { name: 'Emacs is restarting.' })).toBeInTheDocument()
    send({ t: 'emacs', seq: 15, event: 'daemon-restarted' })
    expect(screen.queryByRole('alert', { name: 'Emacs is restarting.' })).toBeNull()
    send({ t: 'emacs', seq: 16, event: 'unresponsive' })
    expect(screen.getByText('Emacs is busy.')).toBeInTheDocument()
  })

  it('Watch it is a demonstration: demo start, a caption, Stop, demo end', async () => {
    const user = userEvent.setup()
    await openLesson()
    await user.click(screen.getByRole('button', { name: 'Watch it' }))
    const caption = await screen.findByRole('status', { name: 'Demo caption' })
    expect(caption).toHaveTextContent('Watching: Open src/main.rs')
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'demo', body: { stepId: 'open', phase: 'start' } }),
    )
    await user.click(within(caption).getByRole('button', { name: 'Stop' }))
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'demo', body: { stepId: 'open', phase: 'end' } }),
    )
    expect(screen.queryByRole('status', { name: 'Demo caption' })).toBeNull()
    // The ticker is back, at the same place.
    expect(screen.getByLabelText(/^Key ticker/)).toBeInTheDocument()
  })

  it('a demonstrated step stays active and offers Do it yourself', async () => {
    const user = userEvent.setup()
    await openLesson()
    send(
      lessonEv(
        20,
        'demonstrated',
        at('open', { phase: 'watching', outcomes: { open: 'demonstrated' } }),
        'open',
      ),
    )
    const steps = screen.getByRole('list', { name: 'Steps' })
    const first = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)[0] as HTMLElement
    expect(first).toHaveAttribute('aria-current', 'step')
    expect(within(first).getByText('You watched this step. Now do it yourself.')).toBeVisible()
    await user.click(within(first).getByRole('button', { name: 'Do it yourself' }))
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'restore', body: {} }))
    send(lessonEv(21, 'reset', at('open', { attempt: 2 }), 'open'))
    expect(within(first).queryByText('You watched this step. Now do it yourself.')).toBeNull()
  })

  it('Reset this step asks first', async () => {
    const user = userEvent.setup()
    await openLesson()
    await user.click(screen.getByRole('button', { name: 'Reset this step' }))
    const dialog = await screen.findByRole('dialog', { name: 'Reset this step?' })
    await user.click(within(dialog).getByRole('button', { name: 'Cancel' }))
    expect(rpcCalls.some((c) => c.op === 'restore')).toBe(false)
    await user.click(screen.getByRole('button', { name: 'Reset this step' }))
    await user.click(
      within(await screen.findByRole('dialog', { name: 'Reset this step?' })).getByRole('button', {
        name: 'Reset',
      }),
    )
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'restore', body: {} }))
  })

  it('a progress reset from another page is not undone here: Load this lesson here', async () => {
    const user = userEvent.setup()
    await openLesson()
    send(lessonEv(40, 'complete', at('open', { outcomes: { open: 'done' } }), 'open'))
    send(lessonEv(41, 'loaded', at('edit', { attempt: 2, outcomes: { open: 'done' } }), 'edit'))
    rpcCalls.length = 0
    // Another page resets the lesson's progress: sessiond forgets the lesson.
    send(lessonEv(42, 'dropped', { ...at(null), lessonId: '' }, 'edit'))
    const button = await screen.findByRole('button', { name: 'Load this lesson here' })
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    await user.click(button)
    await waitFor(() =>
      expect(rpcCalls.filter((c) => c.op === 'scenario').map((c) => c.body)).toEqual([
        { lessonId: LESSON },
      ]),
    )
  })

  it('after a stuck report the learner can skip the step', async () => {
    const user = userEvent.setup()
    server.use(
      http.post('/api/v1/stuck', () => HttpResponse.json({ id: '1' }, { status: 201 })),
      http.post('/s/local/rpc/skip', async ({ request }) => {
        rpcCalls.push({ op: 'skip', body: await request.json() })
        return HttpResponse.json({ ok: true, result: { next: 'edit', demoCompleted: true } })
      }),
    )
    await openLesson()
    await user.click(screen.getByRole('button', { name: /I'm stuck/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Report that you are stuck' })
    await user.click(within(dialog).getByRole('button', { name: 'Send report' }))
    expect(await within(dialog).findByText(/Emacs can do this step for you/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Skip this step' }))
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'skip', body: { stepId: 'open' } }))
    expect(await screen.findByText('Skipped: Open src/main.rs')).toBeInTheDocument()
    const skipped = { outcomes: { open: 'skipped' as const } }
    send(lessonEv(30, 'skipped', at('open', skipped), 'open'))
    send(lessonEv(31, 'loaded', at('edit', { ...skipped, attempt: 2 }), 'edit'))
    const steps = screen.getByRole('list', { name: 'Steps' })
    const items = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)
    expect(items[0]).toHaveTextContent(/^Skipped:/)
    expect(items[0]).toHaveTextContent('skipped')
    expect(items[1]).toHaveAttribute('aria-current', 'step')
  })

  it('a habit card stays quiet for a key the current step teaches', async () => {
    await openLesson()
    const cs = (seq: number): Event => ({
      t: 'key',
      seq,
      keys: 'C-s',
      cmd: 'isearch-forward',
      buf: 'main.rs',
      mode: 'rust-ts-mode',
      frame: 'terminal',
    })
    // Step "edit" says "C-s finds text as you type": no "C-s is not save".
    send(lessonEv(40, 'loaded', at('edit', { attempt: 2 }), 'edit'))
    send(cs(41))
    expect(screen.queryByText(/is not save/)).toBeNull()
    // Step "open" does not mention C-s (the lesson only has C-x C-s): the card fires.
    send(lessonEv(42, 'loaded', at('open', { attempt: 3 }), 'open'))
    send(cs(43))
    expect(screen.getByText(/is not save/)).toBeInTheDocument()
  })

  it('a demo stops when the attempt changes: no keys go into another step', async () => {
    const user = userEvent.setup()
    await openLesson()
    await user.click(screen.getByRole('button', { name: 'Watch it' }))
    await screen.findByRole('status', { name: 'Demo caption' })
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'demo', body: { stepId: 'open', phase: 'start' } }),
    )
    send(lessonEv(50, 'loaded', at('save', { attempt: 2 }), 'save'))
    expect(screen.queryByRole('status', { name: 'Demo caption' })).toBeNull()
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'demo', body: { stepId: 'open', phase: 'end' } }),
    )
    const sent = rpcCalls.filter((c) => c.op === 'keys').length
    await new Promise((r) => setTimeout(r, 1500))
    expect(rpcCalls.filter((c) => c.op === 'keys').length).toBe(sent)
  })

  it('Go to this step covers the screen until the step is active', async () => {
    const user = userEvent.setup()
    let release: () => void = () => undefined
    server.use(
      http.post('/s/local/rpc/scenario', async ({ request }) => {
        const body = await request.json()
        rpcCalls.push({ op: 'scenario', body })
        if ((body as { stepId?: string }).stepId === 'save')
          await new Promise<void>((r) => (release = r))
        return HttpResponse.json({ ok: true, result: { step: body, modules: [] } })
      }),
    )
    await openLesson()
    await user.click(screen.getAllByRole('button', { name: 'Go to this step' })[1] as HTMLElement)
    expect(
      await screen.findByRole('status', { name: 'Going to step 3: Save it…' }),
    ).toBeInTheDocument()
    expect(useSessionStore.getState().quietHabitsUntil).toBe(Number.POSITIVE_INFINITY)
    act(() => release())
    await waitFor(() => expect(screen.queryByRole('status', { name: /^Going to step/ })).toBeNull())
    expect(useSessionStore.getState().quietHabitsUntil).toBeLessThan(Number.POSITIVE_INFINITY)
  })

  it('after a reload during Go to this step the screen stays covered until the step is active', async () => {
    const busy = { op: 'load', lessonId: LESSON, stepId: 'save', startedAt: 1 }
    renderApp()
    await waitFor(() => expect(ws.server.clients().length).toBe(1))
    send({ ...hello, lesson: at('open', { busy }) } as Event)
    send(state())
    expect(
      await screen.findByRole('status', { name: 'Going to step 3: Save it…' }),
    ).toBeInTheDocument()
    send(lessonEv(3, 'loaded', at('save', { attempt: 2, busy }), 'save'))
    send(lessonEv(4, 'busy', at('save', { attempt: 2 })))
    await waitFor(() => expect(screen.queryByRole('status', { name: /^Going to step/ })).toBeNull())
    // The snapshot ended it: no status poll, and the step this page did
    // not ask for is not loaded again.
    expect(rpcCalls.some((c) => c.op === 'status' || c.op === 'scenario')).toBe(false)
  })

  it('after a reload a demonstrated step still offers Do it yourself', async () => {
    renderApp()
    await waitFor(() => expect(ws.server.clients().length).toBe(1))
    const watching = at('open', { phase: 'watching', outcomes: { open: 'demonstrated' } })
    send({ ...hello, lesson: watching } as Event)
    send(state())
    // A demonstrated step counts as started: the lesson opens in Do.
    const steps = await screen.findByRole('list', { name: 'Steps' })
    const first = await waitFor(() => {
      const li = steps.querySelector('[aria-current="step"]')
      expect(li).not.toBeNull()
      return li as HTMLElement
    })
    expect(within(first).getByRole('button', { name: 'Do it yourself' })).toBeInTheDocument()
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    // Once the step starts again (Do it yourself, a reset) it is the learner's.
    send(lessonEv(3, 'reset', at('open', { attempt: 2 }), 'open'))
    expect(within(first).queryByRole('button', { name: 'Do it yourself' })).toBeNull()
  })

  it('a finished lesson is not loaded again: its ticks and Lesson complete show', async () => {
    const done = { status: 'done', hintsUsed: 0, resets: 0 }
    server.use(
      http.get('/api/v1/progress', () =>
        HttpResponse.json({
          lessons: {
            [LESSON]: {
              status: 'done',
              counts: { done: 3, demonstrated: 0, skipped: 1, total: 4 },
              steps: {
                open: done,
                edit: done,
                save: { ...done, status: 'skipped' },
                oops: done,
              },
            },
          },
          updatedAt: 2,
        }),
      ),
    )
    renderApp()
    await waitFor(() => expect(ws.server.clients().length).toBe(1))
    send(hello)
    send(state())
    expect(await screen.findByText(/Lesson complete\./)).toBeInTheDocument()
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    // Start this lesson over: the reset, then a load with reset, whatever
    // the snapshot shows meanwhile.
    let reset: unknown = null
    server.use(
      http.post('/api/v1/progress/reset', async ({ request }) => {
        reset = await request.json()
        return new HttpResponse(null, { status: 204 })
      }),
    )
    await userEvent.click(screen.getByRole('button', { name: 'Start this lesson over' }))
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'scenario', body: { lessonId: LESSON, reset: true } }),
    )
    expect(reset).toEqual({ lessonId: LESSON })
  })

  it('when another page loads another lesson, this one offers to load it again', async () => {
    const user = userEvent.setup()
    await openLesson()
    rpcCalls.length = 0
    send(
      lessonEv(
        60,
        'loaded',
        { ...at(null), lessonId: '00-orientation/02-getting-unstuck', phase: 'active' },
        'prompt',
      ),
    )
    const button = await screen.findByRole('button', { name: 'Load this lesson here' })
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    await user.click(button)
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'scenario', body: { lessonId: LESSON } }),
    )
  })

  it('an incompatible session loads nothing and says how to recover', async () => {
    renderApp()
    await waitFor(() => expect(ws.server.clients().length).toBe(1))
    send({ ...hello, protocol: 1 })
    send(state())
    expect(
      await screen.findByRole('alert', {
        name: 'This session runs an older version of the playground.',
      }),
    ).toHaveTextContent('Restart the stack (make local).')
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
  })

  it('Rescue calls the rpc and names what was done', async () => {
    const user = userEvent.setup()
    server.use(
      http.post('/s/local/rpc/rescue', () =>
        HttpResponse.json({ ok: true, result: { did: ['cancelled the prompt'] } }),
      ),
    )
    await openLesson()
    await user.click(screen.getAllByRole('button', { name: 'Rescue me' })[0] as HTMLElement)
    expect(await screen.findByText('Rescue: cancelled the prompt.')).toBeInTheDocument()
  })
})
