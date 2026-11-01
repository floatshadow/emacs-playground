/**
 * Live session state (frontend.md §3): one Zustand store fed by the single
 * reducer applyEvent for /events. `reduce` is pure (it receives the clock)
 * and is unit-tested with recorded event streams; the store only writes its
 * result.
 */
import { create } from 'zustand'
import {
  Version,
  type CheckEvent,
  type ErrorEvent,
  type Event,
  type FrameKind,
  type HelloEvent,
  type LessonSnapshot,
  type State,
  type Tables,
} from '../protocol.gen'
import {
  habitCardFor,
  loadHabitsSeen,
  saveHabitsSeen,
  type HabitCard,
  type Nudge,
} from '../advice/habits'
import { mentionedNow, type Mentions } from '../advice/mentions'
import {
  tickerGap,
  tickerNote,
  isMouseOnly,
  tickerOnKey,
  tickerOnPrefix,
  type TickerEntry,
} from '../advice/ticker'
import { DEFAULT_TABLES } from '../advice/defaults'

export type ConnState =
  | 'idle'
  | 'connecting'
  | 'open'
  | 'reconnecting'
  | 'closed'
  /** /pty only: another viewer took over (close 4000). */
  | 'replaced'
  /** /pty only: Emacs unavailable (close 4001). */
  | 'unavailable'

export type EmacsOverlay = 'frame-closed' | 'suspended' | 'restarting' | 'stopping' | null

export interface EmacsStatusView {
  overlay: EmacsOverlay
  /** Missed heartbeats: the "Emacs is busy" banner. */
  busy: boolean
  reason?: string
  /** When the daemon last came back after a restart (ms); drives a notice. */
  restartedAt?: number
  /** Frame that was closed, for the overlay text. */
  frame?: FrameKind
}

/** The step the learner works on, as the lesson snapshot says (activeStep). */
export interface ActiveStep {
  lessonId: string
  stepId: string
  index: number
  count: number
}

export interface FileMark {
  event: 'created' | 'modified' | 'deleted'
  ts: number
  /** Increments on every event for the path: a cache-busting version. */
  version: number
}

export interface SessionData {
  sessionId: string | null
  connection: { events: ConnState; pty: ConnState; vnc: ConnState }
  hello: HelloEvent | null
  state: State | null
  prefix: string | null
  ticker: TickerEntry[]
  /** Latest check of the snapshot's attempt, by stepId. */
  checks: Record<string, CheckEvent>
  /**
   * sessiond's lesson snapshot (api.md §4.1): the only source of the loaded
   * lesson, its active step, phase, attempt, outcomes, lap and busy op.
   * hello replaces it; a lesson event applies only with a higher rev.
   */
  lesson: LessonSnapshot | null
  /** hello named another protocol version: nothing automatic may run. */
  incompatible: boolean
  mode: FrameKind
  modules: string[]
  serverNudges: Nudge[]
  cards: HabitCard[]
  dismissed: Set<string>
  habitsSeen: Set<string>
  /** Keys the current lesson mentions (habit cards and patterns stay quiet for them). */
  mentions: Mentions | null
  totalCommands: number
  /** Server time (ts) of the newest key event: older prefix events are stale. */
  lastKeyTs: number
  emacs: EmacsStatusView
  echo: { text: string; at: number } | null
  /** Echo text still shown: set by echo events, cleared by the next key (exits.yaml session.echo). */
  echoLive: string | null
  minibufferSince: number | null
  files: Record<string, FileMark>
  /** Increments on checkpoint events; checkpoint lists refetch on change. */
  checkpointsVersion: number
  /** Increments when a workspace file is created or deleted; the file tree refetches. */
  treeVersion: number
  lastError: ErrorEvent | null
  lastSeq: number
  /** Advice tables from the curriculum index (with built-in defaults). */
  tables: Tables
  /** rpc('describe') glosses for commands missing from tables.gloss. */
  described: Record<string, string>
  /**
   * Key events before this time (ms) raise no habit cards and their ticker
   * entries are marked replayed (no stuck patterns): the demo player or
   * sessiond is typing, not the learner, and a habit shows once per
   * session, so a demo that uses C-s to search must not use up "C-s is not
   * save".
   */
  quietHabitsUntil: number
  /** The lap that finished while this page listened (a lesson event's lapMs). */
  lapDone: { lessonId: string; ms: number } | null
}

