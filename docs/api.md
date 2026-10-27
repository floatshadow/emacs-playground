# API reference

The contracts between the browser, the gateway, sessiond and the Emacs agent. The Go types in `internal/protocol` are the source of truth (their doc comments carry the field-level detail); `web/src/protocol.gen.ts` is generated from them (§8).

## 1. Conventions

- Base URLs: gateway `https://DOMAIN` (server) or `http://localhost:8080` (local). Session endpoints are reached through the gateway at `/s/{sessionId}/…`.
- JSON everywhere; UTF-8; timestamps are milliseconds since the Unix epoch; ids are opaque strings.
- Lesson ids are the lesson directory relative to `curriculum/` and contain one slash (`02-buffers-and-files/03-opening-files`, `projects/p6-polyglot`). They go into URLs literally; routes use trailing wildcards (`{lessonId...}`).
- Errors: `application/problem+json` with `{status, code, message, details?}`. Stable codes (`protocol.ErrorCode`): `unauthenticated` 401, `forbidden` 403, `not_found` 404, `invalid_request` 400, `conflict` 409, `rate_limited` 429, `session_limit` 429/503, `session_unavailable` 502/503, `emacs_unavailable` 503, `timeout` 504, `not_implemented` 501, `internal` 500. Clients treat unknown codes like `internal`.
- Authentication: cookie `pg_session`. State-changing requests and WebSocket upgrades must send an `Origin` matching the public URL (in local mode the loopback names `localhost` and `127.0.0.1` are interchangeable).
- Versioning: the REST prefix is `/api/v1`; WebSocket and agent messages carry `protocol: 1` in their `hello`. Breaking changes bump both.

## 2. Gateway API (`/api/v1`)

### 2.1 Authentication

| Method and path | Body | Response |
|---|---|---|
| `GET /auth/config` | | unauthenticated: `{mode, authMode, oidcStartUrl?}`, what the login screen offers |
| `POST /auth/invite` | `{code}` | `204`, sets the cookie; `403 forbidden` for an unknown, expired or used-up code; `429` per IP and per code; `409 conflict` when auth mode is not `invite` (`none` or `oidc`). An already signed-in browser keeps its user and uses up nothing |
| `GET /auth/oidc/start`, `GET /auth/oidc/callback` | | `501 not_implemented` in slice 1 (`404` when auth mode is not `oidc`) |
| `POST /auth/logout` | | `204`, clears the cookie |
| `GET /me` | | `{user:{id, displayName, kind}, mode, authMode, features:{gui, remoteBox, stuckReports, authorMode}, version}`; `401` when not signed in |

### 2.2 Curriculum

| Method and path | Response |
|---|---|
| `GET /curriculum` | the index (`protocol.CurriculumIndex`): `{format, version, locale, parts:[{n, id, title, minutes, outcome, modules, status, lessons:[{id, n, title, minutes, mode, tags, vscode, status, steps}]}], projects:[…], tables:{exits, habits, gloss, cheats, glossary}}` |
| `GET /curriculum/lessons/{lessonId}` | `{id, title, locale, markdown, front, scenario, assets}`; `scenario` is the parsed scenario with ids assigned (§6) |
| `GET /curriculum/assets/{lessonId}/{path}` | lesson images |

The locale is `?locale=`, else the best `Accept-Language` match, else `curriculum.defaultLocale`. Responses carry strong ETags. Without a loaded bundle these endpoints answer `503`.

### 2.3 Sessions

| Method and path | Body | Response |
|---|---|---|
| `POST /sessions` | | `201 SessionInfo` (idempotent per user; rate limited) |
| `GET /sessions/{id}` | | `SessionInfo`; `emacs`, `mode` and `step` come from sessiond's `/status` when running |
| `DELETE /sessions/{id}` | | `204`; stops the container, keeps the volume |
| `POST /sessions/{id}/restart-emacs` | | proxied to `/rpc/restart-emacs` |

