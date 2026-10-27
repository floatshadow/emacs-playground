# Frontend (React + Vite)

The browser application: lesson, project and course-map screens around a live Emacs. It renders what sessiond and the agent report; it never evaluates step conditions itself (it does evaluate the exits table, §6). The design demo in `../design/demo/` is the visual and interaction reference; its `mock-emacs.js` renderers (`renderTicker`, `renderWhere`, `nudges`, `keyify`) are ported to typed modules here. Server-side contracts referenced below (RPC ops, events, progress) are specified in api.md.

## 1. Stack and layout

React 19, TypeScript (strict), Vite, pnpm. Routing with React Router (data routers). Server state with TanStack Query; live session state with Zustand. Terminal: `@xterm/xterm` 6.1 with the fit, webgl, unicode11, clipboard and web-links addons. GUI frame: `@novnc/novnc`. PDF preview: `pdfjs-dist`. Markdown: `react-markdown` with `remark-gfm` and `remark-directive`. UI strings: `react-i18next`. Styling: CSS Modules over the design tokens from the demo. Tests: Vitest, Testing Library, MSW, `vitest-websocket-mock`; browser scripts with `playwright-core`. Lint and format: typescript-eslint, Prettier.

```
web/
  index.html  vite.config.ts  tsconfig.json  package.json
  src/
    main.tsx  app.tsx  routes.tsx
    protocol.gen.ts        generated from Go (internal/protocol/tsgen); never edited
    api/
      http.ts              fetch wrapper: JSON, problem+json errors, Origin-safe
      queries.ts           TanStack Query hooks: me, curriculum, lesson, progress, session; resetProgress
      session/
        connection.ts      ensures the session, owns the three clients
        events.ts          EventsClient: WebSocket, replay, backoff (backoff.ts)
        pty.ts             PtyClient: binary stream, resize control frames
        vnc.ts             VncClient: RFB over WebSocket (thin wrapper around noVNC)
        rpc.ts             rpc(op, body) with typed ops
    store/
      session.ts           the event reducer and live session state (§3)
      selectors.ts         memoised views: where am I, nudges, step statuses, resume target
      ui.ts                rail, preview tabs, focus mode, hint levels, demo and skip state, toasts, prefs
    advice/
      exits.ts             state × exits table → WhereAmI rows
      predicate.ts         protocol.Predicate port, RE2 → RegExp translation
      habits.ts            key stream × habits table → cards
      mentions.ts          keys the lesson mentions (nudge suppression)
      patterns.ts          fixed stuck patterns
      ticker.ts            reducer for prefix and key events, gloss lookup
      keys.ts              key name parsing, KEY_RE, keyify
      defaults.ts          built-in tables used until the curriculum index arrives
    components/
      layout/    AppShell RootLayout TopBar Workspace Rail Drawer ScreenActions
      screen/    Stage TerminalPane VncPane ScreenHeader ScreenOverlay FrameSwitch FocusBanner
                 LiveAnnouncer; hosts.ts (terminal and VNC hosts), extkeys.ts, focusMode.ts
      live/      Ticker DemoCaption Keycap WhereAmI Nudges CheatSheet RescueCard AuthorPanel; actions.ts
      lesson/    LessonPane Markdown Steps Step Hint TryKeys DemoPlayer; demoPlayer.ts, directives.ts
      project/   FileTree Checkpoints TaskPanel
      preview/   PreviewPane PdfView HtmlView MarkdownView ImageView PreviewStatus
      course/    CourseMap PartSection LessonRow ProjectCard StartOver
      common/    Button Chip Kbd Toast Dialog SegmentedControl
    screens/     CourseMapScreen LessonScreen ProjectScreen SessionScreen LoginScreen
    styles/      tokens.css reset.css global.css
    i18n/        en.json zh-CN.json
    lib/         demo.ts (demo strings) debounce.ts time.ts localtext.ts invariant.ts
  dev/           the fake session (web/dev/README.md), bundle size gate
  e2e/           browser scripts against the running stack (§14)
```

