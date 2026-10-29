# API reference

Contracts between browser, gateway, sessiond and the Emacs agent. The Go types in `internal/protocol` are the source of truth; `web/src/protocol.gen.ts` is generated from them (§8).

## 1. Conventions

- Session endpoints live under the gateway's `/s/{sessionId}/`. JSON, UTF-8, Unix-millisecond timestamps.
- Lesson ids are the lesson directory under `curriculum/`, with one slash (`02-buffers-and-files/03-opening-files`), put into URLs literally; routes use trailing wildcards.
- Errors: `application/problem+json` `{status, code, message, details?}`, codes from `protocol.ErrorCode`: `unauthenticated` 401, `forbidden` 403, `not_found` 404, `invalid_request` 400, `conflict` 409, `rate_limited` 429, `session_limit` 429/503, `session_unavailable` 502/503, `emacs_unavailable` 503, `timeout` 504, `not_implemented` 501, `internal` 500 (also for unknown codes).
- Auth: cookie `pg_session`. State-changing requests and WebSocket upgrades need `Origin` equal to the public URL's (local mode: `localhost` ≡ `127.0.0.1`).
- Versioning: REST under `/api/v1`; WebSocket and agent `hello` carry `protocol: 1`; breaking changes bump both.

## 2. Gateway API (`/api/v1`)

### 2.1 Authentication

- `GET /auth/config` (public) → `{mode, authMode, oidcStartUrl?}`.
- `POST /auth/invite {code}` → `204` and cookie; `403` bad, expired or used-up code; `429`; `409` unless auth mode is `invite`.
- `GET /auth/oidc/start|callback` → `501` (`404` outside `oidc` mode). `POST /auth/logout` → `204`.
- `GET /me` → `{user:{id, displayName, kind}, mode, authMode, features:{gui, remoteBox, stuckReports, authorMode}, version}`.

### 2.2 Curriculum

`GET /curriculum` (`protocol.CurriculumIndex`, including `tables`), `GET /curriculum/lessons/{lessonId}` → `{id, title, locale, markdown, front, scenario, assets}` (scenario as in §6), `GET /curriculum/assets/{lessonId}/{path}`. Locale: `?locale=`, else `Accept-Language`, else `curriculum.defaultLocale`. Strong ETags; `503` without a bundle.

### 2.3 Sessions