`SessionInfo` is `{id, state, createdAt, lastActiveAt?, image?, gui, failure?, emacs?:{version, uptimeMs}, mode?, step?, updateAvailable?}`; `updateAvailable` is true when the session runs an older image than the configured one. States: `creating`, `starting`, `ready`, `idle`, `stopping`, `stopped`, `failed`, `removed`.

### 2.4 Progress

| Method and path | Body | Response |
|---|---|---|
| `GET /progress` | | `{lessons:{[lessonId]:{status, counts:{done, demonstrated, skipped, total}, steps:{[stepId]:{status, hintsUsed, resets, completedAt?}}}}, updatedAt}` |
| `POST /progress/reset` | `{lessonId}`, or no body | `204`. With a lesson id, clears that lesson; with no body, `{}` or an empty `lessonId`, clears every lesson of the user ("Start over"). The gateway then sends the user's running session `drop-step` with the same `lessonId` (§3.4) |

Step statuses, lowest first: `not-started`, `in-progress`, `demonstrated` (completed while a "Watch it" demo latch was set, §4.1), `skipped` (passed with "Skip this step"), `done` (completed by the learner). A step's stored status only rises in that order; only a reset clears it. `completedAt` is set for `done`. `hintsUsed` and `resets` are absolute totals for the step.

A lesson appears once any of its steps has a record. Its `status` is `done` only when every step of the bundle's scenario is `done`, else `in-progress` when any step is above `not-started`. `counts.total` is the number of steps in the bundle's scenario (0 when the lesson is not in the bundle). Resuming a lesson starts at the first step that is neither `done` nor `skipped` (a `demonstrated` step is practised).

"Start over" resets progress only: workspace files, checkpoints and the module set are kept.

### 2.5 Stuck reports

`POST /stuck` with `{sessionId, lessonId?, stepId?, note}` (the "I'm stuck" button): the gateway calls the session's `rpc stuck` for its recent events and stores the report. `201 {id}`; `403` when `features.stuckReports` is off; `429` beyond the per-user hourly limit. A stored report keeps at most the newest 200 events within 512 KiB and a 4000-byte note; identical reports (user, lesson, step, note) within 30 s are merged.

### 2.6 Internal (sessiond → gateway)

Authenticated with `Authorization: Bearer <PG_SESSION_TOKEN>`; the token must belong to the `{id}` in the path. In server mode requests carrying `X-Forwarded-For` or `Forwarded` (everything that arrives through Caddy) get `404`. The learner can read their own session's token, so bodies are not trusted: progress is accepted only for lessons and steps of the loaded bundle (`400` otherwise), and stuck reports count against the same per-user hourly limit as `POST /stuck`.

| Method and path | Purpose |
|---|---|
| `GET /internal/curriculum/scenarios/{lessonId}` | scenario JSON |
| `GET /internal/curriculum/fixtures/{lessonId}` | `application/zstd` tarball, `204` when the lesson has none; `ETag` honoured |
| `POST /internal/sessions/{id}/progress` | `ProgressReport {lessonId, stepId, status, hintsUsed?, resets?, at?}`; `at` becomes `completedAt` for `done` |
| `POST /internal/sessions/{id}/stuck` | `{lessonId?, stepId?, note, events:[…]}` |
| `POST /internal/sessions/{id}/heartbeat` | `{activeWebSockets}` every 30 s (presence for the reaper) |

## 3. Session API (`/s/{id}`)

Proxied by the gateway to sessiond, which serves the same paths on `:7000` without the prefix. The proxy strips the cookie and `Authorization`, sets `X-Playground-User`, and refuses `rpc eval` unless author mode is on and `rpc drop-step` always (it is internal).

Every request the gateway sends to a docker-backend session (proxy, its own RPC calls, `/status`) carries `X-Playground-Proxy` with the session's proxy secret (backend.md §2.3). A sessiond started with `PG_PROXY_SECRET` answers `401 unauthenticated` to any request without it, except `GET`/`HEAD /healthz`, so one learner's container cannot drive another learner's sessiond. The static (local) session has no secret and serves every request.

