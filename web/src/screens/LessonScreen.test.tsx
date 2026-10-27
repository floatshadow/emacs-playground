import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import WS from 'vitest-websocket-mock'
import type { CurriculumIndex, Event } from '../protocol.gen'
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
  useUiStore.setState({ hintLevels: {}, toasts: [], demo: null })
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

const hello: Event = {
  t: 'hello',
  seq: 1,
  protocol: 1,
  sessionId: 'local',
  mode: 'terminal',
  emacs: { version: '31.1', uptimeMs: 1000 },
  frames: { terminal: true, gui: false },
  step: null,
  modules: ['base', 'agent'],
  replayed: false,
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

async function openLesson() {
  renderApp()
  await waitFor(() => expect(ws.server.clients().length).toBe(1))
  send(hello)
  send(state())
  await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
  expect(rpcCalls.find((c) => c.op === 'scenario')?.body).toEqual({
    lessonId: LESSON,
    stepId: 'open',
  })
  send({
    t: 'step',
    seq: 3,
    lessonId: LESSON,
    stepId: 'open',
    status: 'loaded',
    index: 0,
    count: 4,
  })
  await screen.findByRole('heading', { level: 1, name: 'Opening files' })
}

describe('LessonScreen against a fake events stream', () => {
  it('loads the scenario once the session says hello and renders the steps', async () => {
    await openLesson()
    const steps = screen.getByRole('list', { name: 'Steps' })
    const items = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)
    expect(items).toHaveLength(4)
    expect(items[0]).toHaveAttribute('aria-current', 'step')
    expect(within(items[0] as HTMLElement).getByText('Open src/main.rs')).toBeInTheDocument()
    // Directives rendered: the VS Code box, a try-it button and a keycap.
    expect(screen.getByRole('complementary', { name: 'Coming from VS Code.' })).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /try it/ }).length).toBeGreaterThan(0)
  })

  it('reveals hints level by level and records them', async () => {
    const user = userEvent.setup()
    await openLesson()
    send({
      t: 'check',
      seq: 4,
      lessonId: LESSON,
      stepId: 'open',
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
    send({
      t: 'step',
      seq: 6,
      lessonId: LESSON,
      stepId: 'open',
      status: 'complete',
      index: 0,
      count: 4,
    })
    send({
      t: 'step',
      seq: 7,
      lessonId: LESSON,
      stepId: 'edit',
      status: 'loaded',
      index: 1,
      count: 4,
    })
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
    send({
      t: 'step',
      seq: 20,
      lessonId: LESSON,
      stepId: 'open',
      status: 'demonstrated',
      index: 0,
      count: 4,
    })
    const steps = screen.getByRole('list', { name: 'Steps' })
    const first = within(steps)
      .getAllByRole('listitem')
      .filter((li) => li.parentElement === steps)[0] as HTMLElement
    expect(first).toHaveAttribute('aria-current', 'step')
    expect(within(first).getByText('You watched this step. Now do it yourself.')).toBeVisible()
    await user.click(within(first).getByRole('button', { name: 'Do it yourself' }))
    await waitFor(() => expect(rpcCalls).toContainEqual({ op: 'restore', body: {} }))
    send({
      t: 'step',
      seq: 21,
      lessonId: LESSON,
      stepId: 'open',
      status: 'reset',
      index: 0,
      count: 4,
    })
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

  it('a progress reset from another page reloads the lesson at its first step', async () => {
    let openDone = false
    server.use(
      http.get('/api/v1/progress', () =>
        HttpResponse.json({
          lessons: openDone
            ? {
                [LESSON]: {
                  status: 'in-progress',
                  counts: { done: 1, demonstrated: 0, skipped: 0, total: 4 },
                  steps: { open: { status: 'done', hintsUsed: 0, resets: 0 } },
                },
              }
            : {},
          updatedAt: 2,
        }),
      ),
    )
    await openLesson()
    openDone = true
    send({
      t: 'step',
      seq: 40,
      lessonId: LESSON,
      stepId: 'open',
      status: 'complete',
      index: 0,
      count: 4,
    })
    send({
      t: 'step',
      seq: 41,
      lessonId: LESSON,
      stepId: 'edit',
      status: 'loaded',
      index: 1,
      count: 4,
    })
    await waitFor(() =>
      expect(
        screen.getByRole('list', { name: 'Steps' }).querySelector('[aria-current="step"]'),
      ).toHaveTextContent(/./),
    )
    rpcCalls.length = 0
    // Another page (or a script) resets the lesson's progress: sessiond
    // drops the active step. This page must not reload the lesson at the
    // step after the one it saw done.
    openDone = false
    send({
      t: 'step',
      seq: 42,
      lessonId: LESSON,
      stepId: 'edit',
      status: 'dropped',
      index: 1,
      count: 4,
    })
    await waitFor(() => expect(rpcCalls.find((c) => c.op === 'scenario')).toBeDefined())
    expect(rpcCalls.filter((c) => c.op === 'scenario').map((c) => c.body)).toEqual([
      { lessonId: LESSON, stepId: 'open' },
    ])
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
    send({
      t: 'step',
      seq: 30,
      lessonId: LESSON,
      stepId: 'open',
      status: 'skipped',
      index: 0,
      count: 4,
    })
    send({
      t: 'step',
      seq: 31,
      lessonId: LESSON,
      stepId: 'edit',
      status: 'loaded',
      index: 1,
      count: 4,
    })
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
    send({
      t: 'step',
      seq: 40,
      lessonId: LESSON,
      stepId: 'edit',
      status: 'loaded',
      index: 1,
      count: 4,
    })
    send(cs(41))
    expect(screen.queryByText(/is not save/)).toBeNull()
    // Step "open" does not mention C-s (the lesson only has C-x C-s): the card fires.
    send({
      t: 'step',
      seq: 42,
      lessonId: LESSON,
      stepId: 'open',
      status: 'loaded',
      index: 0,
      count: 4,
    })
    send(cs(43))
    expect(screen.getByText(/is not save/)).toBeInTheDocument()
  })

  it('a demo stops when the active step changes: no keys go into another step', async () => {
    const user = userEvent.setup()
    await openLesson()
    await user.click(screen.getByRole('button', { name: 'Watch it' }))
    await screen.findByRole('status', { name: 'Demo caption' })
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({ op: 'demo', body: { stepId: 'open', phase: 'start' } }),
    )
    send({
      t: 'step',
      seq: 50,
      lessonId: LESSON,
      stepId: 'save',
      status: 'loaded',
      index: 2,
      count: 4,
    })
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

  it('after a reload a demonstrated step still offers Do it yourself', async () => {
    server.use(
      http.get('/api/v1/progress', () =>
        HttpResponse.json({
          lessons: {
            [LESSON]: {
              status: 'in-progress',
              counts: { done: 0, demonstrated: 1, skipped: 0, total: 4 },
              steps: { open: { status: 'demonstrated', hintsUsed: 0, resets: 0 } },
            },
          },
          updatedAt: 2,
        }),
      ),
    )
    renderApp()
    await waitFor(() => expect(ws.server.clients().length).toBe(1))
    send({ ...hello, step: { lessonId: LESSON, stepId: 'open' } } as Event)
    send(state())
    const steps = await screen.findByRole('list', { name: 'Steps' })
    const first = await waitFor(() => {
      const li = steps.querySelector('[aria-current="step"]')
      expect(li).not.toBeNull()
      return li as HTMLElement
    })
    expect(within(first).getByRole('button', { name: 'Do it yourself' })).toBeInTheDocument()
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    // Once the step starts again (Do it yourself, a reset) it is the learner's.
    send({
      t: 'step',
      seq: 3,
      lessonId: LESSON,
      stepId: 'open',
      status: 'reset',
      index: 0,
      count: 4,
    })
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
    expect(screen.getByRole('button', { name: 'Start this lesson over' })).toBeInTheDocument()
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
  })

  it('when another page loads another lesson, this one offers to load it again', async () => {
    const user = userEvent.setup()
    await openLesson()
    rpcCalls.length = 0
    send({
      t: 'step',
      seq: 60,
      lessonId: '00-orientation/02-getting-unstuck',
      stepId: 'prompt',
      status: 'loaded',
      index: 0,
      count: 3,
    })
    const button = await screen.findByRole('button', { name: 'Load this lesson here' })
    await new Promise((r) => setTimeout(r, 300))
    expect(rpcCalls.some((c) => c.op === 'scenario')).toBe(false)
    await user.click(button)
    await waitFor(() =>
      expect(rpcCalls).toContainEqual({
        op: 'scenario',
        body: { lessonId: LESSON, stepId: 'open' },
      }),
    )
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