Unit and component tests sit next to their sources (`*.test.ts[x]`).

## 2. Screens and routes

| Route | Screen | Data |
|---|---|---|
| `/login` | invite code or OIDC button | `me` |
| `/` | course map with progress | `curriculum`, `progress` |
| `/lesson/*` | lesson pane, screen, rail; the rest of the path is the lesson id, which contains a slash | `lesson`, session |
| `/project/:projectId/:taskId?` | file tree and tasks, screen, preview and rail | `lesson` (projects share the format), session |
| `/session` | session status, frame mode, modules, restart, export, progress (Start over), preferences, bring-your-own-terminal instructions | `session`, `status` |

The session is ensured once (`POST /api/v1/sessions`) when any screen that needs Emacs mounts; the connection layer lives above the router so navigating between lessons keeps the WebSockets open.

Loading a lesson (`useLesson`) calls `rpc('scenario', {lessonId, stepId})` when the engine's active step is not already in this lesson. The step is the one the URL names, else the resume target (`resumeStep`): the first step that is neither done nor skipped by this visit's outcome or else the saved progress status (a watched step is resumed, so the learner practises it). The target is chosen from freshly fetched progress, so a progress reset from another tab does not make this page reload at a stale step. A `dropped` step event (a progress reset) clears the active step and asks for a new load. GUI lessons (`mode: gui` or tagged `gui`) switch to the GUI frame after loading (§8).

## 3. State

`useSessionStore` (Zustand) holds everything the event stream drives. The important fields:

| Field | Meaning |
|---|---|
| `connection`, `hello`, `state` | socket states, the `hello` event, the latest agent `State` (protocol.gen.ts) |
| `prefix`, `lastKeyTs` | pending prefix from the tap; server time of the newest `key` event (an older `prefix` event is stale and ignored) |
| `ticker` | entries oldest first, max 40; cleared with the pending prefix and the habit cards when a step of another lesson loads |
| `checks`, `activeStep` | latest `check` per step id (active lesson only); the engine's active step |
| `outcomes`, `lessonsComplete` | this visit's outcome per lesson and step (`done`, `demonstrated`, `skipped`); a step that loads again loses its outcome |
| `watched` | the active step was demonstrated: it shows "Do it yourself" until reset or reloaded |
| `demoPlaying`, `quietHabitsUntil` | a demo or skip is playing; habit cards stay quiet while it plays and for a short tail after (the agent's key events trail the player's keys), with no tail when the learner's own keypress stopped it |
| `mode`, `modules`, `emacs`, `echo`, `echoLive` | frame kind, module set, supervisor overlay and busy flag, latest echo text (`echoLive` until the next key) |
| `serverNudges`, `cards`, `dismissed`, `habitsSeen`, `mentions` | nudge inputs (§6); `habitsSeen` persists in localStorage per session |
| `files`, `checkpointsVersion`, `treeVersion` | `file` events for previews; counters that make checkpoint lists and the file tree refetch |
| `tables`, `described` | advice tables from the curriculum index (defaults until loaded); `rpc('describe')` glosses |

Actions: `applyEvent(ev)` (the single reducer for `/events`, pure apart from the store write and unit-tested with recorded streams), `dismiss`, `setDemoPlaying`, `markWatched`, `forgetProgress(lessonId?)` (Start over: forgets outcomes, completions, checks, the watched latch and, for all lessons, habit keys seen, cards and dismissals), `note` (synthetic ticker entries for Rescue and Reset).

Derived data comes from memoised selectors: `selectWhereAmI`, `selectNudges`, `selectTicker`, `selectStepView`, `resumeStep`. `useUiStore` holds page state: rail and drawer, preview tabs, focus mode and keyboard lock, hint levels per `<lessonId>#<stepId>`, the playing demo and its progress, the running skip, GUI starting, toasts, and persisted preferences (locale, reduced motion, screen-reader mode).