const actives = new WeakMap<LessonSnapshot, ActiveStep | null>()

/**
 * The active step: the snapshot's step while it is active or watched. One
 * object per snapshot, so it is a stable store selector.
 */
export function activeStep(s: Pick<SessionData, 'lesson'>): ActiveStep | null {
  const l = s.lesson
  if (!l) return null
  let a = actives.get(l)
  if (a === undefined) {
    const { step, lessonId, phase } = l
    a =
      step && lessonId && (phase === 'active' || phase === 'watching')
        ? { lessonId, stepId: step.id, index: step.index, count: step.count }
        : null
    actives.set(l, a)
  }
  return a
}

export function initialSessionData(): SessionData {
  return {
    sessionId: null,
    connection: { events: 'idle', pty: 'idle', vnc: 'idle' },
    hello: null,
    state: null,
    prefix: null,
    ticker: [],
    checks: {},
    lesson: null,
    incompatible: false,
    mode: 'terminal',
    modules: [],
    serverNudges: [],
    cards: [],
    dismissed: new Set(),
    habitsSeen: new Set(),
    mentions: null,
    totalCommands: 0,
    lastKeyTs: 0,
    emacs: { overlay: null, busy: false },
    echo: null,
    echoLive: null,
    minibufferSince: null,
    files: {},
    checkpointsVersion: 0,
    treeVersion: 0,
    lastError: null,
    lastSeq: 0,
    tables: DEFAULT_TABLES,
    described: {},
    quietHabitsUntil: 0,
    lapDone: null,
  }
}

/**
 * The reducer for /events. Pure: the result depends only on the inputs.
 * Unknown event types are ignored (forward compatibility).
 */
