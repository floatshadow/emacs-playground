# Frontend (React + Vite)

The browser application: lesson, project and course-map screens around a live Emacs. It renders what sessiond and the agent report; it never evaluates lesson conditions itself. The design demo in `../design/demo/` is the visual and interaction reference; its `mock-emacs.js` renderers (`renderTicker`, `renderWhere`, `nudges`, `keyify`) are ported to typed modules here.

## 1. Stack and layout

React 19, TypeScript (strict), Vite, pnpm. Routing with React Router (data routers). Server state with TanStack Query; live session state with Zustand. Terminal: `@xterm/xterm` 6.1 with the fit, webgl, unicode11, clipboard and web-links addons. GUI frame: `@novnc/novnc`. PDF preview: `pdfjs-dist`. Markdown: `react-markdown` with `remark-gfm` and `remark-directive`. UI strings: `react-i18next`. Styling: CSS Modules over the design tokens from the demo. Tests: Vitest, Testing Library, MSW, Playwright. Lint and format: typescript-eslint, Prettier.

```
web/
  index.html  vite.config.ts  tsconfig.json  package.json
  src/
    main.tsx  app.tsx  routes.tsx
    protocol.gen.ts        generated from Go (backend.md §4); never edited
    api/
      http.ts              fetch wrapper: JSON, problem+json errors, Origin-safe
      queries.ts           TanStack Query hooks: me, curriculum, lesson, progress, session
      session/
        events.ts          EventsClient: WebSocket, replay, backoff
        pty.ts             PtyClient: binary stream, resize control frames
        vnc.ts             VncClient: RFB over WebSocket (thin wrapper around noVNC)
        rpc.ts             rpc(op, body) with typed ops
    store/
      session.ts           connection, hello, state, ticker, prefix, checks, step, mode, modules, cards
      ui.ts                rail open, preview tab, focus mode, locale, reduced motion
    advice/
      exits.ts             state × exits table → WhereAmI rows
      habits.ts            key stream × habits table → cards
      patterns.ts          stuck patterns (C-g bursts, repeated unbound, read-only typing, long prompt)
      ticker.ts            reducer for prefix and key events, gloss lookup
      keys.ts              key name parsing, KEY_RE, keyify
    components/
      layout/    AppShell TopBar Workspace Rail Drawer
      screen/    TerminalPane VncPane ScreenHeader ScreenOverlay FrameSwitch
      live/      Ticker Keycap WhereAmI Nudges CheatSheet RescueCard
      lesson/    LessonPane Markdown Steps Step Hint TryKeys DemoPlayer
      project/   FileTree Checkpoints TaskPanel
      preview/   PreviewPane PdfView HtmlView MarkdownView ImageView PreviewStatus
      course/    CourseMap PartSection LessonRow ProjectCard
      common/    Button Chip Kbd Toast Dialog SegmentedControl
    screens/     CourseMapScreen LessonScreen ProjectScreen SessionScreen LoginScreen
    styles/      tokens.css reset.css global.css
    i18n/        en.json zh-CN.json
    lib/         debounce.ts time.ts invariant.ts
  tests/         unit and component tests next to sources; e2e/ for Playwright
```

## 2. Screens and routes

| Route | Screen | Data |
|---|---|---|
| `/login` | invite code or OIDC button | `me` |
| `/` | course map with progress | `curriculum`, `progress` |
| `/lesson/:lessonId` | lesson pane, screen, rail | `lesson`, session |
| `/project/:projectId/:taskId?` | file tree and tasks, screen, preview and rail | `lesson` (projects share the format), session |
| `/session` | session status, frame mode, modules, restart, export, bring-your-own-terminal instructions | `session`, `status` |

The session is ensured once (`POST /api/v1/sessions`) when any screen that needs Emacs mounts; the connection layer lives above the router so navigating between lessons keeps the WebSockets open. Loading a lesson calls `rpc('scenario', {lessonId})`.

## 3. State

`useSessionStore` (Zustand):

