# API reference

Contracts between browser, gateway, sessiond and the Emacs agent. The Go types in `internal/protocol` are the source of truth; `web/src/protocol.gen.ts` is generated from them (§8).

## 1. Conventions

- Session endpoints live under the gateway's `/s/{sessionId}/`. JSON, UTF-8, Unix-millisecond timestamps.
- Lesson ids are the lesson directory under `curriculum/`, with one slash (`02-buffers-and-files/03-opening-files`), put into URLs literally; routes use trailing wildcards.
- Errors: `application/problem+json` `{status, code, message, details?}`, codes from `protocol.ErrorCode`: `unauthenticated` 401, `forbidden` 403, `not_found` 404, `invalid_request` 400, `conflict` 409, `rate_limited` 429, `session_limit` 429/503, `group_quota_exceeded` 429 (details `{group, maxActiveSessions}`, `group` empty for learners in no listed group), `session_unavailable` 502/503, `emacs_unavailable` 503, `timeout` 504, `not_implemented` 501, `internal` 500 (also for unknown codes).
- Auth: cookie `pg_session`. State-changing requests and WebSocket upgrades need `Origin` equal to the public URL's (with a loopback public URL, `localhost`, `127.0.0.1` and `::1` are equivalent); `/internal/` is exempt.
- Versioning: REST under `/api/v1` (it changes only when the gateway REST API breaks); the events `hello` and the agent `hello` carry `protocol: 2` (`protocol.Version`); a breaking change to events, RPC or agent messages bumps it.

## 2. Gateway API (`/api/v1`)

Outside the prefix: `GET /healthz` → `200 {ok:true, reason?}` (`reason` while no curriculum bundle is loaded).

### 2.1 Authentication

- `GET /auth/config` (public) → `{mode, authMode, oidcStartUrl?, oidcDisplayName?}`; `authMode` is `none` or `oidc`.
- `GET /auth/oidc/start?next=/path` → `302` to the provider (`oidcStartUrl` has no query; `next` must be a same-origin path). `GET /auth/oidc/callback` → `302` to `next` (default `/`) with the cookie, or to `/login?error=` `state`, `provider`, `token` or `forbidden` (`protocol.LoginError`). Both `404` outside `oidc` mode.
- `POST /auth/logout` → `204`, or `200 {redirectUrl}` (`protocol.LogoutResult`) to continue at the provider; revokes every cookie of the user.
- `GET /me` → `{user:{id, displayName, kind, username?, email?, groups?}, mode, authMode, features:{gui, remoteBox, stuckReports, authorMode}, version}`; `kind` is `local` or `oidc`.

### 2.2 Curriculum

`GET /curriculum` (`protocol.CurriculumIndex`, including `tables`), `GET /curriculum/lessons/{lessonId}` → `{id, title, locale, markdown, front, scenario, assets}` (scenario as in §6), `GET /curriculum/assets/{lessonId}/{path}`. Locale: `?locale=`, else `Accept-Language`, else `curriculum.defaultLocale`. Strong ETags; `503` without a bundle.

### 2.3 Sessions