export function reduce(s: SessionData, ev: Event, now: number): SessionData {
  const seq = typeof ev.seq === 'number' ? ev.seq : s.lastSeq
  const base: SessionData = seq !== s.lastSeq ? { ...s, lastSeq: Math.max(s.lastSeq, seq) } : s
  switch (ev.t) {
    case 'hello':
      return {
        ...base,
        // A fresh (non-replayed) hello may come from a restarted sessiond
        // whose seq starts again at 1.
        lastSeq: ev.replayed ? base.lastSeq : (ev.seq ?? 0),
        hello: ev,
        sessionId: ev.sessionId || base.sessionId,
        mode: ev.mode,
        modules: ev.modules ?? [],
        emacs: { ...base.emacs, overlay: null, busy: false },
        prefix: null,
        // The current truth at its moment, whatever its rev (a restarted
        // sessiond counts from 0 again); checks are asked for anew. An
        // older sessiond sends none.
        lesson: ev.lesson ?? null,
        incompatible: ev.protocol !== Version,
        checks: {},
      }
    case 'key': {
      // Server time only: prefix events carry the same clock (the client's may differ).
      const lastKeyTs = ev.ts !== undefined ? Math.max(base.lastKeyTs, ev.ts) : base.lastKeyTs
      // Clicks and scrolling are not keys: no ticker entry, no habit card.
      if (!ev.mx && isMouseOnly(ev.keys)) return { ...base, echoLive: null, lastKeyTs }
      const quiet = now < base.quietHabitsUntil
      const ticker = tickerOnKey(base.ticker, ev, now, quiet)
      const entry = ticker[ticker.length - 1]
      const totalCommands = base.totalCommands + 1
      let cards = base.cards
      let habitsSeen = base.habitsSeen
      if (entry && !quiet) {
        const mentioned = mentionedNow(base.mentions, activeStep(base))
        const card = habitCardFor(entry, base.tables.habits, habitsSeen, totalCommands, mentioned)
        if (card) {
          habitsSeen = new Set(habitsSeen).add(card.key)
          cards = [...cards, card]
        }
      }
      return {
        ...base,
        ticker,
        prefix: null,
        totalCommands,
        cards,
        habitsSeen,
        echoLive: null,
        lastKeyTs,
      }
    }
    case 'prefix': {
      // A prefix older than the newest command is stale: Emacs already used
      // its keys (the tap reports a lone ESC from a timer).
      if (ev.ts !== undefined && ev.ts < base.lastKeyTs) return base
      const keys = ev.keys.trim()
      return { ...base, ticker: tickerOnPrefix(base.ticker, ev, now), prefix: keys || null }
    }
    case 'state': {
      const opened = ev.minibuffer !== null && ev.minibuffer !== undefined
      let minibufferSince = base.minibufferSince
      if (opened && minibufferSince === null) minibufferSince = ev.ts ?? now
      if (!opened) minibufferSince = null
      return { ...base, state: ev, minibufferSince }
    }
    case 'echo':
      return { ...base, echo: { text: ev.text, at: ev.ts ?? now }, echoLive: ev.text }
    case 'minibuffer': {
      if (ev.open) {
        return base.minibufferSince === null ? { ...base, minibufferSince: ev.ts ?? now } : base
      }
      return { ...base, minibufferSince: null }
    }
    case 'emacs': {
      const e = base.emacs
      switch (ev.event) {
        case 'frame-closed': {
          // The GUI frame closing while the terminal is shown (a switch back
          // to the terminal) is not something to cover the screen for.
          if (ev.frame === 'gui' && base.mode !== 'gui') return base
          const next: EmacsStatusView = { ...e, overlay: 'frame-closed' }
          if (ev.frame) next.frame = ev.frame
          if (ev.reason) next.reason = ev.reason
          return { ...base, emacs: next }
        }
        case 'frame-opened':
          return {
            ...base,
            emacs: { ...e, overlay: e.overlay === 'frame-closed' ? null : e.overlay },
          }
        case 'suspended':
          return { ...base, emacs: { ...e, overlay: 'suspended' } }
        case 'resumed':
          return { ...base, emacs: { ...e, overlay: e.overlay === 'suspended' ? null : e.overlay } }
        case 'restarting': {
          const next: EmacsStatusView = { ...e, overlay: 'restarting', busy: false }
          if (ev.reason) next.reason = ev.reason
          return { ...base, emacs: next, prefix: null }
        }
        case 'daemon-restarted':
          return {
            ...base,
            emacs: { ...e, overlay: null, busy: false, restartedAt: ev.ts ?? now },
          }
        case 'unresponsive':
          return { ...base, emacs: { ...e, busy: true } }
        case 'responsive':
          return { ...base, emacs: { ...e, busy: false } }
        case 'stopping':
          return { ...base, emacs: { ...e, overlay: 'stopping' } }
      }
      return base
    }
    case 'file': {
      const prev = base.files[ev.path]
      return {
        ...base,
        treeVersion: ev.event === 'modified' ? base.treeVersion : base.treeVersion + 1,
        files: {
          ...base.files,
          [ev.path]: { event: ev.event, ts: ev.ts ?? now, version: (prev?.version ?? 0) + 1 },
        },
      }
    }
    case 'check':
      // A check of another attempt is stale.
      if (ev.attempt !== base.lesson?.attempt) return base
      return { ...base, checks: { ...base.checks, [ev.stepId]: ev } }
    case 'lesson':
      return reduceLesson(base, ev)
    case 'nudge': {
      const n: Nudge = { id: ev.id, kind: ev.kind, title: ev.title, body: ev.body }
      return { ...base, serverNudges: [...base.serverNudges.filter((x) => x.id !== ev.id), n] }
    }
    case 'checkpoint':
      return { ...base, checkpointsVersion: base.checkpointsVersion + 1 }
    case 'mode':
      // Another frame reads keys from another keyboard: a prefix pending in
      // the one left behind is not what the learner is in.
      return { ...base, mode: ev.mode, prefix: ev.mode === base.mode ? base.prefix : null }
    case 'modules':
      return { ...base, modules: ev.enabled ?? base.modules }
    case 'gap':
      return { ...base, ticker: tickerGap(base.ticker, now) }
    case 'error':
      return { ...base, lastError: ev }
  }
  return base
}

/**
 * A lesson event applies only when its rev is higher than the snapshot's:
 * replayed and reordered events are old news. Another lesson takes the
 * ticker, prefix and habit cards with it; a new attempt clears the checks.
 */
function reduceLesson(base: SessionData, ev: Extract<Event, { t: 'lesson' }>): SessionData {
  const prev = base.lesson
  if (prev && ev.lesson.rev <= prev.rev) return base
  const l = ev.lesson
  const next: SessionData = { ...base, lesson: l }
  if (l.attempt !== prev?.attempt) next.checks = {}
  if (l.lessonId !== prev?.lessonId) {
    next.ticker = []
    next.prefix = null
    next.cards = []
  }
  if ((ev.lapMs ?? 0) > 0) next.lapDone = { lessonId: l.lessonId ?? '', ms: ev.lapMs ?? 0 }
  else if (
    (l.lap && l.lap.id !== prev?.lap?.id) ||
    (ev.change === 'loaded' && l.lessonId !== prev?.lessonId)
  )
    next.lapDone = null
  return next
}