`POST /sessions` → `201 SessionInfo` (idempotent per user, rate limited); `GET /sessions/{id}` (`emacs`, `mode`, `step` from sessiond's `/status`); `DELETE /sessions/{id}` → `204` (volume kept); `POST /sessions/{id}/restart-emacs`.

`SessionInfo` = `{id, state, createdAt, lastActiveAt?, image?, gui, failure?, emacs?, mode?, step?, updateAvailable?}`. States: `creating`, `starting`, `ready`, `idle`, `stopping`, `stopped`, `failed`, `removed`.

### 2.4 Progress

- `GET /progress` → `{lessons:{[id]:{status, counts:{done, demonstrated, skipped, total}, steps:{[id]:{status, hintsUsed, resets, completedAt?}}, laps?:{count, bestMs, lastMs, lastAt}}}, updatedAt}`; `laps` counts finished "again, without hints" laps.
- `POST /progress/reset {lessonId}` clears one lesson; no body, `{}` or empty `lessonId` clears all ("Start over"). `204`; the gateway then sends the session `drop-step` with the same body. Files, checkpoints and modules stay.

Step statuses only rise, `not-started` < `in-progress` < `demonstrated` < `skipped` < `done` (`protocol.ProgressRank`); only a reset lowers them. `hintsUsed` and `resets` are absolute totals. A lesson is `done` only when every step of the bundle's scenario is `done`; `counts.total` is 0 outside the bundle. Resume starts at the first step neither `done` nor `skipped`.

### 2.5 Stuck reports

`POST /stuck {sessionId, lessonId?, stepId?, note}` → `201 {id}`: stores the session's recent events (`rpc stuck`). `403` when `features.stuckReports` is off; `429` beyond the per-user hourly limit. At most 200 events, 512 KiB and a 4000-byte note; identical reports within 30 s merge.

### 2.6 Internal (sessiond → gateway)

`Authorization: Bearer <PG_SESSION_TOKEN>` of the session in the path; in server mode, requests with `X-Forwarded-For` or `Forwarded` get `404`. Learners can read their token, so bodies are untrusted: progress only for the bundle's steps (`400`), stuck reports under the hourly limit.

- `GET /internal/curriculum/scenarios/{lessonId}`; `GET /internal/curriculum/fixtures/{lessonId}` (zstd tarball, `204` if none, ETag).
- `POST /internal/sessions/{id}/progress` `{lessonId, stepId, status, hintsUsed?, resets?, at?, lapMs?}` (`lapMs` only on the last step's `done`); `…/stuck` `{lessonId?, stepId?, note, events}`; `…/heartbeat` `{activeWebSockets}` every 30 s.

## 3. Session API (`/s/{id}`)

sessiond serves these on `:7000`; the gateway proxies them (backend.md §2.5). With `PG_PROXY_SECRET` set, sessiond answers `401` without a matching `X-Playground-Proxy`, except `GET`/`HEAD /healthz` (backend.md §3.13).

### 3.1 `GET /pty` (WebSocket)

Query `cols`, `rows`. Binary frames: terminal bytes. Text frames: client `{"t":"resize","cols","rows"}`, `{"t":"ping"}`; server `attached`, `pong`, `replaced`.

Close codes (all session WebSockets): `4000` replaced, `4001` Emacs/tmux unavailable (on `/vnc`: Xvnc exited, reconnect), `4002` GUI not active, `4003` session stopping.

### 3.2 `GET /events` (WebSocket)

`?after=<seq>` replays from the ring. First frame `hello` (`replayed` when a replay follows), then a full `state`, then events (§4). Client may send `ping`.

### 3.3 `GET /vnc` (WebSocket)

RFB to Xvnc (`binary` subprotocol). Only in GUI mode; else, and on switching to the terminal, `4002`.

### 3.4 RPC (`POST /rpc/{op}`)

Result `{"ok":true,"result":…}` or problem+json. *(n)*: not idempotent. Ops needing Emacs answer `503 emacs_unavailable` / `504 timeout`; ops on the active step `409` when none is active or another is named.

- `keys {keys}|{text}, frame?` → `{}` *(n)*: one of validated Emacs keys or literal text (≤ 4096 bytes) into `frame` (default the current mode; `409` if inactive). Learner input.
- `eval {form, timeoutMs?}` → `{value}`: author mode only (`403`); 2 s default, 10 s max; symbol-keyed alists become objects.
- `open {path}` → `{buffer}`: visits a workspace file.
- `rescue {}` → `{did}`: `playground-rescue` (emacs.md §10.3), or C-g through tmux when Emacs is unresponsive.
- `scenario {lessonId, stepId?, reset?, lap?}` → `{step, index, count, modules, restarting, lap?}`: backend.md §3.12. Returns when the step is active, or with `restarting: true` before a daemon restart. `lap: true` starts the lap (CURRICULUM.md B.3): reset, first step, `lap {lessonId, startedAt}`; until it ends `hint`, `demo start` and `skip` answer `409`.
- `check {}` → `{lessonId, stepId, conditions:[{id, ok, unknown?, detail?}], complete}`.
- `hint {stepId, level}` → `{}`.
- `checkpoint {label}` → `{ref, createdAt}`; `checkpoints {}` → `{items:[{label, ref, createdAt, files, step?}]}`, newest first; `files` would change on restore.
- `restore {label}|{ref}|{}` → `{restoredFiles}` *(n)*: `{}` is the active step's checkpoint (Reset this step, Do it yourself); counts a reset, clears the latch, emits `step reset`.
- `mode {mode: terminal|gui}` → `{mode}`; `409` if GUI is disabled.
- `modules {enabled}` → `{restarting}`: sets the set exactly (`scenario` only adds).
- `restart-emacs {}` → `{}`: background restart, reported by `emacs` events.
- `stuck {note}` → `{events}`: last 200 events. Through the proxy (`X-Forwarded-For` present) sessiond also files the report.
- `status {}` (also `GET /status`) → `{sessionId, emacs, frames, mode, step, modules, versions?, activeWebSockets, lap?, remoteBox?}`; `remoteBox` once the box alias is written (deployment.md §6).
- `describe {keys}` → `{cmd, doc}`: for ticker glosses; `cmd` `""` when unbound or a prefix.
- `demo {stepId, phase: start|end}` → `{restored}` *(n)*; `skip {stepId}` → `{next|null, demoCompleted}` *(n)*, `409` while another skip runs: §4.1.
- `drop-step {lessonId}|{}` → `{}`: internal (gateway only); forgets that lesson's (or all) saved progress and the active step if it belongs to it.

### 3.5 `GET /preview/{path}`

A workspace file via `os.Root`; `.playground/` never served. `no-store`, `nosniff`; active documents get `Content-Security-Policy: sandbox allow-scripts allow-same-origin`. `GET /preview/{dir}/` → `{path, entries:[{name, dir, size, mtime}]}`.

### 3.6 `GET /healthz`

`200 {ok:true}` once the agent first said hello; `503 {reason}` before, and after the supervisor gave up on Emacs.

## 4. Event catalog (`/events`)

Envelope `{t, seq, ts, …}`, `seq` monotonic from 1 per process; unknown types are ignored.

- `hello`: `protocol, sessionId, mode, emacs, frames, step|null, modules, replayed, lap?`.
- `key`: `keys, cmd, buf, mode, frame, prefixArg?, mx?`. `cmd` is `real-this-command` (emacs.md §10.1), `"<lambda>"` or `"undefined"`; with `mx: true` (M-x) `keys` are the reader's.
- `prefix`: `keys, source: tap|agent`; `""` clears. Advisory; never follows the `key` that consumed it.
- `state` (§5): coalesced, duplicates suppressed. `echo`: `text` ≤ 500 characters, noise filtered. `minibuffer`: `open, prompt, depth, kind: prompt|isearch`.
- `emacs`: `event, frame?, reason?`; `frame-opened`, `frame-closed`, `daemon-restarted`, `restarting`, `suspended`, `resumed`, `unresponsive`, `responsive`, `stopping`.
- `file`: `path, event: created|modified|deleted`.
- `check`: as the RPC; `unknown: true` (with `ok: false`) when unevaluable; `detail` is evaluator output.
- `step`: `lessonId, stepId, status, index (0-based), count, lessonComplete?, lap?, lapMs?` (`lapMs` on the event that ends a lap).
- `nudge`: `id` (stable per rule), `kind: calm|ask, title, body`.
- `checkpoint`: `label, ref, createdAt`. `mode`: `mode`. `modules`: `enabled, restarting` (`true`, then `false` once the lesson that asked for the modules is set up).
- `gap`: `from, to`; resync (`rpc check`) or reconnect.
- `error`: `code, message` for the learner (fixtures, setup, `emacs_unavailable`).

### 4.1 Step lifecycle

`step` statuses (engine: backend.md §3.8):

- `loaded`: scenario load or advance; the step's checkpoint exists.
- `reset`: `restore {}` or a restoring `demo start`; command counts and checks cleared.
- `complete`: all conditions hold without a latch; progress `done`; next step `loaded`, or `lessonComplete`.
- `demonstrated`: all conditions hold under the latch (once per latch); progress `demonstrated`; no advance; the browser offers Do it yourself (`restore {}`).
- `skipped`: after `skip`; progress `skipped`; continues as `complete`.
- `dropped`: `drop-step` forgot the active step.

**Watch it**: `demo start` restores the checkpoint if a command ran since the step started or was reset (`restored: true`, no reset counted), then sets the latch, which lasts until `restore {}`, a reload of the step or `drop-step`. Playing ends with `demo end`, 10 s without `keys` (`steps.DemoIdle`) or the page's terminal closing; if the step had not completed, the latch is dropped then. Stuck rules pause while playing. Open: under the latch the learner's own completion gets no reaction until Do it yourself.

**Skip**: restores like `demo start`, plays the demo into the learner's frame at playctl pacing, waits ≤ 8 s for the conditions (`demoCompleted: false` is a curriculum defect), records `skipped`, advances; ≤ 60 s, even if the browser leaves.

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
- `waiting` while a loop reads its own keys: `{kind: menu|avy|which-key, what, leave}`; Rescue sends `leave` first (`a` for which-key, where C-g only returns to the prefix).

## 6. Scenario object

Parsed `scenario.yaml` (CURRICULUM.md B.3, `protocol.Scenario`), served with ids assigned (`Scenario.AssignIDs`):

- Condition ids default to `<stepId>/<n>` (1-based, top level), stuck rules `<stepId>/stuck/<n>`; an explicit `id` wins.
- Conditions: `event {command, min?, since?}`, `elisp`, `file {path, host?, exists|contains|not_contains|regex|json_path+equals}`, `shell {run, cwd?, timeout?, host?}`, `buffer {name, mode?, contains?, process?, modified?}`, `layout`, `state` (`protocol.Predicate` over §5); `all`, `any`, `not`.
- Stuck rule `when` (all must hold): `minibuffer_seconds`, `idle_seconds`, `step_seconds`, `quits_in_window {count, seconds}`, `unbound_repeats`.
- `title`, `goal`, `detail`, `text`, `hint`, `say`: a string or a locale map.
- Demos (`protocol.ParseDemo`): Emacs keys, `"typed text"` (`\"`, `\\`, `\n`, `\t`), `~N` pauses; played at 170 ms per chord, 70 ms per character (`protocol.PlayDemo`).

## 7. Agent protocol (Unix socket, JSON lines)

One JSON object per line, ≤ 1 MB (`protocol.MaxAgentLine`); a new connection replaces the old.

Agent → sessiond: `hello {protocol, agent, emacs, pid, modules?, moduleErrors?}`; `key`, `state`, `echo`, `minibuffer` as in §4; `frame {event: opened|closed, kind}`, `closed` only when a terminal's last frame goes; `ping` every 2 s; `reply {id, ok, value?, error?}`.

sessiond → agent: `pong`, `call {id, op, args, timeoutMs}`, serialised; a prompting op fails instead of blocking (emacs.md §10.4). Ops:

- `eval {form}` → `{value}`; `check {forms}` → `{results}` of `true|false|"unknown"` (`protocol.Truth`; 1 s per form).
- `rescue` → `{did}`; `snapshot` → `WindowSpec`; `open WindowSpec` → `{opened, layoutDeferred?}`.
- `reset-step {spec}` → `{reverted, killed}`; `setup {open?, layout?, eval?, compile?, workspace, reset?, root?}` → `{ok}`.
- `set-modules {enabled}` → `{written}` (sessiond then restarts); `frame {kind, display?}` → `{frame}`; `describe {keys}|{cmd}` → `{cmd, doc}`.

`WindowSpec`: `files:[{path, point, visible}]` (most recent first, workspace-relative), `layout` (used without `windowState`), `windowState` and `specialBuffers` (printed, opaque to sessiond), `selected`, `tab`, `tabs`, `tabIndex`, `takenAt`; semantics in emacs.md §10.3.

The agent never blocks editing: it queues output while the socket is stuck and reconnects with backoff (emacs.md §10.4).

## 8. Type generation

`playctl protocol typescript` (`internal/protocol/tsgen`: reflection plus the Go AST) renders string-literal enums, an `Event` union on `t` and an `RpcOps` request/result map. `go test ./internal/protocol/...` fails when it is stale; `make protocol` regenerates it. `deploy/config.schema.json` comes from invopop/jsonschema.