### 3.1 `GET /pty` (WebSocket)

Query: `cols`, `rows`. Binary frames carry terminal bytes in both directions. Text frames are control messages: client `{"t":"resize","cols":120,"rows":36}` and `{"t":"ping"}`; server `{"t":"attached"}` once tmux is attached, `{"t":"pong"}`, and `{"t":"replaced"}` before closing when another viewer took over.

Close codes (all session WebSockets): `4000` replaced by a new viewer, `4001` Emacs or tmux unavailable (on `/vnc`: Xvnc exited while GUI mode is on; reconnect), `4002` GUI not active, `4003` session stopping.

### 3.2 `GET /events` (WebSocket)

Query: `after=<seq>` replays missed events still in the ring buffer. Server to client, one JSON event per text frame (§4). The first frame is always `hello` (with `replayed: true` when a replay follows), then a full `state`. The client may send `{"t":"ping"}`; anything else is ignored.

### 3.3 `GET /vnc` (WebSocket)

Binary frames carry the RFB stream to Xvnc; the `binary` subprotocol is negotiated when offered (noVNC). Open only while `mode` is `gui`: otherwise, and when the learner switches back to the terminal, it closes with `4002`.

### 3.4 RPC (`POST /rpc/{op}`)

Request body JSON; response `{"ok":true,"result":…}` or a problem+json error. Ops are idempotent unless noted. Ops that need Emacs answer `503 emacs_unavailable` when the agent is not connected and `504 timeout` when it does not answer; ops about the active step answer `409 conflict` when there is none or another step is named.