/** How long after a demo ends its trailing key events still raise no habit cards. */
export const DEMO_QUIET_TAIL_MS = 600

export interface SessionStore extends SessionData {
  applyEvent(ev: Event): void
  dismiss(id: string): void
  setSessionId(id: string | null): void
  setConnection(kind: 'events' | 'pty' | 'vnc', st: ConnState): void
  setTables(t: Tables): void
  setDescribed(cmd: string, gloss: string): void
  /** The keys the lesson on screen mentions (nudge suppression). */
  setMentions(m: Mentions | null): void
  /**
   * Demo playback started (true) or ended (false). Habit cards stay quiet
   * while it plays and for tailMs (default DEMO_QUIET_TAIL_MS) after, since
   * the agent's key events trail the keys the player sent. When the
   * learner's own typing stopped the demo the tail is 0: their keys count.
   */
  setDemoPlaying(playing: boolean, tailMs?: number): void
  /**
   * "Start over" of the whole course: forgets the habit keys seen (also in
   * localStorage), habit cards and dismissals. Lesson facts come from
   * sessiond's snapshot.
   */
  forgetProgress(): void
  /** A synthetic ticker entry (Rescue, Reset). */
  note(entry: { keys: string[]; cmd: string; gloss?: string; kind: TickerEntry['kind'] }): void
  /** Back to the initial state (tests, logout). */
  reset(): void
}

export const useSessionStore = create<SessionStore>()((set) => ({
  ...initialSessionData(),
  applyEvent: (ev) => set((s) => reduce(s, ev, Date.now())),
  dismiss: (id) => set((s) => ({ dismissed: new Set(s.dismissed).add(id) })),
  setSessionId: (id) => set({ sessionId: id }),
  setConnection: (kind, st) =>
    set((s) => (s.connection[kind] === st ? s : { connection: { ...s.connection, [kind]: st } })),
  setTables: (tables) => set({ tables }),
  setDescribed: (cmd, gloss) => set((s) => ({ described: { ...s.described, [cmd]: gloss } })),
  setMentions: (mentions) => set({ mentions }),
  setDemoPlaying: (playing, tailMs = DEMO_QUIET_TAIL_MS) =>
    set({ quietHabitsUntil: playing ? Number.POSITIVE_INFINITY : Date.now() + tailMs }),
  forgetProgress: () =>
    set((s) => {
      if (s.sessionId) saveHabitsSeen(s.sessionId, new Set())
      return { habitsSeen: new Set<string>(), cards: [], dismissed: new Set<string>() }
    }),
  note: (entry) => set((s) => ({ ticker: tickerNote(s.ticker, entry, Date.now()) })),
  reset: () => set(initialSessionData()),
}))

// "Once each" survives reloads: the habit keys already shown are kept per
// session in localStorage, read when the session id becomes known and
// written whenever a card fires.
useSessionStore.subscribe((s, prev) => {
  if (s.sessionId && s.sessionId !== prev.sessionId) {
    const stored = loadHabitsSeen(s.sessionId)
    if (stored.some((k) => !s.habitsSeen.has(k))) {
      useSessionStore.setState({ habitsSeen: new Set([...s.habitsSeen, ...stored]) })
    }
    return
  }
  if (s.sessionId && s.habitsSeen !== prev.habitsSeen && s.habitsSeen.size > 0) {
    saveHabitsSeen(s.sessionId, s.habitsSeen)
  }
})

/** Resolves once the store has a session id (rpc and the panes wait on it). */
export function whenSessionId(timeoutMs = 30000): Promise<string> {
  const cur = useSessionStore.getState().sessionId
  if (cur) return Promise.resolve(cur)
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => {
      unsub()
      reject(new Error('no session'))
    }, timeoutMs)
    const unsub = useSessionStore.subscribe((s) => {
      if (s.sessionId) {
        clearTimeout(t)
        unsub()
        resolve(s.sessionId)
      }
    })
  })
}