Server state (curriculum, progress, me, session metadata) goes through TanStack Query; progress is invalidated on every `step` event. The course map renders its rows once progress has arrived, so lessons never flash "not started".

## 4. Connection layer

- `EventsClient`: opens `/s/{id}/events?after=<lastSeq>`, dispatches each JSON frame to `applyEvent`, reconnects with backoff 250 ms to 8 s with jitter, sends `ping` every 20 s; a `gap` event triggers `rpc('check')` and a divider in the ticker.
- `PtyClient`: binary WebSocket; `term.onData` → `TextEncoder` → send; incoming bytes → `term.write`; `ResizeObserver` on the pane → fit addon → `{"t":"resize"}` control frame (debounced). A `replaced` frame (or close 4000) shows "attached elsewhere" with a Reattach button.
- `VncClient`: lazily imports noVNC; `resizeSession = true`, so noVNC asks Xvnc for the pane's size and the GUI frame is drawn at scale 1 (sessiond runs Xvnc with `-AcceptSetDesktopSize=1`; the Emacs side fits the frame to the X screen). Resizes are debounced 200 ms with `resizeSession` held off meanwhile; a hidden (zero-size) pane asks for nothing. `scaleViewport = true` is the fallback when the server refuses a size; `clipViewport = false`, `showDotCursor = true`. The remote clipboard goes to the browser clipboard; a paste into the focused pane goes to the remote clipboard.
- `rpc`: typed wrapper over `POST /s/{id}/rpc/{op}`; errors become toasts unless the caller passes `silent` and handles them.

All four read the session id from the store; the path prefix `/s/{id}` is the only thing that differs between local and server mode.

## 5. Terminal pane

xterm.js options: `fontFamily: '"Iosevka Nerd Font", "JetBrains Mono", monospace'`, `fontSize: 14`, `lineHeight: 1.2`, `theme` from the modus-vivendi-tinted palette used in the demo, `allowProposedApi: true`, `scrollback: 0` (tmux owns history; Emacs owns scrolling), `macOptionIsMeta: true`, `cursorBlink: false`, `vtExtensions: { kittyKeyboard: true }`, `screenReaderMode` from preferences. Addons: fit, webgl (disposed on context loss, falling back to the DOM renderer), unicode11 (`term.unicode.activeVersion = '11'` for CJK and icon widths), clipboard (OSC 52; Emacs kills reach the browser clipboard when the page has focus, refusals are ignored), web-links (opens in a new tab). The terminal waits for `document.fonts.ready` before the first `fit()` so cell metrics are right. `window.__pgScreenText()` returns the visible text for browser tests, since the screen is a canvas.