| Op | Body | Result | Notes |
|---|---|---|---|
| `keys` | `{keys:"C-x C-f"}` or `{text:"hello"}`, `frame?` | `{}` | Exactly one of `keys` (Emacs notation, validated) and `text` (typed literally, at most 4096 bytes). Sent with tmux `send-keys` or xdotool into `frame` (default: the current mode; `409` if that frame is not active). Counts as learner input. Not idempotent |
| `eval` | `{form, timeoutMs?}` | `{value}` | Author mode only (`403` otherwise, in sessiond and in the proxy). Timeout default 2000, max 10000 ms. The value is JSON; alists with symbol keys become objects, unserialisable values their printed form |
| `open` | `{path}` | `{buffer}` | Visits a workspace-relative file in the learner's window (the project file tree); the path is checked to stay in the workspace, so no author mode is needed |
| `rescue` | `{}` | `{did:["cancelled the prompt", …]}` | `playground-rescue` (emacs.md §10.3). When Emacs is unresponsive or the agent times out, sends C-g through tmux instead (`did: ["interrupted Emacs (C-g)"]`) |
| `scenario` | `{lessonId, stepId?, reset?}` | `{step:{lessonId, stepId}, index, count, modules, restarting}` | Loads a lesson (backend.md §3.12): fixtures, modules, setup, checkpoint. Without `stepId` it starts at the first step neither `done` nor `skipped`. With a later `stepId` ("Go to this step") the step starts from its checkpoint, or from a fast-forward through the earlier steps' demos; the RPC returns when the step is active. Reloading the active lesson is a no-op. `restarting: true` when the module set changed: the step activates after the daemon restart |
| `check` | `{}` | `{lessonId, stepId, conditions:[{id, ok, unknown?, detail?}], complete}` | Evaluates the active step now |
| `hint` | `{stepId, level}` | `{}` | Records hint usage (`level` ≥ 1) for progress |
| `checkpoint` | `{label}` | `{ref, createdAt}` | Manual checkpoint (projects); label at most 200 characters |
| `checkpoints` | `{}` | `{items:[{label, ref, createdAt, files, step?}]}` | Newest first; `files` are the paths a restore would overwrite or delete |
| `restore` | `{label}`, `{ref}`, or `{}` | `{restoredFiles}` | Restores a checkpoint (backend.md §3.9). `{}` is the active step's checkpoint: "Reset this step" and "Do it yourself"; it counts a reset, clears the step's demo latch and emits `step {status:"reset"}`. Not idempotent |
| `mode` | `{mode:"terminal"\|"gui"}` | `{mode}` | Starts Xvnc and the GUI frame on demand; `409` when the GUI is disabled |
| `modules` | `{enabled:[…]}` | `{restarting}` | Sets the module set exactly (unlike `scenario`, which only adds) and restarts the daemon when it changed |
| `restart-emacs` | `{}` | `{}` | Restarts the daemon in the background with buffer restore; progress arrives as `emacs` events |
| `stuck` | `{note}` | `{events:[…]}` | The last 200 events, oldest first, as sent on `/events`. Called by the browser through the proxy (the request carries `X-Forwarded-For`), sessiond also files the report with the gateway; called by the gateway (`POST /stuck`) it only returns the events |
| `status` | `{}` | `{sessionId, emacs:{version, pid, uptimeMs, agent, responsive}, frames:{terminal, gui}, mode, step, modules, versions?, activeWebSockets}` | Also `GET /status`; `versions` are the image's toolchain pins |
| `describe` | `{keys}` | `{cmd, doc}` | What a key sequence runs and the first docstring line (ticker glosses for commands missing from `tables.gloss`); `cmd` is `""` when unbound or a prefix |
| `demo` | `{stepId, phase:"start"\|"end"}` | `{restored}` | The browser's "Watch it" (§4.1). `start`: when a command ran since the step started or was last reset, first restores the step's checkpoint (as `restore {}`, emitting `step reset`, without counting a reset; `restored: true`), then sets the demo latch. `end`: the player finished or was stopped; the latch stays. Not idempotent |
| `skip` | `{stepId}` | `{next:stepId\|null, demoCompleted}` | "Skip this step" (§4.1). Restores the checkpoint when a command ran, plays the step's `demo` into the frame the learner sees at playctl pacing without recording a completion, waits up to 8 s for the conditions (`demoCompleted`; false is a curriculum defect and is logged), records `skipped` and advances. Bounded by 60 s and finishes even if the browser goes away; `409` while another skip runs. Not idempotent |
| `drop-step` | `{lessonId}` or `{}` | `{}` | Internal: sent by the gateway after `POST /progress/reset`. With a lesson id, forgets that lesson's saved progress and the active step if it belongs to it; with `{}`, all saved progress and the active step. A forgotten active step emits `step {status:"dropped"}` |

### 3.5 `GET /preview/{path}`

Serves a workspace file through `os.Root` (no escape through `..` or symlinks; `.playground/` is never served). Everything is `no-store` and `nosniff`; HTML and other active documents get `Content-Security-Policy: sandbox allow-scripts allow-same-origin`. `GET /preview/{dir}/` returns a JSON listing `{path, entries:[{name, dir, size, mtime}]}` for the file tree.

### 3.6 `GET /healthz`

`200 {ok:true}` once the agent has said hello for the first time; `503 {reason}` before that (`"Emacs is starting"`) and after the supervisor gave up restarting Emacs. The HTTP server starts before Emacs, so the gateway and Compose can poll from the first second.

## 4. Event catalog (`/events`)

Common envelope: `{"t":"<type>","seq":123,"ts":1761472000123, …fields}`. `seq` is monotonic per sessiond process, starting at 1. Consumers ignore unknown types.