`POST /sessions` → `201 SessionInfo` (idempotent per user; `429 rate_limited` beyond the creation rate, `429 session_limit` or `group_quota_exceeded` when full); `GET /sessions/{id}` (`emacs`, `mode`, `step` from sessiond's `/status`); `DELETE /sessions/{id}` → `204` (volume kept); `POST /sessions/{id}/restart-emacs` → `{ok:true, result:{}}` (rpc `restart-emacs`).

`SessionInfo` = `{id, state, createdAt, lastActiveAt?, image?, gui, failure?, emacs?, mode?, step?, updateAvailable?}`. States: `creating`, `starting`, `ready`, `idle`, `stopping`, `stopped`, `failed`, `removed`.

### 2.4 Progress

- `GET /progress` → `{lessons:{[id]:{status, counts:{done, demonstrated, skipped, total}, steps:{[id]:{status, hintsUsed, resets, completedAt?}}, laps?:{count, bestMs, lastMs, lastAt}}}, updatedAt}`; `laps` counts finished "again, without hints" laps.
- `POST /progress/reset {lessonId}` clears one lesson; no body, `{}` or empty `lessonId` clears all ("Start over"). `204`; the gateway then sends the session `drop-step {lessonId?, generation}`. Files, checkpoints and modules stay.

Each reset issues `max(stored maximum + 1, gateway time in milliseconds)` as its generation; reports older than the gateway generation for their lesson are discarded with a warning (the greater of the lesson and whole-user generations, initially 0). Newer reports are accepted without advancing it, and sessiond adopts the gateway generations exactly on sync. Report timestamps do not order resets; omitted completion times default to receipt time. A report applied twice changes nothing: a finished lap counts once per `lapId` (a report without one, once per completion time and duration).

Step statuses only rise, `not-started` < `in-progress` < `demonstrated` < `skipped` < `done` (`protocol.ProgressRank`); only a reset lowers them. `hintsUsed` and `resets` are absolute totals. A lesson is `done` only when every step of the bundle's scenario is `done`; `counts.total` is 0 outside the bundle. Resume starts at the first step neither `done` nor `skipped`.

### 2.5 Stuck reports

`POST /stuck {sessionId, lessonId?, stepId?, note}` → `201 {id}`: stores the session's recent events (`rpc stuck`). `403` when `features.stuckReports` is off; `429` beyond the per-user hourly limit. At most 200 events, 512 KiB and a 4000-byte note; identical reports within 30 s merge.

### 2.6 Internal (sessiond → gateway)

`Authorization: Bearer <PG_SESSION_TOKEN>` of the session in the path; in server mode, requests with `X-Forwarded-For` or `Forwarded` get `404`. Learners can read their token, so bodies are untrusted: progress only for the bundle's steps (`400`), stuck reports under the hourly limit.

- `GET /internal/curriculum/scenarios/{lessonId}`; `GET /internal/curriculum/fixtures/{lessonId}` (zstd tarball, `204` if none, ETag).
- `GET /internal/sessions/{id}/progress-generations` → a map of lesson ids to generations; the empty key is the whole-user generation. sessiond synchronizes it at startup and before every `scenario`.
- `POST /internal/sessions/{id}/progress` `{lessonId, stepId, status, generation?, hintsUsed?, resets?, at?, lapMs?, lapId?}` → `204` (`lapMs` and `lapId` only on the last step's `done`; `lapId` is the lap's `id`, `<lessonId>@<startedAt>`, else 400); sessiond's progress outbox posts each entry until the gateway answers and may post one again (backend.md §3.8); `…/stuck` `{lessonId?, stepId?, note, events}`; `…/heartbeat` `{activeWebSockets}` every 30 s.

## 3. Session API (`/s/{id}`)

sessiond serves these on `:7000`; the gateway proxies them (backend.md §2.5). With `PG_PROXY_SECRET` set, sessiond answers `401` without a matching `X-Playground-Proxy`, except `GET`/`HEAD /healthz` (backend.md §3.13).

### 3.1 `GET /pty` (WebSocket)

Query `cols`, `rows`. Binary frames: terminal bytes. Text frames: client `{"t":"resize","cols","rows"}`, `{"t":"ping"}`; server `attached`, `pong`, `replaced`.

Close codes (all session WebSockets): `4000` replaced, `4001` Emacs/tmux unavailable (on `/vnc`: Xvnc exited, reconnect), `4002` GUI not active, `4003` session stopping.

### 3.2 `GET /events` (WebSocket)

`?after=<seq>` replays from the 500-event ring. First frame `hello` (`replayed` when a replay follows; its lesson snapshot is read after the subscription began), then replayed events in sequence order, or the latest full `state` when no replay follows (§4). Client may send `ping`.

### 3.3 `GET /vnc` (WebSocket)

RFB to Xvnc (`binary` subprotocol). Only in GUI mode; else, and on switching to the terminal, `4002`.

### 3.4 RPC (`POST /rpc/{op}`)

Result `{"ok":true,"result":…}` or problem+json. *(n)*: not idempotent. Ops needing Emacs answer `503 emacs_unavailable` / `504 timeout`; ops on the active step `409` when none is active or another is named. `scenario`, `restore`, `modules`, `drop-step`, `demo start`, `skip`, `checkpoint` and `restart-emacs` are ops of the session core (backend.md §3.12): one at a time, each to completion even when its request goes away; a request arriving meanwhile waits its turn, and the snapshot's `busy` names the running op (§4.1). An op ended by a drop answers `409`, one past its deadline `504`.

- `keys {keys}|{text}, frame?` → `{}` *(n)*: one of validated Emacs keys or literal text (≤ 4096 bytes) into `frame` (default the current mode; `409` if inactive). Learner input.
- `eval {form, timeoutMs?}` → `{value}`: author mode only (`403`, also in the proxy); 2 s default, 10 s max; symbol-keyed alists become objects.
- `open {path}` → `{buffer}`: visits a workspace file.
- `rescue {}` → `{did}`: `playground-rescue` (emacs.md §10.3), or C-g through tmux when Emacs is unresponsive.
- `scenario {lessonId, stepId?, reset?, lap?}` → `{step, index, count, modules, restarting, lap?}`: the load (backend.md §3.12). Returns when the step is active, or with `restarting: true` once a daemon restart begins (the load then finishes in the background; a request arriving meanwhile gets the final result). `400` for an unknown step, `409` for a lesson without steps. `lap: true` starts the lap (CURRICULUM.md B.3) at the first step with a reset (`400` for another `stepId`); `lap {id, lessonId, startedAt}` (`id` names the lap for its progress report; `id` and `startedAt` are empty in an early `restarting` result). Until the lap ends `hint`, `demo start` and `skip` answer `409`. Reloading the active lesson without `stepId` keeps the lap; any other load, and `drop-step`, ends it.
- `check {}` → `{lessonId, stepId, conditions:[{id, ok, unknown?, detail?}], complete}`; the `check` event it causes carries the `attempt`.
- `hint {stepId, level}` → `{}`: records the highest level shown for a step of the loaded lesson (`409` for another lesson's step or during a lap).
- `checkpoint {label}` → `{ref, createdAt}`; `checkpoints {}` → `{items:[{label, ref, createdAt, files, step?}]}`, newest first; `files` would change on restore.
- `restore {label}|{ref}|{}` → `{restoredFiles}` *(n)*: `{}` is the active step's checkpoint (Reset this step, Do it yourself; `409` without an active step); `404` when the checkpoint does not exist. Restoring a step's checkpoint while its lesson is loaded counts a reset, clears the latch and publishes `lesson reset`.
- `mode {mode: terminal|gui}` → `{mode}`; `409` if GUI is disabled.
- `modules {enabled}` → `{restarting}`: sets the set exactly (`scenario` only adds); `400` without `agent`.
- `restart-emacs {}` → `{}`: queues a restart, reported by `emacs` events.
- `stuck {note}` → `{events}`: last 200 events. Through the proxy (`X-Forwarded-For` present) sessiond also files the report.
- `status {}` (also `GET /status`) → `{sessionId, emacs:{version, pid, uptimeMs, agent, responsive}, frames, mode, step, modules, versions?, activeWebSockets, remoteBox?, lesson}`; `step` is the active step (the gateway's session info), `lesson` the snapshot (§4.1); `remoteBox` once the box alias is written (deployment.md §6).
- `describe {keys}` → `{cmd, doc}`: for ticker glosses; `cmd` `""` when unbound or a prefix.
- `demo {stepId, phase: start|end}` → `{restored, attempt}` *(n)*; `skip {stepId}` → `{next|null, demoCompleted}` *(n)*, `409` while another skip runs: §4.1. Both start only for the attempt they were admitted for: `409` when a load or advance queued before them replaced it.
- `drop-step {lessonId?, generation?}` → `{}`: internal (the gateway calls it; the proxy answers `403`); forgets that lesson's (without `lessonId` every lesson's) saved progress, lap and active step; a `generation` not newer than the one applied is ignored. It runs before queued ops and, unless ignored, cancels a running op on that lesson.

### 3.5 `GET /preview/{path}`

A workspace file via `os.Root`; `.playground/` never served. `no-store`, `nosniff`; active documents get `Content-Security-Policy: sandbox allow-scripts allow-same-origin`. `GET /preview/{dir}/` → `{path, entries:[{name, dir, size, mtime}]}`.

### 3.6 `GET /healthz`

`200 {ok:true}` once the agent first said hello; `503 {reason}` before, and after the supervisor gave up on Emacs.

## 4. Event catalog (`/events`)

Envelope `{t, seq, ts, …}`, `seq` monotonic from 1 per process; unknown types are ignored.

- `hello`: `protocol, sessionId, mode, emacs, frames, modules, replayed, lesson` (the snapshot, §4.1). `protocol` is `protocol.Version` (2); a page that gets another starts nothing and offers to stop the session (frontend.md §5).
- `key`: `keys, cmd, buf, mode, frame, prefixArg?, mx?`. `cmd` is `real-this-command` (emacs.md §10.1), `"<lambda>"` or `"undefined"`; with `mx: true` (M-x) `keys` are the reader's.
- `prefix`: `keys, source: tap|agent`; `""` clears. Advisory; never follows the `key` that consumed it.
- `state` (§5): coalesced, duplicates suppressed. `echo`: `text` ≤ 500 characters, noise filtered. `minibuffer`: `open, prompt, depth, kind: prompt|isearch`.
- `emacs`: `event, frame?, reason?`; `frame-opened`, `frame-closed`, `daemon-restarted`, `restarting`, `suspended`, `resumed`, `unresponsive`, `responsive`, `stopping`.
- `file`: `path, event: created|modified|deleted`.
- `check`: as the RPC, plus `attempt`; `unknown: true` (with `ok: false`) when unevaluable; `detail` is evaluator output. A check of another attempt than the snapshot's is stale.
- `lesson`: `change, stepId?, lapMs?, lesson` (§4.1; `lapMs` on the `complete` that ends a lap).
- `nudge`: `id` (stable per rule), `kind: calm|ask, title, body`.
- `checkpoint`: `label, ref, createdAt`. `mode`: `mode`. `modules`: `enabled, restarting` (`true`, then `false` once the lesson that asked for the modules is set up, or after the restart of `rpc modules`).
- `gap`: `from, to`; reconnect from the last received sequence to recover lost transitions.
- `error`: `code, message` for the learner (fixtures, setup, `emacs_unavailable`).

### 4.1 Lesson snapshot and lifecycle

The session core (backend.md §3.8) owns the lesson's lifecycle facts and publishes them as one snapshot: `{rev, lessonId?, step: {id, index, count}|null, phase: idle|active|watching|complete, attempt, outcomes, lap?, busy?}`. `rev` rises with every published change (per sessiond process); `attempt` numbers the active step's activation (0 without a step); `outcomes` are the loaded lesson's saved `done|skipped|demonstrated` steps; `complete` means every step is done or skipped; a running skip shows `active`. `busy {op, lessonId?, stepId?, startedAt}` is the lesson op running: `load`, `resume`, `restore`, `demo-start`, `skip`, `set-modules`, `restart-emacs`, `recover` or `reconnect`, an open set whose unknown kinds clients ignore. `hello` and `status` carry the current snapshot; a client applies a `lesson` event only when its `rev` is higher than the snapshot it holds, and every `hello` whatever its rev. A rehearsal (fast-forward) is never visible. `change` names why a snapshot was published:

- `loaded`: scenario load, resume or advance. A step that takes a checkpoint has it, unless its capture failed or a fast-forward to it did not complete (it then activates without one, and `restore {}` answers 404 until the next load takes it).
- `reset`: a step checkpoint restored (`restore`, or `demo start` and `skip` restoring first); a new attempt, the step's outcome forgotten.
- `complete`: all conditions hold without a latch; progress `done`; next step `loaded`, or phase `complete` after the last.
- `demonstrated`: all conditions hold under the latch (once per latch); progress `demonstrated`; phase stays `watching`; the browser offers Do it yourself (`restore {}`).
- `skipped`: after `skip`; progress `skipped`; continues as `complete`.
- `dropped`: `drop-step` forgot progress; when it concerns the loaded lesson, that lesson is unloaded (`lessonId` empty, phase `idle`).
- `phase`: the latch was set or dropped, or an interrupted load put the step before it back (a new attempt).
- `busy`: a lesson op started or ended.

**Watch it**: `demo start` restores the checkpoint when a command ran since the step started or was reset, or a failed load changed the workspace (`restored: true`, no reset counted), then sets the latch, which lasts until `restore {}`, a reload of the step or `drop-step`. Playing ends with `demo end`, 65 s without `keys` (`session.DemoIdle`, longer than the maximum 60 s pause) or the page's terminal closing; if the step had not completed, the latch is dropped then. Stuck rules pause while playing. While the latch holds, the learner's own completion records nothing; Do it yourself starts a fresh attempt.

**Skip**: restores like `demo start`, plays the demo into the learner's frame at `protocol.PlayDemo` pacing, waits ≤ 8 s for the conditions (`demoCompleted: false` is a curriculum defect), records `skipped`, advances; ≤ 60 s, even if the browser leaves.

## 5. State object

From the agent, plus `seq` and `ts`. Field names are the paths `state` predicates and exit rows use.

```json
{"t":"state","buf":"main.rs","file":"/work/hello/src/main.rs","mode":"rust-ts-mode","modeName":"Rust",
 "minors":["eglot--managed-mode"],"modified":true,"readonly":false,"narrowed":false,"overwrite":false,
 "line":9,"col":20,"point":182,"markActive":false,
 "windows":[{"id":3,"buf":"main.rs","mode":"rust-ts-mode","selected":true,"dedicated":false,"side":null},
            {"id":7,"buf":"*compilation*","mode":"compilation-mode","selected":false,"dedicated":false,"side":"bottom"}],
 "layout":["vsplit",3,7],"frame":"terminal","frames":1,"tabs":{"count":1,"current":"main"},
 "minibuffer":null,"isearch":false,"recursive":0,"macro":false,"prefixArg":null,
 "readOnlyReason":null,"diagAtPoint":null,"waiting":null}
```

- The learner's frame (emacs.md §10.3). `minors` lists only `playground-agent-interesting-minor-modes`.
- `windows[].id` is stable per window; `side` is `window-side`.
- `layout`: a window id or `["vsplit"|"hsplit", child, …]`; `vsplit` stacks (C-x 2), `hsplit` is side by side (C-x 3).
- `minibuffer` when open: `{depth, prompt, contents, kind}`. `frames` includes the daemon's invisible frame.
- `diagAtPoint`: `{severity: error|warning|note, message, source?, line, col}`.
- `waiting` while a loop reads its own keys or a transient menu is open: `{kind: menu|avy|which-key|transient, what, leave}`; `leave` is `C-g`, or `a` for which-key, where C-g only returns to the prefix. Rescue closes a transient menu first, then sends a loop's `leave`.

## 6. Scenario object

Parsed `scenario.yaml` (CURRICULUM.md B.3, `protocol.Scenario`), served with ids assigned (`Scenario.AssignIDs`):

- Condition ids default to `<stepId>/<n>` (1-based, top level), stuck rules `<stepId>/stuck/<n>`; an explicit `id` wins.
- Conditions: `event {command, min?, since?}`, `elisp`, `file {path, host?, exists|contains|not_contains|regex|json_path+equals}`, `shell {run, cwd?, timeout?, host?}`, `buffer {name, mode?, contains?, process?, modified?}`, `layout`, `state` (`protocol.Predicate` over §5; its regexes are regex dialect 1, CURRICULUM.md B.4, while `file.regex` is plain RE2); `all`, `any`, `not`.
- Stuck rule `when` (all must hold): `minibuffer_seconds`, `idle_seconds`, `step_seconds`, `quits_in_window {count, seconds}`, `unbound_repeats`.
- `title`, `goal`, `detail`, `text`, `hint`, `say`: a string or a locale map.
- Demos (`protocol.ParseDemo`, syntax in CURRICULUM.md B.3): `protocol.PlayDemo` plays them at 170 ms per chord and 70 ms per character (`DemoChordDelayMs`, `DemoCharDelayMs`) for playctl, Skip and the fast-forward; the browser's Watch it plays at 600 and 120 ms (frontend.md §7).

## 7. Agent protocol (Unix socket, JSON lines)

One JSON object per line, ≤ 1 MB (`protocol.MaxAgentLine`); a new connection replaces the old. sessiond logs a warning when the hello's `protocol` differs from `protocol.Version` or `agent` has another major version than `protocol.AgentVersion`.

Agent → sessiond: `hello {protocol, agent, emacs, pid, modules?, moduleErrors?}`; `key`, `state`, `echo`, `minibuffer` as in §4; `frame {event: opened|closed, kind}`, `closed` only when the last frame of its terminal goes; `ping` every 2 s (five missed mark Emacs unresponsive); `reply {id, ok, value?, error?}`.

sessiond → agent: `pong`, `call {id, op, args, timeoutMs}`, serialised; a prompting op fails instead of blocking (emacs.md §10.4). Ops take typed arguments (`protocol.Agent*Args`); sessiond builds no Lisp, and only `eval` (author mode) and `check` (curriculum conditions) carry forms. Semantics in emacs.md §10.3:

- `eval {form}` → `{value}`; `check {forms}` → `{results}` of `true|false|"unknown"` (`protocol.Truth`; 1 s per form).
- `capture-editor {workspace, vars, maxText, maxUndo, maxTotal, maxGlobals}` → `{spec: WindowSpec, buffers: {path: [modified, text|null, undo|null]}, globals: string|null}`; `restore-editor {workspace, spec, buffers, globals}` → `{reverted, killed, buffers, globals, degraded}`; `modified-files {workspace}` → `{paths}`; `flush-buffers` → `{saved}` (the snapshot service, backend.md §3.9).
- `open WindowSpec` → `{opened, layoutDeferred?}`; `visit {path}` → `{buffer}`; `setup {open?, layout?, eval?, compile?, workspace, reset?, root?}` → `{ok}`.
- `rescue` → `{did}`; `describe {keys}|{cmd}` → `{cmd, doc}`.
- `leave-display` → `{closed, detail}`; `resume-loop` → `{}`; `reset-debug` → `{}` (the supervisor).
- The agent also answers `set-modules {enabled}` → `{written}` and `frame {kind, display?}` → `{frame}`, which sessiond does not call: it writes `modules.el` itself and opens the GUI frame with `emacsclient -c`.

`WindowSpec`: `files:[{path, point, visible}]` (most recent first, workspace-relative), `layout` (used without `windowState`), `windowState` and `specialBuffers` (printed, opaque to sessiond), `selected`, `tab`, `tabs`, `tabIndex`, `takenAt`; semantics in emacs.md §10.3.

The agent never blocks editing: it queues output while the socket is stuck and reconnects with backoff (emacs.md §10.4). Contract fixtures recorded from the real agent pin these shapes for sessiond's fake editor (backend.md §6).

## 8. Type generation

`playctl protocol typescript` (`internal/protocol/tsgen`: reflection plus the Go AST) renders string-literal enums, an `Event` union on `t` and an `RpcOps` request/result map. `go test ./internal/protocol/...` fails when it is stale; `make protocol` regenerates it. `deploy/config.schema.json` comes from invopop/jsonschema (`playctl config schema`).

Shared semantics have one conformance corpus, `internal/protocol/testdata/conformance/*.json` (predicates, templates, demos, keys, layouts, a sample state): `conformance_test.go` runs it against Go and `web/src/advice/conformance.test.ts` against the browser ports (layouts are Go only), each case with Go's answer.