```ts
interface SessionStore {
  sessionId: string | null
  connection: { events: ConnState; pty: ConnState; vnc: ConnState }
  hello: Hello | null
  state: State | null                 // latest agent state (protocol.gen.ts)
  prefix: string | null               // pending prefix from the tap
  ticker: TickerEntry[]               // newest last, max 40; cleared when another lesson loads
  checks: Record<string, CheckEvent>  // by stepId
  activeStep: { lessonId: string; stepId: string } | null
  outcomes: Record<lessonId, Record<stepId, 'done' | 'demonstrated' | 'skipped'>>  // this visit
  lessonsComplete: string[]
  watched: { lessonId: string; stepId: string } | null  // shows "Do it yourself"
  mode: 'terminal' | 'gui'
  modules: string[]
  serverNudges: Nudge[]
  cards: HabitCard[]                  // layer-2 cards; no expiry, cleared when another lesson loads
  dismissed: Set<string>
  habitsSeen: Set<string>             // persisted in localStorage per session
  mentions: Mentions | null           // keys the lesson on screen mentions
  lastKeyTs: number                   // a prefix event older than this is stale
  totalCommands: number
  applyEvent(ev: Event): void         // the single reducer for /events
  dismiss(id: string): void
  forgetProgress(lessonId?: string): void  // Start over
}
```

`applyEvent` is pure apart from the store write; it is unit-tested with recorded event streams. Derived data is computed with memoised selectors: `selectWhereAmI(state, tables)`, `selectNudges(store, now)`, `selectTicker(store, gloss)`, `selectStepView(scenario, outcomes, progress, checks, hintLevel)`.

Server state (curriculum, progress, me, session metadata) goes through TanStack Query with sensible stale times; progress is invalidated on `step` events. The course map renders its rows once progress has arrived, so lessons never flash "not started".

## 4. Connection layer

