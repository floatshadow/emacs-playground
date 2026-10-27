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
  ticker: TickerEntry[]               // newest last, max 40
  checks: Record<string, CheckEvent>  // by stepId
  activeStep: { lessonId: string; stepId: string } | null
  mode: 'terminal' | 'gui'
  modules: string[]
  serverNudges: Nudge[]
  cards: HabitCard[]                  // layer-2 cards, with createdAtTotal for expiry
  dismissed: Set<string>
  habitsSeen: Set<string>
  totalCommands: number
  applyEvent(ev: Event): void         // the single reducer for /events
  dismiss(id: string): void
}
```

`applyEvent` is pure apart from the store write; it is unit-tested with recorded event streams. Derived data is computed with memoised selectors: `selectWhereAmI(state, tables)`, `selectNudges(store, tables, now)`, `selectTicker(store, gloss)`, `selectStepView(scenario, checks, hintLevel)`.

Server state (curriculum, progress, me, session metadata) goes through TanStack Query with sensible stale times; progress is invalidated on `step` events.

## 4. Connection layer

- `EventsClient`: opens `/s/{id}/events?after=<lastSeq>`, dispatches each JSON frame to `applyEvent`, reconnects with backoff 250 ms to 8 s with jitter, sends `ping` every 20 s, surfaces `gap` by requesting `rpc('check')` and marking the ticker with a divider.
- `PtyClient`: binary WebSocket; `term.onData` → `TextEncoder` → send; incoming bytes → `term.write`; `ResizeObserver` on the pane → fit addon → `{"t":"resize"}` control frame (debounced 100 ms). Handles `replaced` by showing "attached elsewhere" with a Reattach button.
- `VncClient`: lazily imports noVNC; `new RFB(el, url)`, `scaleViewport = true`, `clipViewport = false`, `showDotCursor = true`; listens to `connect`, `disconnect`, `clipboard`; `clipboardPasteFrom` on paste when the pane is focused.
- `rpc`: typed wrapper over `POST /s/{id}/rpc/{op}`; errors become toasts unless the caller handles them.

All four read the session id from the store; the gateway path prefix is the only thing that differs between local and server.

## 5. Terminal pane

xterm.js options: `fontFamily: '"Iosevka Nerd Font", "JetBrains Mono", monospace'`, `fontSize: 14`, `lineHeight: 1.2`, `theme` from the modus-vivendi-tinted palette used in the demo, `allowProposedApi: true`, `scrollback: 0` (tmux owns history; Emacs owns scrolling), `macOptionIsMeta: true`, `cursorBlink: false`, `vtExtensions: { kittyKeyboard: true }`, `screenReaderMode` toggled from settings. Addons: fit, webgl (fallback to canvas on context loss), unicode11 (`term.unicode.activeVersion = '11'` for CJK and icon widths), clipboard (OSC 52 both ways), web-links (opens in a new tab).

Keyboard: xterm.js encodes keys; the app does not decode them (the server tap does). Because tmux never forwards Emacs's kitty-keyboard push, xterm.js stays in the legacy encoding; the custom key handler sends CSI u itself for the chords legacy cannot carry (`C-;`, `C-=`, `C-?`, `C-1`, `C-S-a`, `C-RET`; `extkeys.ts`). `attachCustomKeyEventHandler` is used for three things only: outside focus mode, when a browser-reserved combination (`Ctrl+W`, `Ctrl+T`, `Ctrl+N`, `Ctrl+Shift+*`, `Alt+letter` on Firefox) is pressed, show a one-time toast pointing at Focus mode; `Shift+Escape` moves focus from the terminal to the lesson pane (a reserved chrome key, unbound in Emacs); everything else passes through.

Focus mode: `screenWrap.requestFullscreen()` then `navigator.keyboard.lock()` where available (Chromium). Exiting fullscreen ends focus mode. Firefox gets fullscreen without the lock and a note about the keys it keeps.

Fonts: `tokens.css` declares `@font-face` for Iosevka Nerd Font served from the gateway (`/fonts/`); the terminal waits for `document.fonts.ready` before the first `fit()` so cell metrics are right.

Overlays over the pane: frame closed, Emacs restarting, Emacs busy, attached elsewhere, reconnecting. These are driven by `emacs` events and connection state and mirror the demo's overlay card.

## 6. Live panels and the advice engine

The three panels are pure views over the store plus tables from the curriculum index (`tables.exits`, `tables.habits`, `tables.gloss`, `tables.cheats`).

- Ticker: `ticker.ts` keeps entries `{keys: string[], cmd?, gloss?, kind: 'ok'|'prefix'|'unbound'|'quit', mx?: boolean, at}`. A `prefix` event adds or replaces a pending entry; a `key` event removes pending prefix entries and appends; `(unbound)` is reported by the agent as `cmd: "undefined"` with the key sequence. Gloss comes from `tables.gloss`, then from `rpc('describe')` for unknown commands (cached per session). Newest entry first, left-most, emphasised; six visible. Key events made only of mouse events (`<mouse-1>`, `<down-mouse-1>`, drags, the wheel, also after an area such as `<mode-line>`) are not shown and do not count as commands: one click to focus the terminal would otherwise add two entries and push the learner's keys out of view.
- WhereAmI: `exits.ts` evaluates `tables.exits` rows against the `State` object in order. Each row has a `when` predicate (equality, membership and boolean tests over state fields, including `otherWindows.mode in [...]`), a `why` template and a `how` template; templates substitute `{prompt}`, `{buffer}`, `{windows}`. Rows are data; a module that introduces a special mode adds rows. Besides the state, rows may test the virtual root `session` (`protocol.ExitSession`), which the browser fills from the event stream: `session.prefix` (pending prefix), `session.emacs` (`frame-closed`, `suspended`, `restarting`, `unresponsive`), `session.mode` and `session.echo` (latest echo text until the next key).
- Nudges: `habits.ts` fires one card per habit key per session when a single-key `key` event matches `tables.habits`; cards expire after 12 commands. `patterns.ts` has the fixed rules from the demo (three `C-g` in 8 s, the same unbound key twice, typing into a read-only buffer, prompt open for 20 s, three or more windows). Server-side `nudge` events (stuck rules from the scenario) merge into the same list. Dismissals are per session.
- CheatSheet: rows for the current major mode from `tables.cheats` plus the common rows.
- RescueCard: `rpc('rescue')` and `rpc('restore', {ref: currentStepRef})` with toasts naming what was done. Toasts appear at the top centre, under the top bar, so they never cover the ticker that shows the Rescue or Reset entry.

`keys.ts` holds `KEY_RE` and `keyify` from the demo, used to render keycaps in hints, exits and nudges.

## 7. Lesson pane

Markdown rendering: `react-markdown` with `remark-gfm`, `remark-directive` and a small plugin that maps directives to components (CURRICULUM.md Part C §2): `:kbd[C-x C-f]` → `<Kbd>`, `:try[C-x C-f]` → `<TryKeys>` (sends `rpc('keys')`), `:jargon[buffer]` → tooltip from the glossary, `:::vscode`, `:::aside`, `:::gui-only` containers, `::steps` placement marker. Backticked text matching `KEY_RE` also renders as keycaps, so authors rarely need `:kbd`.

Steps: `<Steps>` renders the scenario's steps with the latest `check` results: done, active, conditions met or next, the hint for the first unmet condition at the requested level ("Show a hint" increments and calls `rpc('hint')` so progress counts it), "Watch it" runs `<DemoPlayer>`. While the active step is scrolled out of the lesson pane (long lessons put `::steps` below the fold), a chip at the pane's bottom edge names it ("Current step: …") and scrolls to it.

DemoPlayer: parses the step's `demo` string into key tokens, typed strings and pauses, and sends them through `rpc('keys')` one chunk at a time with pacing (170 ms per chord, 70 ms per typed character) so the learner can follow in the ticker. Cancelled by any learner keypress in the terminal.

## 8. GUI pane

Shown when `mode === 'gui'` or the lesson is tagged `gui`. The switch button calls `rpc('mode', {mode})`; while the GUI frame starts (up to 3 s) the pane shows the terminal with a "starting GUI frame" chip, then swaps. Both panes stay mounted but only one is visible, so switching back is immediate.

## 9. Preview pane

Tabs come from the scenario's `preview.tabs` plus files the learner opens from the file tree. `preview.follow` maps a source file to the artefact shown when that file is current (`docs/report.typ` → `docs/report.pdf`). Views: `PdfView` (pdfjs-dist, worker loaded lazily; renders visible pages to canvases; keeps scale and scroll across reloads; reloads on `file` events for its path), `HtmlView` (sandboxed iframe), `MarkdownView` (same pipeline as lessons, without directives), `ImageView`. `PreviewStatus` shows the last `file` event for the artefact and the time since.

## 10. Course map and projects

The course map renders `curriculum.parts` and `projects` with progress bars from `progress`. Order is recommended, not enforced: a part shows "suggested after part N" rather than a lock. Project screens use the same lesson format with the file tree (`GET /preview/<root>/` listings), the checkpoint list (`rpc('checkpoints')`) and Restore (`rpc('restore', {label})` after a confirmation dialog that names what will be overwritten).

## 11. Styling

`tokens.css` carries the palette and type scale from the demo (paper, ink, violet, screen, mode line, ok, warn, err; Source Serif 4 for prose and interface, Iosevka or JetBrains Mono for keys and the screen). Components use CSS Modules; no utility framework. Layout is the demo's three-column grid collapsing to two then one column at 1180 px and 860 px. Motion only on state changes (step done, card appears), disabled under `prefers-reduced-motion`. A dark chrome theme is a token swap left for later.

## 12. Internationalisation

UI strings in `i18n/*.json` through `react-i18next`. Lesson content is per file (`lesson.zh-CN.md`) and tables per locale (`exits.zh-CN.yaml`), served by the gateway according to `Accept-Language` or the user's choice. Key names and command names are never translated.

## 13. Accessibility

The terminal is a canvas; everything it conveys about state is also text: the ticker (`aria-live="polite"`), WhereAmI and the echo line mirrored into a visually hidden live region. xterm.js `screenReaderMode` is a setting. All chrome is keyboard-operable with visible focus rings; `Shift+Escape` leaves the terminal; dialogs trap focus; colour contrast meets WCAG AA on paper and on the screen theme (the modus palette is designed for it).

## 14. Testing and quality

- Unit (Vitest): `advice/*` against recorded streams (ported from the demo's assertions), `ticker.ts`, `applyEvent`, directive plugin, `keys.ts`.
- Component (Testing Library + MSW): LessonScreen with a fake events WebSocket (`vitest-websocket-mock`) and mocked REST; asserts step rendering, hint levels, overlays.
- End to end (Playwright): against `docker compose` in local mode. Scenarios: open lesson 2.3, type `C-x C-f`, expect the ticker entry `find-file`; complete the step with keys; reset the step; switch to the GUI frame; preview reload on save.
- Types: `protocol.gen.ts` is regenerated in CI; the build fails on drift.
- Lint: typescript-eslint recommended plus `react-hooks`, `jsx-a11y`; Prettier.

## 15. Build and development

`pnpm dev` runs Vite on `:5173` proxying `/api`, `/s`, `/fonts` and `/internal` to the gateway (`playctl dev` starts both). `pnpm build` emits `web/dist`, which the gateway embeds with `go:embed`; the gateway Dockerfile builds the web bundle in a Node stage first. Code splitting: noVNC, pdfjs and the course map are lazy; the lesson screen with xterm.js is the main chunk. Budget: 600 KB gzipped for the main chunk; checked in CI with `vite-bundle-visualizer` output and a size gate.

## 16. Performance budgets

Event to ticker render under 16 ms (one frame); `applyEvent` allocation-free for `key` and `prefix`; selectors memoised by state identity; ticker and WhereAmI render only when their inputs change (store slices). The terminal uses the webgl renderer; `fit()` is debounced. The lesson Markdown is parsed once per lesson.