| Type | Fields | When |
|---|---|---|
| `hello` | `protocol:1, sessionId, mode, emacs:{version, uptimeMs}, frames:{terminal, gui}, step:{lessonId, stepId}\|null, modules, replayed` | on connect |
| `key` | `keys, cmd, buf, mode, frame, prefixArg?, mx?` | after each command (agent). `cmd` is `real-this-command` (emacs.md §10.1), `"<lambda>"` for anonymous commands, `"undefined"` for an unbound sequence. `mx: true` when run by name through M-x; `keys` is then the reader's keys |
| `prefix` | `keys, source:"tap"\|"agent"` | a prefix is pending (input tap, backend.md §3.6); `keys: ""` clears it. Advisory: the next `key` supersedes it. A `prefix` never follows the `key` of the command that consumed its keys |
| `state` | §5 | after commands and UI changes (agent), coalesced, duplicates suppressed |
| `echo` | `text` | echo-area message the agent did not filter as noise (at most 500 characters) |
| `minibuffer` | `open, prompt, depth, kind:"prompt"\|"isearch"` | minibuffer or isearch enter and exit |
| `emacs` | `event, frame?, reason?` | supervisor. `event`: `frame-opened`, `frame-closed` (with `frame`), `daemon-restarted`, `restarting`, `suspended`, `resumed`, `unresponsive`, `responsive`, `stopping` |
| `file` | `path, event:"created"\|"modified"\|"deleted"` | workspace watcher, coalesced per path at 200 ms |
| `check` | `lessonId, stepId, conditions:[{id, ok, unknown?, detail?}], complete` | step engine. `unknown: true` (with `ok: false`) when a condition could not be evaluated; `detail` is evaluator output (shell stdout up to 4 KB, the elisp error) |
| `step` | `lessonId, stepId, status, index, count, lessonComplete?` | step engine (§4.1). `index` is 0-based; `lessonComplete` is set on the `complete` or `skipped` event of the last step |
| `nudge` | `id, kind:"calm"\|"ask", title, body` | a scenario stuck rule held; `id` is stable per rule so the browser can dismiss it. The browser adds its own `habit` cards |
| `checkpoint` | `label, ref, createdAt` | checkpoint taken |
| `mode` | `mode` | frame switch |
| `modules` | `enabled, restarting` | module set change; sent with `restarting: true` and again with `false` when the restart is done |
| `gap` | `from, to` | a slow subscriber lost these events; the browser calls `rpc check` or reconnects |
| `error` | `code, message` | anything the learner should see (fixtures could not be fetched, setup failed, `emacs_unavailable` offering a restart when Rescue's C-g did not help) |

Example:

```json
{"t":"key","seq":418,"ts":1761472000123,"keys":"C-x C-f","cmd":"find-file","buf":"main.rs","mode":"rust-ts-mode","frame":"terminal"}
```

### 4.1 Step lifecycle

The step engine (backend.md §3.8) moves the active step through these `step` statuses:

| Status | When | Afterwards |
|---|---|---|
| `loaded` | a scenario load, or the engine advanced | the step is active; its checkpoint exists |
| `reset` | `restore {}` (Reset this step, Do it yourself), or `demo start` restored the checkpoint | the step is active again; its command counts and checks are cleared |
| `complete` | every condition holds and no demo latch is set | progress `done`; the engine takes the next step's checkpoint and emits its `loaded`, or sets `lessonComplete` on the last step |
| `demonstrated` | every condition holds while the demo latch is set (once per latch) | progress `demonstrated`; the step stays active and nothing advances; the browser offers "Do it yourself" (`restore {}`) |
| `skipped` | `skip` finished | progress `skipped`; the next step's `loaded` follows, or `lessonComplete` on the last step |
| `dropped` | `drop-step` forgot the active step | no step is active until the next `scenario` |

The demo latch is set by `demo start` and cleared by `restore {}`, a `scenario` load of the step and `drop-step`; only a completion without the latch counts as `done`. Stuck rules are not evaluated while a demo plays (between `demo start` and `demo end`); a demo also ends without `demo end` when no `keys` arrived for 10 s (`steps.DemoIdle`) or the terminal connection of the page closes, and if it had not completed the step its latch is dropped, so the learner's own completion counts as `done`. Keys sent with `keys` (inline "try it", playctl) are learner input.

## 5. State object

Emitted by the agent and forwarded unchanged apart from `seq` and `ts`. The browser's exit table and the step engine's `state` and `layout` conditions read it; field names are the paths their predicates use (`minibuffer.kind`, `windows.length`).

```json
{
  "t": "state", "seq": 419, "ts": 1761472000130,
  "buf": "main.rs", "file": "/work/hello/src/main.rs", "mode": "rust-ts-mode", "modeName": "Rust",
  "minors": ["eglot--managed-mode", "corfu-mode", "flymake-mode"],
  "modified": true, "readonly": false, "narrowed": false, "overwrite": false,
  "line": 9, "col": 20, "point": 182, "markActive": false,
  "windows": [
    {"id": 3, "buf": "main.rs", "mode": "rust-ts-mode", "selected": true, "dedicated": false, "side": null},
    {"id": 7, "buf": "*compilation*", "mode": "compilation-mode", "selected": false, "dedicated": false, "side": "bottom"}
  ],
  "layout": ["vsplit", 3, 7],
  "frame": "terminal", "frames": 1, "tabs": {"count": 1, "current": "main"},
  "minibuffer": null,
  "isearch": false, "recursive": 0, "macro": false, "prefixArg": null,
  "readOnlyReason": null, "diagAtPoint": null, "waiting": null
}
```

- The state describes the learner's frame (emacs.md §10.3), not whatever frame Emacs has selected.
- `minors` lists only the minor modes in `playground-agent-interesting-minor-modes`; housekeeping modes such as ws-butler are left out.
- `windows` are the live windows of that frame; `id` is a small integer stable for the window's lifetime. `side` is the `window-side` parameter or null.
- `layout` is a window id for a single window, or `["vsplit"|"hsplit", child, …]`. `vsplit` stacks children top to bottom (C-x 2, Emacs's vertical combination), `hsplit` places them side by side (C-x 3). Layout conditions match it with `protocol.MatchLayout`.
- `minibuffer`, when open: `{"depth":1,"prompt":"Find file: ","contents":"~/hello/src/","kind":"prompt"}`; isearch is reported with `kind: "isearch"`.
- `frames` is `(length (frame-list))`, including the daemon's invisible frame.
- `waiting`, while a loop that reads keys of its own runs (a menu-bar menu in the terminal, avy reading its target, which-key paging through a prefix's keys): `{"kind":"menu"|"avy"|"which-key","what":"a menu","leave":"C-g"}`, else null. `leave` is the keys that end it (`a` for which-key, where C-g only returns to the prefix); Rescue sends them first.

## 6. Scenario object

The parsed form of `scenario.yaml` (CURRICULUM.md B.3, `protocol.Scenario`), as sent by `GET /curriculum/lessons/{id}` and `/internal/curriculum/scenarios/{id}`, always with ids assigned (`Scenario.AssignIDs`):

- Condition ids default to `<stepId>/<n>` with 1-based `n` over the step's top-level conditions; stuck rule ids to `<stepId>/stuck/<n>`. An explicit `id` wins.
- Condition kinds: `event {command, min?, since?: step|lesson}`, `elisp`, `file {path, exists|contains|not_contains|regex|json_path+equals}`, `shell {run, cwd?, timeout? (s, default 5)}`, `layout` (a layout pattern), `state` (a `protocol.Predicate` over §5), and the combinators `all`, `any`, `not`.
- Stuck rule `when` fields (all must hold): `minibuffer_seconds`, `idle_seconds`, `step_seconds`, `quits_in_window {count, seconds}`, `unbound_repeats`.
- Localisable strings (`title`, `goal`, `detail`, `text`, `hint`, `say`) are a plain string or a locale map.
- Demo strings (`protocol.ParseDemo`) are whitespace-separated keys in Emacs notation, `"typed text"` with `\"`, `\\`, `\n`, `\t` escapes, and `~N` pauses. They are played at 170 ms per chord and 70 ms per character (`protocol.PlayDemo`, shared by playctl, `skip` and the fast-forward).

## 7. Agent protocol (Unix socket, JSON lines)

UTF-8 JSON, one object per line, no line longer than 1 MB (`protocol.MaxAgentLine`). sessiond keeps one agent connection; a new one replaces the old.

Agent → sessiond:

| Type | Fields |
|---|---|
| `hello` | `protocol:1, agent:"0.1.0", emacs:"31.1", pid, modules?, moduleErrors?` (modules as loaded, and load errors by module) |
| `key` | as in §4, without `seq`/`ts` (sessiond stamps them) |
| `state` | §5 |
| `echo` | `text` |
| `minibuffer` | §4 |
| `frame` | `event:"opened"\|"closed", kind:"terminal"\|"gui"`; sessiond turns it into `emacs frame-opened`/`frame-closed`. `closed` is reported only when the last frame of a terminal goes away (C-x 5 2 then C-x 5 0 in the terminal closes nothing) |
| `ping` | every 2 s; sessiond answers `pong`. Five missed pings mark Emacs unresponsive |
| `reply` | `id, ok, value?, error?` |

sessiond → agent: `call {id, op, args, timeoutMs}` and `pong`. Calls are serialised: the agent never receives a second call before it replied to the first. The agent runs each call from a timer at top level, under `with-timeout` and with `inhibit-interaction`, so an op that would prompt fails with an error reply instead of blocking the agent.

| Op | Args | Value |
|---|---|---|
| `eval` | `{form}` | `{value}` |
| `check` | `{forms:[…]}` | `{results:[true\|false\|"unknown"]}` (`protocol.Truth`); 1 s per form, error or timeout → `"unknown"` |
| `rescue` | `{}` | `{did:[…]}` |
| `snapshot` | `{}` | a `WindowSpec` (below) |
| `open` | a `WindowSpec` | `{opened, layoutDeferred?}`; `layoutDeferred` when no client frame exists yet (right after a daemon restart), the layout then goes to the next client frame |
| `reset-step` | `{spec}` | `{reverted, killed}` |
| `setup` | `{open?, layout?, eval?, compile?, workspace, reset?, root?}` | `{ok:true}` |
| `set-modules` | `{enabled}` | `{written}`; the agent writes `modules.el`, sessiond then restarts the daemon |
| `frame` | `{kind:"gui", display?}` | `{frame}` |
| `describe` | `{keys}` or `{cmd}` | `{cmd, doc}` |

`WindowSpec` (`protocol.WindowSpec`) is what a checkpoint and the periodic restart snapshot keep of Emacs's windows: `files:[{path, point, visible}]` (most recently used first, workspace-relative), `layout` (a pattern, used when `windowState` is empty), `windowState` (the printed window state, opaque to sessiond), `selected`, `tab`, `tabs`, `tabIndex`, `specialBuffers` (printed, opaque) and `takenAt`. emacs.md §10.3 describes how the agent takes and applies it.

The agent never blocks editing on the socket: writes go through `process-send-string` on a non-blocking process; while disconnected, or while sessiond has not answered a ping for 5 s, events are queued (at most 1000; the oldest events are dropped, replies never), and a peer silent for 10 s is dropped and reconnected with a 1 to 16 s backoff.

## 8. Type generation

`playctl protocol typescript` (`internal/protocol/tsgen`) renders `web/src/protocol.gen.ts` directly from the Go types: reflection for structure, the Go AST for enum constants and doc comments. It yields string-literal enums, an `Event` union discriminated by `t`, and an `RpcOps` map of request and result types per op. `go test ./internal/protocol/...` fails when the file is stale; `make protocol` regenerates it. The configuration JSON Schema (`deploy/config.schema.json`) is produced with invopop/jsonschema from the config struct.