Keyboard: xterm.js encodes keys; the app does not decode them (sessiond's tap does). tmux never forwards Emacs's kitty-keyboard push, so xterm.js stays in the legacy encoding, and `extkeys.ts` sends CSI u (`ESC [ code ; mods u`) itself for exactly the chords legacy cannot carry: Ctrl with a printable key outside the legacy set (`C-;`, `C-=`, `C-?`, `C->`, `C-1`), Ctrl+Shift+letter (`C-S-a`), `C-RET`, `S-RET` and `C-DEL`. tmux (`extended-keys always`, `csi-u`) passes them to Emacs. The custom key handler otherwise does only this:

- `Shift+Escape` moves focus from the terminal to the lesson pane (a reserved chrome key, unbound in Emacs), leaving focus mode first.
- Outside focus mode, a browser-reserved combination (`Ctrl+W`, `Ctrl+T`, `Ctrl+N`, `Ctrl+Shift+*`, `Alt+letter` on Firefox) shows a one-time toast pointing at Focus mode.
- While "Skip this step" plays, the learner's keys are swallowed.
- A real keydown (not a lone modifier) or a paste counts as learner input and stops a playing demo. `onData` does not signal input: it also carries focus reports, mouse reports and OSC 52 replies.

Focus mode is an in-page state first: the stage takes the whole width (lesson pane and rail hidden) and a one-line banner (`role="status"`) says what the mode does, whether the keyboard is locked, and how to leave (the banner's button, the top-bar button, or Shift+Esc). Then `requestFullscreen()` and `navigator.keyboard.lock()` are tried where available (Chromium), each giving up after 1.5 s (a headless or embedded browser may never answer). Leaving a fullscreen that focus mode entered ends focus mode. The top-bar button carries the description as a tooltip and through `aria-describedby`.

Overlays over the pane: frame closed, Emacs restarting, Emacs busy, attached elsewhere, reconnecting, and a veil while "Skip this step" plays ("Emacs is doing this step for you…"). They are driven by `emacs` events and connection state and mirror the demo's overlay card.

Nothing in the stage changes size except the window: the screen header is one line of fixed height (long text ends in an ellipsis, chips shrink before the size label), and the strip under the screen has a fixed height whether it shows the ticker or the demo caption, so the terminal is never refitted while a demo plays.

## 6. Live panels and the advice engine

The panels are pure views over the store plus the tables from the curriculum index (`tables.exits`, `tables.habits`, `tables.gloss`, `tables.cheats`; format in CURRICULUM.md B.4).

**Ticker** (`ticker.ts`). Entries `{keys, cmd?, gloss?, kind: 'ok'|'prefix'|'unbound'|'quit'|'gap', mx?, at, typed?, count?}`. A `prefix` event adds or replaces the pending entry; a `key` event removes pending entries and appends. An unbound key arrives as `cmd: "undefined"`. Consecutive typing commands (`self-insert-command`, `isearch-printing-char`, `org-self-insert-command`, `term-send-raw`) collapse into one entry that grows in place: "typed N characters" with the last 12 in quotes. Key events made only of mouse events (`<mouse-1>`, `<down-mouse-1>`, drags, the wheel, also after an area such as `<mode-line>`) are not shown and do not count as commands: one click to focus the terminal would otherwise add two entries. Glosses come from `tables.gloss`, then from `rpc('describe')` (cached per session), clamped to two lines. Newest entry first, emphasised; six visible. The ticker belongs to the lesson (§3); a frame switch clears the pending prefix.

**WhereAmI** (`exits.ts`). Evaluates `tables.exits` rows in order against the state document (`protocol.StateDoc` plus the virtual root `session`) and shows every matching row as a reason (`why`) and one exit (`how`), with `{path}` templates expanded and keys rendered as keycaps. The browser fills `session` from the event stream (`protocol.ExitSession`): `session.prefix` (pending prefix or null), `session.emacs` (`frame-closed`, `suspended`, `restarting`, `unresponsive` or null), `session.mode` (`terminal`/`gui`), `session.echo` (latest echo text until the next key, or null). Only the browser evaluates `session.*`; step conditions cannot use it. Built-in live rows cover the same facts for tables without them; when the table has `session.prefix` or `session.emacs` rows, they replace the built-in rows for that fact. The Mode row omits housekeeping minor modes (`HOUSEKEEPING_MINORS`, e.g. `ws-butler-mode`).
Rows' `regex` operators are RE2 (Go is the reference); `predicate.ts` translates them for JavaScript (`translateRe2`): a leading `(?i)`, `(?m)`, `(?s)` becomes RegExp flags, scoped `(?i:…)` and `(?s:…)` groups are spelled out (letters in both cases, `.` as `[\s\S]`), `(?P<name>…)` becomes `(?<name>…)`, `\p{…}` adds the `u` flag. Anything else JavaScript cannot express is a validation error, never a row that silently fails to match.

**Nudges** ("Did you mean"). Three sources merged into one list, minus dismissed ids:

- Habit cards (`habits.ts`): one card per habit key per session when a single-key `key` event matches `tables.habits`. Seen keys persist in localStorage (`pg.habitsSeen.<sessionId>`) so "once each" survives reloads; Start over clears them. No card while a demo or skip plays (`quietHabitsUntil`).
- Fixed patterns (`patterns.ts`): three quits within 8 s; the same unbound key twice in a row; typing into a read-only buffer; a prompt open for 20 s; three or more windows after the learner split one (windows that pop up, such as a build's output, do not count).
- Server `nudge` events (the scenario's stuck rules).

Habit cards and the many-windows pattern are suppressed for keys the lesson mentions (`mentions.ts`): the key sequences in the lesson Markdown (backticked keys, `:kbd[…]`, `:try[…]`) and in the current step's goal, detail and condition hints, normalised. A sequence is cut into commands after every key that is not a prefix (`PREFIX_KEYS`), and a card for key K is suppressed when K starts one of those commands: `C-s` in lesson 1.4 suppresses "C-s is not save", `C-x C-s` does not, and `C-a C-k` suppresses both `C-a` and `C-k`. A suppressed card does not count as seen. The many-windows pattern stays quiet when the lesson mentions `C-x 2` or `C-x 3`. When a lesson writes keys after a new global prefix, add the prefix to `PREFIX_KEYS`.
Cards have no command TTL: they stay until dismissed or until another lesson loads. The panel shows three, newest first, with older ones behind "N earlier tips". Dismissing removes exactly that card for the page session. The rail puts Rescue above "Did you mean", so cards never push it below the fold.

**CheatSheet**: rows for the current major mode from `tables.cheats`, then the `common` rows.

**RescueCard** (`actions.ts`):

- Rescue: `rpc('rescue')`; "Never touches your files".
- Reset this step ("Puts the files and windows back to how this step started; changes since then are lost."): a "Reset this step?" dialog, then `rpc('restore', {})`, which also clears the demo latch.
- "I'm stuck" (when `features.stuckReports` is on): files a report through `POST /api/v1/stuck`; once sent, the dialog offers Skip this step (§7) and Close.

Rescue and Reset add a synthetic ticker entry. Toasts appear top centre under the top bar, so they never cover the ticker.

`keys.ts` holds `KEY_RE` and `keyify` from the demo, used to render keycaps in hints, exits and nudges.

## 7. Lesson pane

Markdown rendering: `react-markdown` with `remark-gfm`, `remark-directive` and `directives.ts`, which maps the lesson dialect (CURRICULUM.md B.2) to components: `:kbd[…]` → `<Kbd>`, `:try[…]` → `<TryKeys>` (sends `rpc('keys')`), `:jargon[…]` → glossary tooltip, `:::vscode`, `:::aside`, `:::gui-only` containers, `::steps` placement marker. Backticked text that parses as a key sequence also renders as keycaps. Unknown text and leaf directives turn back into their source text, so prose like "Keys:none" survives. The Markdown is parsed once per lesson.

**Steps.** `<Steps>` renders the scenario's steps with the latest `check` results (conditions met, unknown, or next). Statuses (`stepStatuses`) follow the engine:

- The engine's active step of this lesson is always shown active, whatever progress says. The last step stays active after it completes; only an ending seen in this visit (done or skipped) shows over it.
- Steps after the active step show no tick (they are evaluated again when reached).
- Other steps show this visit's outcome, else their saved status when it is done, demonstrated or skipped, else to do.

Done steps have a filled tick, watched (demonstrated) ones a hollow check and "watched", skipped ones an arrow and "skipped". Inactive steps offer "Go to this step" or, when done, "Do this step again" (`rpc('scenario', {lessonId, stepId})`; sessiond restores that step's checkpoint or fast-forwards, api.md §3.4).

Hints: "Show a hint" raises the step's hint level and calls `rpc('hint', {stepId, level})` so progress counts it. Level 1 shows the first unmet condition's hint; level 2 ("Show the exact keys") adds the keys of the step's demo.

While the active step is scrolled out of the pane (long lessons put `::steps` below the fold), a chip at the pane's bottom edge names it ("Current step: …") and scrolls to it. When a step ends, its tick pops and the step is highlighted for 600 ms, then the next step scrolls into view and a polite live region announces "Step done: …"; under reduced motion there is no highlight and the scroll is instant. A finished lesson offers "Start this lesson over": `POST /progress/reset {lessonId}`, then `rpc('scenario', {lessonId, reset: true})`.

**Watch it** (`DemoPlayer`, `demoPlayer.ts`) is a demonstration, not a completion:

1. `rpc('demo', {stepId, phase: 'start'})`: sessiond restores the step's checkpoint when the learner already ran commands in it (`restored: true`; the caption then says "Starting from the beginning of this step." and the player waits 900 ms) and latches the step, so a completion is recorded `demonstrated`, not done. `409` (not the active step) aborts with a toast.
2. The step's `demo` string is parsed (`lib/demo.ts`, a port of `protocol.ParseDemo`) and sent through `rpc('keys')` one chord or typed character at a time at human pace: 600 ms per chord, 120 ms per character, `~N` pauses as written (`WATCH_PACING`). playctl, Skip and the fast-forward use the faster 170/70 ms pacing (`REPLAY_PACING`).
3. `rpc('demo', {phase: 'end'})` follows in any case.

While it plays, the strip under the screen shows the caption instead of the ticker (`role="status"`, `aria-label="Demo caption"`): "Watching: <goal>", the keys sent so far with the current one highlighted, typed text growing inside quotes, and Stop. Any learner keypress or paste in the terminal stops it (§5). Afterwards the step stays active with a hollow check, "You watched this step. Now do it yourself." and a primary Do it yourself button: `rpc('restore', {})`, which resets the step and clears the latch. Keys sent by inline "try it" buttons are learner input and complete steps as done.

**Skip this step**: `rpc('skip', {stepId})` makes sessiond play the step's demo itself (up to a minute) and record it `skipped`; the veil covers the screen and swallows the learner's keys until it returns, then a toast says "Skipped: <goal>". It is offered after an "I'm stuck" report (§6).

## 8. GUI pane

Shown when `mode === 'gui'`; GUI lessons switch to it on load. The frame switch calls `rpc('mode', {mode})`; its GUI half has a tooltip saying what the graphical frame is ("the same Emacs, with the same buffers, drawn with images and mouse menus"). While the GUI frame starts, the terminal stays visible with a one-line "Starting…" chip until the `mode` event arrives (after 20 s it gives up with a toast). Both panes stay mounted with only one visible, so switching back is immediate. The X screen follows the pane's size (§4), so the frame is drawn at scale 1.

## 9. Preview pane

Tabs come from the scenario's `preview.tabs` plus files the learner opens from the file tree. `preview.follow` maps a source file to the artefact shown while that source is the current buffer (`docs/report.typ` → `docs/report.pdf`). Views: `PdfView` (pdfjs-dist, worker loaded lazily; renders visible pages to canvases; keeps scale and scroll across reloads), `HtmlView` (iframe with `sandbox="allow-scripts"`; sessiond also sends a sandboxing CSP), `MarkdownView` (same pipeline as lessons, without directives), `ImageView`. Views reload on `file` events for their path; `PreviewStatus` shows the last `file` event for the artefact and the time since.

## 10. Course map and projects

The course map renders `curriculum.parts` and `projects` with progress bars from `progress`. Order is recommended, not enforced: a part shows "suggested after part N" rather than a lock. Lesson rows:

| Progress | Row |
|---|---|
| every step done | "done" |
| every step done, watched or skipped, not all done (from `counts`) | "finished · N skipped · M watched" with a hollow mark |
| otherwise | open; "· N skipped" when there are any |

Continue skips lessons whose steps are all done or skipped (a watched step is practised). **Start over** sits next to Continue once there is progress, and in the Progress section of the Session page: a "Start over?" dialog ("This clears the progress of every lesson: ticks, hints and resets. Your files and Emacs stay as they are."), then `POST /api/v1/progress/reset` with no body. The page forgets its own memory of that progress (`forgetProgress`, hint levels) and sessiond's `dropped` step event clears the active step.

Project screens use the same lesson format with the file tree (`GET /preview/<root>/` listings; clicking a file calls `rpc('open', {path})`), the checkpoint list (`rpc('checkpoints')`) and Restore (`rpc('restore', {label})` after a confirmation dialog that names what will be overwritten).

## 11. Styling

`tokens.css` carries the palette and type scale from the demo (paper, ink, violet, screen, mode line, ok, warn, err; Source Serif 4 for prose and interface, Iosevka or JetBrains Mono for keys and the screen; fonts served by the gateway under `/fonts/`). Components use CSS Modules; no utility framework. Layout is the demo's three-column grid collapsing at 1440 px and 860 px: at 1440 px and below the rail becomes a drawer behind the "Where am I" button, so the terminal keeps its width (129 columns at 1280 px). Motion only on state changes (step done, card appears), disabled under `prefers-reduced-motion` or the reduced-motion preference. A dark chrome theme is a token swap left for later.

## 12. Internationalisation

UI strings in `i18n/*.json` through `react-i18next`. Lesson content is per file (`lesson.zh-CN.md`) and tables per locale (`exits.zh-CN.yaml`), served by the gateway according to `Accept-Language` or the user's choice. Key names and command names are never translated.

## 13. Accessibility

The terminal is a canvas; everything it conveys about state is also text: the ticker (`aria-live="polite"`), WhereAmI and the echo line mirrored into a visually hidden live region (`LiveAnnouncer`). xterm.js `screenReaderMode` is a preference. All chrome is keyboard-operable with visible focus rings; `Shift+Escape` leaves the terminal; dialogs trap focus; colour contrast meets WCAG AA on paper and on the screen theme (the modus palette is designed for it).

## 14. Testing and quality

- Unit (Vitest, jsdom): `advice/*`, `ticker.ts`, `applyEvent`, the directive plugin, `keys.ts`, `lib/demo.ts`, against event streams recorded from the fake session's mock Emacs (`src/test/recorder.ts`).
- Component (Testing Library, MSW, `vitest-websocket-mock`): `LessonScreen` with a fake events WebSocket and mocked REST; asserts step rendering, hint levels, overlays.
- Fake session (`web/dev/`, `pnpm dev:fake`): stands in for the gateway and one session without Docker; implements the step contracts (demo latch, skip, restore, progress reset). `e2e/lesson.mjs` walks a lesson against it.
- Browser scripts (`playwright-core` over CDP against the running compose stack; see AGENTS.md for the Chrome container): `e2e/smoke.mjs` runs the integration scenarios (course map, lesson 2.3 with the ticker, Where am I, Watch it then Do it yourself, Reset this step, the GUI frame, the Markdown preview, Rescue, `playctl curriculum play` replays) and checks `/events` frames and workspace files; `e2e/review.mjs` is the acceptance suite for the UX contracts in this document. Both reset progress when they end.
- Types: `protocol.gen.ts` is regenerated by `make protocol`; `go test ./internal/protocol/...` fails on drift.
- Lint: typescript-eslint recommended plus `react-hooks`, `jsx-a11y`; Prettier.

## 15. Build and development

`pnpm dev` runs Vite on `:5173` proxying `/api/`, `/s/` (WebSockets too), `/fonts/` and `/internal/` to `$PG_GATEWAY` or `http://127.0.0.1:8080` (`playctl dev` starts the gateway and Vite together). `pnpm build` emits `web/dist`, which the gateway embeds with `-tags embedweb`; the gateway Dockerfile builds the bundle in a Node stage first. Code splitting: noVNC, pdfjs, the course map, project and session screens are lazy; the lesson screen with xterm.js is the main chunk. Budget: 600 KB gzipped for the JavaScript loaded up front, enforced by `pnpm size` (`dev/size-gate.ts`).

## 16. Performance budgets

Event to ticker render under 16 ms (one frame); `applyEvent` allocation-free for `key` and `prefix`; selectors memoised by state identity; ticker and WhereAmI render only when their inputs change (store slices). The terminal uses the webgl renderer; `fit()` is debounced. The lesson Markdown is parsed once per lesson.
