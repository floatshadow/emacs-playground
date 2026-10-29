# Frontend (React + Vite)

The browser app: course map, lesson and project screens around a live Emacs. It renders what sessiond and the agent report and never evaluates step conditions (only the exits table, §6). Visual reference: `../design/demo/`. Server contracts: api.md.

## 1. Stack and layout

React 19, TypeScript, Vite, TanStack Query (server state), Zustand (live state), xterm.js, noVNC, pdfjs-dist, react-markdown, react-i18next, CSS Modules. Versions: third-party.md §6.

`web/src/`: `protocol.gen.ts` (generated from Go, never edited), `api/` (HTTP, queries, `session/` clients, §4), `store/` (§3), `advice/` (§6), `components/`, `screens/`, `i18n/`, `lib/`. `web/dev/` is the fake session, `web/e2e/` the browser scripts; tests sit next to their sources.

## 2. Screens and routes

| Route | Screen |
|---|---|
| `/login` | invite code or OIDC |
| `/` | course map (§10) |
| `/lesson/*` | lesson pane, screen, rail; the rest of the path is the lesson id (it contains a slash) |
| `/project/:projectId/:taskId?` | file tree, tasks, screen, preview, rail |
| `/session` | status, frame mode, modules, restart, export, Start over, preferences |

The connection layer sits above the router, so navigation keeps the WebSockets open.

Loading a lesson (`useLesson`) calls `rpc('scenario', {lessonId, stepId})` unless the engine's active step is already in this lesson. The step is the URL's, else `resumeStep`: the first step neither done nor skipped (this visit's outcome, else freshly fetched progress, so another tab's reset counts; a watched step is resumed for practice). A `dropped` event clears the active step and the lesson's outcomes and reloads. GUI lessons (`mode: gui` or tag `gui`) switch to the GUI frame after loading, a `mode: gui` step when it becomes active; other lessons and tasks switch back to the terminal unless the learner picked the GUI frame.

## 3. State

`useSessionStore` (Zustand) is driven by `/events` through one reducer, `applyEvent`, tested with recorded streams:

- A `prefix` event older than the newest `key` event is stale and ignored.
- The ticker (max 40), pending prefix and habit cards are cleared when a step of another lesson loads.
- The engine's last step stays active after the lesson completes, so the lesson is not reloaded.
- `outcomes` are this visit's `done`/`demonstrated`/`skipped`; a step that loads again loses its outcome.
- `watched` is the Watch it latch (§7); `quietHabitsUntil` keeps habit cards quiet during a demo, Skip or Go to this step and a short tail after (none when the learner's key stopped it).
- `forgetProgress(lessonId?)` (Start over) forgets outcomes, completions, checks, the latch and, for all lessons, habits seen and cards.

`useUiStore` holds page state and persisted preferences. Progress is invalidated on every `step` event and refetched when the page becomes visible; the course map renders rows only once progress has arrived.

## 4. Connection layer

- `EventsClient`: `/s/{id}/events?after=<lastSeq>`, backoff 250 ms to 8 s with jitter, `ping` every 20 s; `gap` → `rpc('check')` and a ticker divider.
- `PtyClient`: binary stream; `ResizeObserver` → fit → debounced `resize` frame; `replaced` (or close 4000) shows "attached elsewhere" with Reattach.
- `VncClient`: lazy noVNC with `resizeSession = true`, so Xvnc takes the pane's size and draws at scale 1 (emacs.md §6); resizes debounced 200 ms, none for a zero-size pane; clipboard both ways.
- `rpc`: typed `POST /s/{id}/rpc/{op}`; errors become toasts unless `silent`.

## 5. Terminal pane

xterm.js: Iosevka Nerd Font, `scrollback: 0` (Emacs scrolls), `macOptionIsMeta`, `vtExtensions.kittyKeyboard`, `screenReaderMode` from preferences; the first `fit()` waits for `document.fonts.ready`. Addons: webgl (dropped on context loss; not loaded when `ResizeObserver` reports CSS pixels at a device pixel ratio above 1, where it draws magnified), unicode11, clipboard (OSC 52 when focused), web-links. `window.__pgScreenText()` exposes the visible text to browser tests.

Keys: xterm.js encodes, sessiond's tap decodes. tmux never forwards Emacs's kitty-keyboard push, so xterm.js stays legacy and `extkeys.ts` sends CSI u itself for exactly the chords legacy cannot carry: Ctrl with a printable outside the legacy set (`C-;`, `C-=`, `C->`, `C-1`), Ctrl+Shift+letter, Ctrl+Alt with a legacy symbol (`C-M-_`, `C-M-/`), `C-RET`, `S-RET`, `C-DEL`. Plain Ctrl+/ stays the legacy `C-_` byte (habit cards treat it as `C-/`). Otherwise the key handler only:

- `Shift+Escape` moves focus to the lesson pane (leaving focus mode).
- Outside focus mode, a browser-reserved chord (`Ctrl+W`, …) shows a one-time toast about focus mode.
- Swallows keys while Skip or Go to this step plays, and while Emacs restarts (`restarting` until `daemon-restarted`; for modules until `modules {restarting: false}`, after the lesson's setup).
- Counts a real keydown (not a lone modifier) or a paste as learner input, which stops a demo (`onData` also carries focus, mouse and OSC 52 reports).

Focus mode is in-page first: the stage takes the full width and a `role="status"` banner says whether the keyboard is locked and how to leave (its button, the top-bar button, Shift+Esc). Then document fullscreen and `navigator.keyboard.lock()` are tried, each abandoned after 1.5 s; leaving that fullscreen ends focus mode.

Overlays mirror `emacs` events and connection state. Only the window changes the stage's size: the screen header is one line and the strip under the screen keeps its height for ticker or caption, so a demo never refits the terminal.

## 6. Live panels and the advice engine

Views over the store and the index tables (CURRICULUM.md B.4).

**Ticker** (`ticker.ts`): a `prefix` event adds or replaces the pending entry; a `key` event replaces it with the command (`undefined` is unbound; a prefix ended by `C-g` is "cancelled"). Prefix arguments fold into their command (`C-u 4 0 =` is one entry); consecutive typing collapses into "typed N characters" (in a terminal buffer only printable keys count); mouse-only events are not shown; entries made while keys are quiet are `replayed` and ignored by stuck patterns. Glosses: `tables.gloss`, else `rpc('describe')`. A frame switch clears the pending prefix.

**Where am I** (`exits.ts`): `tables.exits` rows in order against `protocol.StateDoc` plus the browser-only root `session` (CURRICULUM.md B.4); each match shows `why` and one `how` with templates expanded. Built-in rows cover `session.prefix` and `session.emacs` unless the table has rows for them; the Mode row hides `HOUSEKEEPING_MINORS`. The cheat sheet (`cheats.ts`) adds rows for enabled minor modes and points at `C-h m` otherwise; mode facts live in `modes.ts`. `regex` is RE2; `translateRe2` (`predicate.ts`) accepts leading or scoped `(?i)`/`(?m)`/`(?s)` flags, `(?P<name>…)` and `\p{…}`, and anything else is a validation error (as in `validate.py`), never a row that silently fails.

**Nudges** ("Did you mean"): habit cards, fixed patterns and server `nudge` events.
- Habit card: once per habit key per session (localStorage `pg.habitsSeen.<sessionId>`, cleared by Start over), on a single-key event; never for keys a terminal buffer sends to its program or while keys are quiet.
- Patterns (`patterns.ts`): three quits in 8 s; the same unbound key twice; typing into a read-only buffer (not into a prompt over a read-only preview); a prompt open 20 s; three or more windows after the learner split one (pop-ups, side windows, `*name*` buffers and tool modes such as Magit or the agenda do not count).
- Suppression (`mentions.ts`): a key is mentioned when it starts a command of a key sequence in the lesson Markdown or the step's goal, detail or hints; sequences split after every key not in `PREFIX_KEYS` (add new global prefixes there), so `C-a C-k` mentions both and `C-x C-s` not `C-s`. Suppressed cards do not count as seen. Many-windows is quiet when `C-x 2` or `C-x 3` is mentioned.
- Cards stay until dismissed or another lesson loads; three show, older ones behind "N earlier tips"; Rescue sits above them.
- Limits: suppression sees only the current lesson and step; habits match single keys only (VS Code's Ctrl+G gets no card although `C-g 35` inserts "35").

**Rescue card** (`actions.ts`): Rescue (`rpc('rescue')`, never touches files); Reset this step (confirmed, then `rpc('restore', {})`, which clears the latch); "I'm stuck" (`features.stuckReports`) posts `/api/v1/stuck`, then offers Skip this step.

## 7. Lesson pane

Markdown goes through `directives.ts` (dialect in CURRICULUM.md B.2): `:kbd` keycaps, `:try` → `rpc('keys')`, `:jargon`, `:::vscode`/`:::aside`/`:::gui-only`, `::steps`. Backticked key sequences render as keycaps; unknown directives revert to source text.

**Step statuses** (`stepStatuses`): the engine's active step of this lesson is always active, whatever progress says (the last step stays active after completing unless this visit saw it end); later steps show no tick; others show this visit's outcome, else a saved `done`/`demonstrated`/`skipped`, else to do.

**Go to this step / Do this step again**: `rpc('scenario', {lessonId, stepId})` (checkpoint or fast-forward, api.md §3.4). For a step after the first: stop any demo, veil the screen ("Going to step N…"), swallow keys until the step is active, then refocus the terminal.

A lesson with every step done or skipped is not loaded on opening: it shows Lesson complete; "Start this lesson over" (`POST /progress/reset {lessonId}`, then `scenario {reset: true}`) or a step's Go to loads it. When another page made another lesson active, a page offers "Load this lesson here" instead of reloading.

Hints: level 1 is the first unmet condition's hint, level 2 the demo's keys; each calls `rpc('hint')` and refocuses the terminal. On completion the step is highlighted 600 ms, the next scrolls into view and a live region says "Step done: …" (no motion under reduced motion).

**Watch it** (`demoPlayer.ts`) demonstrates; it does not complete:
1. `rpc('demo', {stepId, phase: 'start'})`: if the learner already ran commands in the step, sessiond restores its checkpoint first (`restored: true`, said in the caption). The step is latched: a completion records `demonstrated`. `409` aborts.
2. The demo (`lib/demo.ts`, port of `protocol.ParseDemo`) goes through `rpc('keys')` at `WATCH_PACING` (600 ms/chord, 120 ms/char); replays use `REPLAY_PACING` (170/70).
3. `rpc('demo', {phase: 'end'})` always; if the page goes away, sessiond ends it (api.md §4.1).

The caption replaces the ticker while playing ("Watching: <goal>", keys so far, Stop). Learner input, another active step or leaving the lesson stops it. Afterwards the step offers Do it yourself (`rpc('restore', {})`), also after a reload while the active step is saved `demonstrated`. `:try` keys are learner input and complete steps as done. While latched, the learner's own completion gets no reaction until Do it yourself (open, DESIGN.md §12).

**Skip this step**: `rpc('skip', {stepId})`; sessiond plays the demo and records `skipped` while the veil swallows keys. It is offered only after an "I'm stuck" report, so `features.stuckReports: false` hides it (open).

## 8. GUI pane

Shown when `mode === 'gui'`; the frame switch calls `rpc('mode')`. While the frame starts the terminal stays with a "Starting…" chip (20 s, then a toast). Both panes stay mounted, one visible.

Lesson notices (`LessonNotices.tsx`): GUI lessons explain the GUI frame and offer to reopen it, or say the server has it off (`features.gui`); remote lessons (tag `remote` or `remote.box`) show `status.remoteBox`, polled every 5 s while false, or say the server has no box (`features.remoteBox`).

## 9. Preview pane

Tabs: `preview.tabs` plus files opened from the tree; `preview.follow` maps the current source buffer to its artefact. Views: PDF (lazy worker, keeps scale and scroll across reloads), HTML (iframe `sandbox="allow-scripts"`), Markdown (lesson pipeline without directives), image; they reload on `file` events. A HEAD probe gates every view: a missing artefact says so and, under `preview.follow`, which source builds it (the one being edited, else the last changed; a followed file nobody builds is never "rebuilding"); the `file` event that creates it shows the view. A PDF that fails to parse (still being written) is retried once after 800 ms. sessiond's `SandboxCSP` keeps `allow-same-origin`, an open question (deployment.md §9.3).

## 10. Course map and projects

A lesson row reads "done" when every step is done, "finished · N skipped · M watched" (hollow mark) when all are done, watched or skipped, else open with "· N skipped". Continue skips lessons whose steps are all done or skipped.

**Start over** (next to Continue once there is progress; always on the Session page) confirms that files and Emacs stay, posts `/api/v1/progress/reset` with no body and calls `forgetProgress`. Open: an open lesson tab then reloads with `setup.reset`, which discards unsaved edits under the lesson root.

Projects (`ProjectScreen.tsx`) show "Task N of M" with a progress bar (done, skipped, watched, from saved progress; a finished project with tasks the learner did not do says so), one file tree per `project.roots` (default `setup.root`; `GET /preview/<root>/`, a click calls `rpc('open', {path})`), and the checkpoint list (refetched after file changes): task checkpoints ("Start of task N: goal", restored by `{ref}`) and manual ones; Restore's dialog fetches and lists the files changed since.

Once every task is done or skipped the project offers **Again, without hints** (`lap.ts`): `rpc('scenario', {lessonId, lap: true})`; while `lap` is set (ScenarioResult, StatusResult, HelloEvent) hints, Watch it, Go to and Skip are hidden (sessiond answers 409) and a timer runs from `lap.startedAt` against `project.lapMinutes`. The result is `lapMs` on the completing step event; Stop the lap reloads the task without it. Finished laps (count, best, last) come from `progress.laps`.

## 11. Styling

`tokens.css` holds the demo's palette and type (Source Serif 4; Iosevka for keys and screen). Breakpoints: at 1440 px the rail becomes a drawer behind "Where am I" (129 columns at 1280 px); at 1180 px the session chip hides and crumbs, then the brand, shrink so "Where am I" and "Rescue me" stay visible; 860 px is the narrow layout. The drawer returns focus to its opener. Motion only on state changes.

## 12. Internationalisation

Preferences offer only locales covering every key of `en.json` (zh-CN covers about a fifth and is hidden; some `advice/` strings are English literals); a stored locale not offered falls back to English. Lesson files and tables are per locale (`lesson.zh-CN.md`, `exits.zh-CN.yaml`), chosen by the gateway. Key and command names are never translated.

## 13. Accessibility

What the canvas shows is also text: the ticker (`aria-live`), Where am I and the echo line (`LiveAnnouncer`). Dialogs trap focus; contrast meets WCAG AA.

## 14. Testing

- Unit and component (Vitest, jsdom, MSW), on streams recorded from the fake session.
- Fake session (`pnpm dev:fake`): gateway and session without Docker; `e2e/lesson.mjs` walks a lesson on it.
- Against the compose stack over CDP (AGENTS.md): `e2e/smoke.mjs` (integration) and `e2e/review.mjs` (this document's UX contracts); both reset progress at the end.

## 15. Build and development

`pnpm dev` runs Vite on `:5173`, proxying `/api/`, `/s/`, `/fonts/`, `/internal/` to `$PG_GATEWAY` or `127.0.0.1:8080`. noVNC, pdfjs and every screen but the lesson are lazy chunks. Budget: 600 KB gzipped up front (`pnpm size`).

## 16. Performance budgets

Event to ticker render within one frame; `applyEvent` allocation-free for `key` and `prefix`.
