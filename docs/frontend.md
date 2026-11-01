# Frontend (React + Vite)

The browser app: course map, lesson and project screens around a live Emacs. It renders what sessiond and the agent report and never evaluates step conditions (only the exits table, §6). Visual reference: `../design/demo/`. Server contracts: api.md.

## 1. Stack and layout

React 19, TypeScript, Vite, React Router, TanStack Query (server state), Zustand (live state), xterm.js, noVNC, pdfjs-dist, react-markdown, react-i18next, CSS Modules. Versions: third-party.md §6.

`web/src/`: `protocol.gen.ts` (generated from Go, never edited), `api/` (HTTP, queries, `session/` clients, §4), `store/` (§3), `advice/` (§6), `components/`, `screens/`, `i18n/`, `lib/`, `test/`. `web/dev/` is the fake session, `web/e2e/` the browser scripts; tests sit next to their sources.

## 2. Screens and routes

| Route | Screen |
|---|---|
| `/login` | "Sign in with <displayName>" (OIDC), back to the requested page; a message per `?error=` (`state`, `provider`, `token`, `forbidden`) |
| `/` | course map (§10) |
| `/lesson/*` | lesson pane, screen, rail; the rest of the path is the lesson id (it contains a slash) |
| `/project/:projectId/:taskId?` | file tree, tasks, screen, preview, rail |
| `/session` | status, frame mode, modules, restart, export, Start over, preferences; account (name, email, Sign out, following the logout `redirectUrl`) outside auth mode `none` |

The connection layer and the screen hosts sit above the router, so navigation keeps the WebSockets and the screen (§4). Lesson and project screens load their lesson through `useScenarioLoad` (§7) and pick the frame as §8 says.

## 3. State

`useSessionStore` (Zustand) is driven by `/events` through one reducer, `applyEvent`, tested with recorded streams:

- A `prefix` event older than the newest `key` event is stale and ignored.
- `lesson` is sessiond's lesson snapshot (api.md §4.1), the only source of the loaded lesson, active step, phase, attempt, outcomes, lap and busy op. Every `hello` replaces it whatever its rev (a restarted sessiond counts again) and clears the checks; a `lesson` event applies only when its `rev` is higher. `activeStep(s)` derives the step while the phase is `active` or `watching`; `incompatible` is a `hello` of another protocol.
- A `check` applies only for the snapshot's `attempt`; a new attempt clears the checks.
- An applied snapshot of another lesson clears the ticker (max 40), pending prefix and habit cards; `lapMs` on a lesson event sets `lapDone`.
- Side effects (progress invalidation, the completion transition, announcements) run only for applied lesson events: `onSessionEvent` never sees the others.
- `quietHabitsUntil` keeps habit cards quiet during a demo, Skip or Go to this step and a short tail after (none when the learner's key stopped it). `forgetProgress()` (Start over of everything) forgets habits seen, cards and dismissals.

`useUiStore` holds page state and persisted preferences (`pg.prefs`: locale, reduced motion, screen reader mode, the lesson pane's width), and per lesson the Read/Do view (this tab only). Progress is invalidated on every applied lesson event but `busy` and `phase`, and refetched when the page becomes visible; it is read for lessons the snapshot does not show (step ticks, the course map, whether a lesson is finished); the course map renders rows only once progress has arrived.

## 4. Connection layer

`connection.ts` ensures the session once (`POST /api/v1/sessions`) and owns the events stream; `hosts.ts` keeps one terminal host (xterm.js and `PtyClient`) and one GUI host (noVNC) that panes only move into their container.

- `EventsClient`: `/s/{id}/events?after=<lastSeq>`, backoff 250 ms to 8 s with jitter, `ping` every 20 s; `gap` → reconnect for replay and a ticker divider; every hello whose snapshot has a step requests `check`.
- `PtyClient`: binary stream; `ResizeObserver` → fit → debounced `resize` frame; `replaced` (or close 4000) shows "attached elsewhere" with Reattach.
- `VncClient`: lazy noVNC with `resizeSession = true`, so Xvnc takes the pane's size and draws at scale 1 (emacs.md §6); resizes debounced 200 ms, none for a zero-size pane; clipboard both ways.
- `rpc`: typed `POST /s/{id}/rpc/{op}`; errors become toasts unless `silent`.

## 5. Terminal pane

xterm.js: 14 px `TERMINAL_FONT` without ligatures, `scrollback: 0` (Emacs scrolls), `macOptionIsMeta`, `vtExtensions.kittyKeyboard`, `screenReaderMode` from preferences, a white cursor that blinks while the terminal has focus and is a solid block (`cursorInactiveStyle`) when it has not, and a selection in the Emacs region's colour (`theme.ts`, modus-vivendi-tinted ANSI colours). The first `fit()` waits for both faces of the terminal font (Iosevka Nerd Font from the gateway's `/fonts/`, the bundled JetBrains Mono) or 2.5 s; a face arriving later refits. Addons: webgl (dropped on context loss; not loaded when `ResizeObserver` reports CSS pixels at a device pixel ratio above 1, where it draws magnified), unicode11, clipboard (OSC 52 when focused), web-links. `window.__pgScreenText()` and `window.__pgScreenCellBg(row, col)` expose the visible text and a cell's background to browser tests.

Keys: xterm.js encodes, sessiond's tap decodes. tmux never forwards Emacs's kitty-keyboard push, so xterm.js stays legacy and `extkeys.ts` sends CSI u itself for exactly the chords legacy cannot carry: Ctrl with a printable outside the legacy set (`C-;`, `C-=`, `C->`, `C-1`), Ctrl+Shift+letter, Ctrl+Alt with a legacy symbol (`C-M-_`, `C-M-/`), `C-RET`, `S-RET`, `C-DEL`. Plain Ctrl+/ stays the legacy `C-_` byte (habit cards treat it as `C-/`). Otherwise the key handler only:

- `Shift+Escape` moves focus to the lesson pane (leaving focus mode).
- Outside focus mode, a browser-reserved chord (`Ctrl+W`, …) shows a one-time toast about focus mode.
- Swallows keys (`keysHeld`: also in `onData`, so a paste waits too) while the session is incompatible, the snapshot names a busy lesson op (`busy`, api.md §4.1), this page runs Skip or Go to, or Emacs restarts.
- Counts a real keydown (not a lone modifier) or a paste as learner input, which stops a demo (`onData` also carries focus, mouse and OSC 52 reports).

Keys reach Emacs only while the terminal has focus (after a click in the lesson prose, Ctrl+V and Alt+V stay with the page). Focused, Ctrl+V is `C-v` (`\x16`; text is pasted only by the browser's paste event: Cmd+V on macOS, the context menu; Ctrl+Shift+V is `C-S-v`) and Alt+V is `ESC v` (`M-v`); where the browser takes Alt+letter (Firefox's menu bar), typing `ESC v` is the fallback.

Focus mode is in-page first: the stage takes the full width and a `role="status"` banner says whether the keyboard is locked and how to leave (its button, the top-bar button, Shift+Esc). Then document fullscreen and `navigator.keyboard.lock()` are tried, each abandoned after 1.5 s; leaving that fullscreen ends focus mode.

Overlays mirror `emacs` events and connection state (a restart whose reason names modules says so). A `hello` of another protocol shows one message instead and nothing automatic runs: Stop (`DELETE /api/v1/sessions/{id}`, then reload) on a server, "Restart the stack (make local)" locally. A session that cannot start shows why with a Retry button; `group_quota_exceeded` names the group and its limit (also on `/session`). Only the window changes the stage's size: the screen header is one line and the strip under the screen keeps its height for ticker or caption, so a demo never refits the terminal.

## 6. Live panels and the advice engine

Views over the store and the index tables (CURRICULUM.md B.4).

**Ticker** (`ticker.ts`): a `prefix` event adds or replaces the pending entry; a `key` event replaces it with the command (`undefined` is unbound; a prefix ended by `C-g` is "cancelled"). Prefix arguments fold into their command (`C-u 4 0 =` is one entry); consecutive typing collapses into "typed N characters" (in a terminal buffer only printable keys count); mouse-only events are not shown; entries made while keys are quiet are `replayed` and ignored by stuck patterns. Glosses: `tables.gloss`, else `rpc('describe')`. A frame switch clears the pending prefix.

**Where am I** (`exits.ts`): `tables.exits` rows in order against `protocol.StateDoc` plus the browser-only root `session` (CURRICULUM.md B.4); each match shows `why` and one `how` with templates expanded. Built-in rows cover `session.prefix` and `session.emacs` unless the table has rows for them; the Mode row hides `HOUSEKEEPING_MINORS`. The cheat sheet (`cheats.ts`) adds rows for enabled minor modes and points at `C-h m` otherwise; mode facts live in `modes.ts`. `regex` is regex dialect 1 (CURRICULUM.md B.4); `compileDialect` (`predicate.ts`) compiles it with RE2's matches and throws on anything outside it, so such a row is a validation error, never a row that silently fails.

**Nudges** ("Did you mean"): habit cards, fixed patterns and server `nudge` events.
- Habit card: once per habit key per session (localStorage `pg.habitsSeen.<sessionId>`, cleared by Start over), on a single-key event; never for keys a terminal buffer sends to its program or while keys are quiet.
- Patterns (`patterns.ts`): three quits in 8 s; the same unbound key twice; typing into a read-only buffer (not into a prompt over a read-only preview); a prompt open 20 s; three or more windows after the learner split one (pop-ups, side windows, `*name*` buffers and tool modes such as Magit or the agenda do not count).
- Suppression (`mentions.ts`): a key is mentioned when it starts a command of a key sequence in the lesson Markdown or the step's goal, detail or hints; sequences split after every key not in `PREFIX_KEYS` (add new global prefixes there), so `C-a C-k` mentions both and `C-x C-s` not `C-s`. Suppressed cards do not count as seen. Many-windows is quiet when `C-x 2` or `C-x 3` is mentioned.
- Cards stay until dismissed or another lesson loads; three show, older ones behind "N earlier tips"; Rescue sits above them.
- Limits (open, DESIGN.md §12): suppression sees only the current lesson and step; habits match single keys only.

**Author panel** (`AuthorPanel.tsx`, only with `features.authorMode`): the active step's condition results, the exit rows that match and the raw state.

**Rescue card** (`actions.ts`): Rescue (`rpc('rescue')`, never touches files); Reset this step (confirmed, then `rpc('restore', {})`, which clears the latch); "I'm stuck" (`features.stuckReports`) posts `/api/v1/stuck`, then offers Skip this step.

## 7. Lesson pane

Markdown goes through `directives.ts` (dialect in CURRICULUM.md B.2): `:kbd` keycaps, `:try` → `rpc('keys')`, `:jargon`, `:::vscode`/`:::aside`/`:::gui-only`, `::steps`. Backticked key sequences render as keycaps; unknown text and leaf directives revert to source text, unknown containers render their content.

**Read and Do**: the pane shows the title and lede, then a switch. Read is the Markdown (`::steps`, or the end, is a button to Do); Do is the load state, the step list and the lesson-complete box, labelled "Do 2/5". The view is the learner's per lesson (`useUiStore.lessonTab`, this tab only); its default, decided once the snapshot and progress are in, is Read for a lesson not started (no outcome, not past step 1), else Do. No event switches it: a step that ends while the learner reads pulses the Do label and is announced. In Do the active step shows everything and the others their goal, status and Go to (a click on the goal expands one); author mode expands all.

**Step statuses** (`stepStatuses`): while the snapshot shows this lesson, its step is active, later steps show no tick and the others its `outcomes`; otherwise saved progress decides. Complete is the snapshot's phase `complete` while it shows this lesson, else every step done or skipped in progress.

**Loading** (`useScenarioLoad`): one request per navigation intent (lesson, step, Retry), sent once `hello` arrived, the protocol matches, no lesson op is busy and the snapshot shows another lesson or step. Without a step it sends `scenario {lessonId}` and sessiond resumes the lesson; a lesson finished in an earlier visit (progress) is not loaded: it shows Lesson complete, and "Start this lesson over" (`POST /progress/reset {lessonId}`, then a load with `reset`) or a step's Go to loads it. A failed load waits for Retry. When the snapshot no longer shows the lesson this page loaded (another tab or window, or a progress reset elsewhere) it says so and offers "Load this lesson here" instead of reloading.

**Go to this step / Do this step again** (`loadAtStep`): `rpc('scenario', {lessonId, stepId})` (checkpoint or fast-forward, backend.md §3.12). For a step after the first: stop any demo, veil the screen ("Going to step N…"), swallow keys until the step is active, then refocus the terminal. A load of a later step that another page started, or that ran before a reload, shows the same veil from the snapshot's `busy` until the snapshot that clears it.

Hints: level 1 is the first unmet condition's hint, level 2 the demo's keys; each calls `rpc('hint')` and refocuses the terminal. On completion the step is highlighted 600 ms, in Do the next scrolls into view, and a live region says "Step done: …" (no motion under reduced motion).

**Watch it** (`demoPlayer.ts`) demonstrates; it does not complete:
1. `rpc('demo', {stepId, phase: 'start'})`: if the learner already ran commands in the step, sessiond restores its checkpoint first (`restored: true`, said in the caption). The step is latched (phase `watching`): a completion records `demonstrated`. The result names the `attempt`; `409` aborts.
2. The demo (`lib/demo.ts`, port of `protocol.ParseDemo`) goes through `rpc('keys')` at `WATCH_PACING` (600 ms/chord, 120 ms/char; sessiond's pacing, api.md §6, is `REPLAY_PACING` in `lib/demo.ts`).
3. `rpc('demo', {phase: 'end'})` always; if the page goes away, sessiond ends it (api.md §4.1).

The caption replaces the ticker while playing ("Watching: <goal>", keys so far, Stop). Learner input, a snapshot of another attempt or one that leaves `watching`, or leaving the lesson stops it. While the snapshot says `watching` the step offers Do it yourself (`rpc('restore', {})`), also after a reload. `:try` keys are learner input and complete steps as done. While latched, the learner's own completion gets no reaction until Do it yourself (open, api.md §4.1).

**Skip this step**: `rpc('skip', {stepId})`; sessiond plays the demo and records `skipped` while the veil swallows keys. It is offered only after an "I'm stuck" report, so `features.stuckReports: false` hides it (open).

## 8. GUI pane

Shown when `mode === 'gui'`; the frame switch (`frame.ts`) calls `rpc('mode')`. While the frame starts the terminal stays with a "Starting…" chip (20 s, then a toast). Both panes stay mounted, one visible.

GUI lessons (`mode: gui` or tag `gui`, `lessonTags.ts`) switch to the GUI frame once loaded, a `mode: gui` step once per activation; other lessons and tasks switch back to the terminal unless the learner picked the GUI frame. Nothing switches while `features.gui` is off.

Lesson notices (`LessonNotices.tsx`): GUI lessons explain the GUI frame and offer to reopen it, or say the server has it off (`features.gui`); remote lessons (tag `remote` or `remote.box`) show `status.remoteBox`, polled every 5 s while false, or say the server has no box (`features.remoteBox`).

## 9. Preview pane

Tabs: `preview.tabs` plus files opened from the tree; `preview.follow` maps the current source buffer to its artefact. Views: PDF (lazy worker, keeps scale and scroll across reloads), HTML (iframe `sandbox="allow-scripts"`), Markdown (lesson pipeline without directives), image, plain text; they reload on `file` events. A HEAD probe gates every view: a missing artefact says so and, under `preview.follow`, which source builds it (the one being edited, else the last changed; a followed file nobody builds is never "rebuilding"); the `file` event that creates it shows the view. A PDF that fails to parse (still being written) is retried once after 800 ms. sessiond's `SandboxCSP` keeps `allow-same-origin`, an open question (deployment.md §9.3).

## 10. Course map and projects

A lesson row reads "done" when every step is done, "finished · N skipped · M watched" (hollow mark) when all are done, watched or skipped, else open with "· N skipped". Continue skips lessons whose steps are all done or skipped.

**Start over** (next to Continue once there is progress; always on the Session page) confirms that files and Emacs stay, posts `/api/v1/progress/reset` with no body, calls `forgetProgress()`, clears hint levels and refetches progress. sessiond forgets the loaded lesson (a `dropped` lesson event), so an open lesson tab says Emacs no longer shows it and offers Load this lesson here; files and buffers stay.

Projects (`ProjectScreen.tsx`) show "Task N of M" with a progress bar (done, skipped, watched, from saved progress; a finished project with tasks the learner did not do says so), one file tree per `project.roots` (default `setup.root`; `GET /preview/<root>/`, a click calls `rpc('open', {path})`), and the checkpoint list (refetched after file changes): task checkpoints ("Start of task N: goal", restored by `{ref}`) and manual ones (by `{label}`); Restore's dialog refetches the list and names the files a restore overwrites.

Once every task is done or skipped the project offers **Again, without hints** (`lap.ts`): `rpc('scenario', {lessonId, lap: true})`; while the snapshot has a `lap`, hints and Watch it are hidden (sessiond answers 409), and Go to too (another load ends the lap) and a timer runs from `lap.startedAt` against `project.lapMinutes`. The result is `lapMs` on the completing lesson event; Stop the lap reloads the task without it. The task list collapses like Do. Finished laps (count, best, last) come from the lesson's progress `laps`.

## 11. Styling

`tokens.css` holds the demo's palette and type (Source Serif 4 and JetBrains Mono, bundled subsets; `--mono` puts the gateway's Iosevka Nerd Font first; ligatures off in `kbd`, `code`, `pre`, `samp`, the terminal and every rule that sets the mono font, so `M->` stays three keys). The lesson pane and the stage share the width through a splitter (`Splitter.tsx`: drag, ←/→ 24 px, Home resets): the pane is the learner's `lessonWidth` (`pg.prefs`) or 30vw within 360–560 px, clamped to 300 px … 55vw; the splitter hides in focus mode (which shows only the `.stage` element) and in the narrow layout. Breakpoints: at 1440 px the rail becomes a drawer behind "Where am I"; at 1180 px the session chip hides and crumbs, then the brand, shrink so "Where am I" and "Rescue me" stay visible; 860 px is the narrow layout. The drawer returns focus to its opener. Motion only on state changes.

## 12. Internationalisation

Preferences offer only locales covering every key of `en.json` (zh-CN covers about a sixth and is hidden; some `advice/` strings are English literals); a stored locale not offered falls back to English. Lesson files and tables are per locale (`lesson.zh-CN.md`, `exits.zh-CN.yaml`), chosen by the gateway. Key and command names are never translated.

## 13. Accessibility

What the canvas shows is also text: the ticker (`aria-live`), Where am I and the echo line (`LiveAnnouncer`). Dialogs trap focus; contrast meets WCAG AA.

## 14. Testing

- Unit and component (Vitest, jsdom, MSW), on streams recorded from the fake session (which publishes lesson snapshots as sessiond does); `useScenarioLoad` mounted over the store with a mocked rpc; the predicate, template, demo and key ports against the conformance corpus (`advice/conformance.test.ts`, api.md §8).
- Fake session (`pnpm dev:fake`, `web/dev/README.md`): gateway and session without Docker; `e2e/lesson.mjs` walks a lesson on it (or a real gateway), `e2e/shots.mjs` screenshots every screen.
- Against the compose stack over CDP (AGENTS.md): `e2e/smoke.mjs` (integration), `e2e/review.mjs` (this document's UX contracts, `PG_REVIEW_ONLY` picks checks) and `e2e/demo-focus.mjs` (a focus report does not stop Watch it); they reset progress at the end.

## 15. Build and development

`pnpm dev` runs Vite on `:5173`, proxying `/api/`, `/s/`, `/fonts/`, `/internal/` to `$PG_GATEWAY` or `127.0.0.1:8080`. noVNC, pdfjs and every screen but the lesson are lazy chunks. Budget: 600 KB gzipped up front (`pnpm size`).

## 16. Performance budgets

Event to ticker render within one frame (end-to-end budgets: architecture.md §7). `reduce` does constant work per event (the ticker keeps 40 entries); derived views (`selectors.ts`) are memoised by input identity, `activeStep` is one object per snapshot, and the lesson Markdown is parsed once per lesson, not per store update.