- `EventsClient`: opens `/s/{id}/events?after=<lastSeq>`, dispatches each JSON frame to `applyEvent`, reconnects with backoff 250 ms to 8 s with jitter, sends `ping` every 20 s, surfaces `gap` by requesting `rpc('check')` and marking the ticker with a divider.
- `PtyClient`: binary WebSocket; `term.onData` → `TextEncoder` → send; incoming bytes → `term.write`; `ResizeObserver` on the pane → fit addon → `{"t":"resize"}` control frame (debounced 100 ms). Handles `replaced` by showing "attached elsewhere" with a Reattach button.
- `VncClient`: lazily imports noVNC; `new RFB(el, url)`, `resizeSession = true` (noVNC asks Xvnc for the pane's size, so the GUI frame is drawn at scale 1; sessiond starts Xvnc with `-AcceptSetDesktopSize=1` and the Emacs side sizes the GUI frame to its display), pane resizes debounced 200 ms (resizeSession is held off while the size changes; a hidden pane asks for nothing), `scaleViewport = true` as the fallback when the server refuses a size, `clipViewport = false`, `showDotCursor = true`; listens to `connect`, `disconnect`, `clipboard`; `clipboardPasteFrom` on paste when the pane is focused.
- `rpc`: typed wrapper over `POST /s/{id}/rpc/{op}`; errors become toasts unless the caller handles them.

All four read the session id from the store; the gateway path prefix is the only thing that differs between local and server.

## 5. Terminal pane

xterm.js options: `fontFamily: '"Iosevka Nerd Font", "JetBrains Mono", monospace'`, `fontSize: 14`, `lineHeight: 1.2`, `theme` from the modus-vivendi-tinted palette used in the demo, `allowProposedApi: true`, `scrollback: 0` (tmux owns history; Emacs owns scrolling), `macOptionIsMeta: true`, `cursorBlink: false`, `vtExtensions: { kittyKeyboard: true }`, `screenReaderMode` toggled from settings. Addons: fit, webgl (fallback to canvas on context loss), unicode11 (`term.unicode.activeVersion = '11'` for CJK and icon widths), clipboard (OSC 52 both ways), web-links (opens in a new tab).

Keyboard: xterm.js encodes keys; the app does not decode them (the server tap does). Because tmux never forwards Emacs's kitty-keyboard push, xterm.js stays in the legacy encoding; the custom key handler sends CSI u itself for the chords legacy cannot carry (`C-;`, `C-=`, `C-?`, `C-1`, `C-S-a`, `C-RET`; `extkeys.ts`). `attachCustomKeyEventHandler` is used for these things only: outside focus mode, when a browser-reserved combination (`Ctrl+W`, `Ctrl+T`, `Ctrl+N`, `Ctrl+Shift+*`, `Alt+letter` on Firefox) is pressed, show a one-time toast pointing at Focus mode; `Shift+Escape` moves focus from the terminal to the lesson pane (a reserved chrome key, unbound in Emacs), leaving focus mode first when it is on; while "Skip this step" plays a step the learner's keys are swallowed; everything else passes through.

Focus mode is an in-page state first (B14): the stage takes the whole width (the lesson pane and the rail are hidden) and a one-line banner (`role="status"`) above the screen says what the mode does ("Focus mode: keys go to Emacs, and the lesson and panels step aside."), whether the keyboard is locked, and how to leave (a "Leave focus mode" button, the top-bar button, or Shift+Esc). Then `document.documentElement.requestFullscreen()` and `navigator.keyboard.lock()` are tried where available (Chromium), each giving up after 1.5 s (a headless or embedded browser may never answer); the banner reports the lock ("The keyboard is locked: Ctrl+W, Ctrl+T and Ctrl+N reach Emacs too." or that this browser keeps them). Leaving a fullscreen that focus mode entered ends focus mode. The top-bar button carries the description as a tooltip and through `aria-describedby`.

Fonts: `tokens.css` declares `@font-face` for Iosevka Nerd Font served from the gateway (`/fonts/`); the terminal waits for `document.fonts.ready` before the first `fit()` so cell metrics are right.

Overlays over the pane: frame closed, Emacs restarting, Emacs busy, attached elsewhere, reconnecting, and a light veil while "Skip this step" plays the step ("Emacs is doing this step for you…"). These are driven by `emacs` events and connection state and mirror the demo's overlay card.

Nothing in the stage changes size except the window (B7): the screen header is one line of fixed height (long text ends in an ellipsis, chips shrink before the size label), and the area under the screen has a fixed height whether it shows the ticker or the demo caption, so the terminal is never refitted while a demo plays.

## 6. Live panels and the advice engine

The three panels are pure views over the store plus tables from the curriculum index (`tables.exits`, `tables.habits`, `tables.gloss`, `tables.cheats`).

- Ticker: `ticker.ts` keeps entries `{keys: string[], cmd?, gloss?, kind: 'ok'|'prefix'|'unbound'|'quit', mx?: boolean, at, typed?, count?}`. A `prefix` event adds or replaces a pending entry; a `key` event removes pending prefix entries and appends; `(unbound)` is reported by the agent as `cmd: "undefined"` with the key sequence. Consecutive typing commands (`self-insert-command`, `isearch-printing-char`, `org-self-insert-command`, `term-send-raw`, each run separately) collapse into one entry that grows in place: "typed N characters" with the last 12 characters in quotes (B10, R6). Gloss comes from `tables.gloss`, then from `rpc('describe')` for unknown commands (cached per session). Newest entry first, left-most, emphasised; six visible; glosses are clamped to two lines. Key events made only of mouse events (`<mouse-1>`, `<down-mouse-1>`, drags, the wheel, also after an area such as `<mode-line>`) are not shown and do not count as commands: one click to focus the terminal would otherwise add two entries and push the learner's keys out of view. The ticker belongs to the lesson: it is cleared, with the pending prefix and the habit cards, when a step of another lesson loads (a reload also starts it empty). A `prefix` event older (by `ts`) than the newest `key` event is stale and ignored (B4: Emacs already used its keys); a frame switch clears the pending prefix.
- WhereAmI: `exits.ts` evaluates `tables.exits` rows against the `State` object in order. Each row has a `when` predicate (equality, membership and boolean tests over state fields, including `otherWindows.mode in [...]`), a `why` template and a `how` template; templates substitute `{prompt}`, `{buffer}`, `{windows}`. Rows are data; a module that introduces a special mode adds rows. Besides the state, rows may test the virtual root `session` (`protocol.ExitSession`), which the browser fills from the event stream: `session.prefix` (pending prefix), `session.emacs` (`frame-closed`, `suspended`, `restarting`, `unresponsive`), `session.mode` and `session.echo` (latest echo text until the next key).
  The rows' `regex` operators are written for Go's RE2; `predicate.ts` translates them for JavaScript (`translateRe2`, B11): a leading `(?i)`, `(?m)`, `(?s)` becomes RegExp flags, scoped `(?i:…)` and `(?s:…)` groups are spelled out (letters in both cases, `.` as `[\s\S]`), `(?P<name>…)` becomes `(?<name>…)`; anything else JavaScript cannot express is a validation error rather than a row that silently never matches. The Mode row leaves out housekeeping minor modes (`ws-butler-mode`; `HOUSEKEEPING_MINORS` in `exits.ts`).
- Nudges: `habits.ts` fires one card per habit key per session when a single-key `key` event matches `tables.habits`, unless the lesson mentions the key (fix plan contract d): the mentioned keys (`mentions.ts`) are the key sequences in the lesson's Markdown (backticked keys, `:kbd[…]`, `:try[…]`) and in the current step's goal, detail and condition hints, normalised; a card for K is suppressed when a mentioned sequence starts with K (`C-s` in lesson 1.4 suppresses "C-s is not save", `C-x C-s` in 0.1 does not), and a suppressed card does not count as seen. The habit keys seen persist in localStorage (`pg.habitsSeen.<sessionId>`), so "once each" survives reloads; Start over clears them. Cards have no command TTL: they stay until dismissed or until another lesson loads; the panel shows three, newest first, with older ones behind "N earlier tips" (B9). `patterns.ts` has the fixed rules from the demo (three `C-g` in 8 s, the same unbound key twice, typing into a read-only buffer, prompt open for 20 s, three or more windows unless the lesson mentions `C-x 2` or `C-x 3`). Server-side `nudge` events (stuck rules from the scenario) merge into the same list. Dismissing removes exactly that card, for the page session. The rail puts Rescue above "Did you mean", so cards never push it below the fold.
- CheatSheet: rows for the current major mode from `tables.cheats` plus the common rows.
- RescueCard: `rpc('rescue')` ("Never touches your files" belongs to it) and, on its own line ("Puts the files and windows back to how this step started; changes since then are lost."), Reset this step: a "Reset this step?" dialog (Cancel, Reset), then `rpc('restore', {})`, which also clears a demo latch (R10). "I'm stuck" files a report; once it is sent the dialog says "Thanks, the report was sent. If you would rather move on, Emacs can do this step for you." with Skip this step and Close (S2). Toasts appear at the top centre, under the top bar, so they never cover the ticker that shows the Rescue or Reset entry.

`keys.ts` holds `KEY_RE` and `keyify` from the demo, used to render keycaps in hints, exits and nudges.

## 7. Lesson pane

Markdown rendering: `react-markdown` with `remark-gfm`, `remark-directive` and a small plugin that maps directives to components (CURRICULUM.md Part C §2): `:kbd[C-x C-f]` → `<Kbd>`, `:try[C-x C-f]` → `<TryKeys>` (sends `rpc('keys')`), `:jargon[buffer]` → tooltip from the glossary, `:::vscode`, `:::aside`, `:::gui-only` containers, `::steps` placement marker. Backticked text matching `KEY_RE` also renders as keycaps, so authors rarely need `:kbd`.

Steps: `<Steps>` renders the scenario's steps with the latest `check` results: done, active, conditions met or next, the hint for the first unmet condition at the requested level ("Show a hint" increments and calls `rpc('hint')` so progress counts it), "Watch it" runs `<DemoPlayer>`. Which steps are ticked follows the engine (fix plan contract a, X1): its active step of this lesson is always shown active, whatever progress says; steps after it never show a tick (they are evaluated again when reached); steps before it show this visit's outcome (`complete` → done, `demonstrated` → watched, `skipped`) or else their saved status when it is done, demonstrated or skipped, or else not done; with no active step in the lesson, this visit's outcomes and progress decide. Done steps have a filled tick, watched ones a hollow check and "watched", skipped ones an arrow and "skipped". Resuming a lesson starts at the first step that is neither done nor skipped (a watched step is practised). While the active step is scrolled out of the lesson pane (long lessons put `::steps` below the fold), a chip at the pane's bottom edge names it ("Current step: …") and scrolls to it. When a step ends, its tick pops and the step is highlighted for 600 ms, then the next step scrolls into view and a polite live region says "Step done: …" (L1); under reduced motion there is no highlight and the scroll is instant. A finished lesson's box offers "Start this lesson over" (`POST /progress/reset {lessonId}`, then `rpc('scenario', {lessonId, reset: true})`).

DemoPlayer ("Watch it", fix plan contract b): a demonstration, not a completion. It calls `rpc('demo', {stepId, phase: 'start'})` (sessiond restores the step's checkpoint when the learner already ran commands in it and latches the step, so a completion is recorded `demonstrated`), parses the step's `demo` string into key tokens, typed strings and pauses, and sends them through `rpc('keys')` one chunk at a time at human pace (600 ms per chord, 120 ms per typed character, `~N` pauses as written), then `rpc('demo', {phase: 'end'})` in any case. While it plays, the area under the screen shows a caption strip instead of the ticker (`role="status"`, `aria-label="Demo caption"`): "Watching: <goal>", "Starting from the beginning of this step." after a restore, the keys sent so far left to right with the current one highlighted, typed text growing inside quotes, and Stop. Afterwards the step stays active with a hollow check, "You watched this step. Now do it yourself." and a primary Do it yourself button (`rpc('restore', {})`, which clears the latch). Cancelled by any learner keypress in the terminal. Inline "try it" keys are learner input and complete steps as done. playctl replays keep the 170/70 ms pacing (`REPLAY_PACING`).

Skip this step (contract c): `rpc('skip', {stepId})` lets sessiond play the step itself (up to a minute) and record it skipped; a veil covers the screen and holds the learner's keys until it returns, then a toast says "Skipped: <goal>".

## 8. GUI pane

Shown when `mode === 'gui'` or the lesson is tagged `gui`. The switch button calls `rpc('mode', {mode})`; its GUI half has a tooltip saying what the graphical frame is ("the same Emacs, with the same buffers, drawn with images and mouse menus"). While the GUI frame starts (up to 3 s) the pane shows the terminal with a one-line chip ("Graphical frame: the same Emacs drawn with images and mouse menus. Starting…"), then swaps (R10). Both panes stay mounted but only one is visible, so switching back is immediate. The X screen follows the pane's size (§4), so the frame is drawn at scale 1.

## 9. Preview pane

Tabs come from the scenario's `preview.tabs` plus files the learner opens from the file tree. `preview.follow` maps a source file to the artefact shown when that file is current (`docs/report.typ` → `docs/report.pdf`). Views: `PdfView` (pdfjs-dist, worker loaded lazily; renders visible pages to canvases; keeps scale and scroll across reloads; reloads on `file` events for its path), `HtmlView` (sandboxed iframe), `MarkdownView` (same pipeline as lessons, without directives), `ImageView`. `PreviewStatus` shows the last `file` event for the artefact and the time since.

## 10. Course map and projects

The course map renders `curriculum.parts` and `projects` with progress bars from `progress`. Order is recommended, not enforced: a part shows "suggested after part N" rather than a lock. A lesson row reads "done" when every step is done; "finished · N skipped · M watched" with a hollow mark when every step is done, watched or skipped but not all are done (from the progress `counts`); open rows add "· N skipped" when there are any. Continue skips lessons whose steps are all done or skipped. Start over (fix plan contract a, B5) sits next to Continue once there is progress, and in a Progress section of the Session page: a "Start over?" dialog ("This clears the progress of every lesson: ticks, hints and resets. Your files and Emacs stay as they are."), then `POST /api/v1/progress/reset` with no body; the page forgets its own memory of that progress (outcomes, hint levels, habit keys seen, cards) and sessiond's `dropped` step event clears the active step. Project screens use the same lesson format with the file tree (`GET /preview/<root>/` listings), the checkpoint list (`rpc('checkpoints')`) and Restore (`rpc('restore', {label})` after a confirmation dialog that names what will be overwritten).

## 11. Styling

`tokens.css` carries the palette and type scale from the demo (paper, ink, violet, screen, mode line, ok, warn, err; Source Serif 4 for prose and interface, Iosevka or JetBrains Mono for keys and the screen). Components use CSS Modules; no utility framework. Layout is the demo's three-column grid collapsing to two then one column at 1440 px and 860 px: from 1440 px down the rail is a drawer behind the "Where am I" button, so the terminal keeps its width (S5: at 1280 px it has 129 columns instead of 76). Motion only on state changes (step done, card appears), disabled under `prefers-reduced-motion`. A dark chrome theme is a token swap left for later.

## 12. Internationalisation

UI strings in `i18n/*.json` through `react-i18next`. Lesson content is per file (`lesson.zh-CN.md`) and tables per locale (`exits.zh-CN.yaml`), served by the gateway according to `Accept-Language` or the user's choice. Key names and command names are never translated.

## 13. Accessibility

The terminal is a canvas; everything it conveys about state is also text: the ticker (`aria-live="polite"`), WhereAmI and the echo line mirrored into a visually hidden live region. xterm.js `screenReaderMode` is a setting. All chrome is keyboard-operable with visible focus rings; `Shift+Escape` leaves the terminal; dialogs trap focus; colour contrast meets WCAG AA on paper and on the screen theme (the modus palette is designed for it).

## 14. Testing and quality

- Unit (Vitest): `advice/*` against recorded streams (ported from the demo's assertions), `ticker.ts`, `applyEvent`, directive plugin, `keys.ts`.
- Component (Testing Library + MSW): LessonScreen with a fake events WebSocket (`vitest-websocket-mock`) and mocked REST; asserts step rendering, hint levels, overlays.
- End to end (Playwright): against `docker compose` in local mode. Scenarios: open lesson 2.3, type `C-x C-f`, expect the ticker entry `find-file`; complete the step with keys; watch a demonstration and do it yourself; reset the step; switch to the GUI frame; preview reload on save. `web/e2e/review.mjs` is the acceptance test of the newcomer-review fixes. The scripts post a progress reset with no body when they end.
- Types: `protocol.gen.ts` is regenerated in CI; the build fails on drift.
- Lint: typescript-eslint recommended plus `react-hooks`, `jsx-a11y`; Prettier.

## 15. Build and development

`pnpm dev` runs Vite on `:5173` proxying `/api`, `/s`, `/fonts` and `/internal` to the gateway (`playctl dev` starts both). `pnpm build` emits `web/dist`, which the gateway embeds with `go:embed`; the gateway Dockerfile builds the web bundle in a Node stage first. Code splitting: noVNC, pdfjs and the course map are lazy; the lesson screen with xterm.js is the main chunk. Budget: 600 KB gzipped for the main chunk; checked in CI with `vite-bundle-visualizer` output and a size gate.

## 16. Performance budgets

Event to ticker render under 16 ms (one frame); `applyEvent` allocation-free for `key` and `prefix`; selectors memoised by state identity; ticker and WhereAmI render only when their inputs change (store slices). The terminal uses the webgl renderer; `fit()` is debounced. The lesson Markdown is parsed once per lesson.
