# API reference

The contracts between the browser, the gateway, sessiond and the Emacs agent. Go types in `internal/protocol` are the source of truth; TypeScript types are generated from them (backend.md §4).

## 1. Conventions

- Base URLs: gateway `https://DOMAIN` (server) or `http://localhost:8080` (local). Session endpoints are reached through the gateway at `/s/{sessionId}/…`.
- JSON everywhere; UTF-8; timestamps are milliseconds since the Unix epoch (`ts`); ids are opaque strings.
- Errors: `application/problem+json` with `{status, code, message, details?}`. Stable codes: `unauthenticated`, `forbidden`, `not_found`, `invalid_request`, `session_limit`, `session_unavailable`, `emacs_unavailable`, `timeout`, `conflict`, `internal`.
- Authentication: cookie `pg_session`. State-changing requests and WebSocket upgrades must send an `Origin` header matching the public URL.
- Versioning: the REST prefix is `/api/v1`; WebSocket and agent messages carry `protocol: 1` in their `hello`. Breaking changes bump both.
- Pagination is not needed in v1; lists are small.

## 2. Gateway API (`/api/v1`)

### 2.1 Authentication

| Method and path | Body | Response |
|---|---|---|
| `POST /auth/invite` | `{code}` | `204`, sets cookie; `403 invalid_request` on bad code |
| `GET /auth/oidc/start` | | `302` to the provider |
| `GET /auth/oidc/callback` | | `302 /` with cookie, or `401` |
| `POST /auth/logout` | | `204`, clears cookie |
| `GET /me` | | `{user:{id, displayName, kind}, mode:"local"|"server", authMode, features:{gui, remoteBox}}` |

### 2.2 Curriculum

| Method and path | Response |
|---|---|
| `GET /curriculum` | the index: `{version, parts:[{n, id, title, minutes, outcome, modules, lessons:[{id, n, title, minutes, mode, tags, vscode}]}], projects:[…], tables:{exits, habits}}` |
| `GET /curriculum/lessons/{lessonId}` | `{id, title, markdown, scenario, assets:[…]}` where `scenario` is the parsed scenario (curriculum-format in CURRICULUM.md Part C) |
| `GET /curriculum/assets/{lessonId}/{path}` | lesson images |

### 2.3 Sessions

| Method and path | Body | Response |
|---|---|---|
| `POST /sessions` | | `201 {id, state, createdAt, image, gui:bool}` (idempotent per user) |
| `GET /sessions/{id}` | | `{id, state, lastActiveAt, emacs:{version, uptime}, mode, step}` (last four from sessiond `/status` when running) |
| `DELETE /sessions/{id}` | | `204`; stops the container, keeps the volume |
| `POST /sessions/{id}/restart-emacs` | | proxied to `/rpc/restart-emacs` |

States: `creating`, `starting`, `ready`, `idle`, `stopping`, `stopped`, `failed`.

### 2.4 Progress

| Method and path | Body | Response |
|---|---|---|
| `GET /progress` | | `{lessons:{[lessonId]:{status, counts:{done, demonstrated, skipped, total}, steps:{[stepId]:{status, hintsUsed, resets, completedAt}}}}, updatedAt}` |
| `POST /progress/reset` | `{lessonId}`, or no body | `204`. With a lesson id, clears that lesson's progress; with no body, `{}` or an empty `lessonId`, clears every lesson of the user ("Start over"). sessiond is then told to drop the active step: `drop-step {lessonId}` or `drop-step {}` (§3.4) |

Step statuses: `not-started`, `in-progress`, `demonstrated` (completed while a "Watch it" demonstration was latched, §4.1), `skipped` (passed with "Skip this step"), `done` (completed by the learner). A step's stored status only rises, in the order `not-started` < `in-progress` < `demonstrated` < `skipped` < `done`; only a reset clears it. `completedAt` is set for `done`. A lesson's `status` is `done` only when every step of its scenario is `done`, `in-progress` when any step is above `not-started`, else `not-started`. `counts.total` is the number of steps in the bundle's scenario (0 when the lesson is not in the bundle); the other counts are steps with that status. Resuming a lesson starts at the first step that is neither `done` nor `skipped`.

"Start over" resets progress only: workspace files, checkpoints and the module set are kept.

### 2.5 Stuck reports

`POST /stuck` with `{sessionId, lessonId, stepId, note}` makes the gateway ask sessiond for its last 200 events and stores them. The browser calls this from the "I'm stuck" button. Response `201 {id}`.

### 2.6 Internal (sessiond → gateway)

Authenticated with `Authorization: Bearer <PG_SESSION_TOKEN>`; reachable only on the sessions networks (requests carrying `X-Forwarded-For` or `Forwarded`, which arrive through Caddy, get 404 in server mode). The learner can read their own session's token, so the bodies are not trusted: progress is accepted only for lessons and steps of the loaded curriculum bundle (400 otherwise), and stuck reports count against the same per-user hourly limit as `POST /stuck` (429 beyond it).

| Method and path | Purpose |
|---|---|
| `GET /internal/curriculum/scenarios/{lessonId}` | scenario JSON |
| `GET /internal/curriculum/fixtures/{lessonId}` | `application/zstd` tarball; `ETag` honoured |
| `POST /internal/sessions/{id}/progress` | `{lessonId, stepId, status, hintsUsed?, resets?, at?}`; `status` is `in-progress`, `demonstrated`, `skipped` or `done` |
| `POST /internal/sessions/{id}/stuck` | `{lessonId, stepId, note, events:[…]}` |
| `POST /internal/sessions/{id}/heartbeat` | `{activeWebSockets}` every 30 s (belt and braces for the reaper) |

## 3. Session API (`/s/{id}`)

Proxied verbatim by the gateway to sessiond. Inside the container the same paths are served on `:7000` without the prefix. The gateway adds `X-Playground-User` and, in server mode, `X-Playground-Proxy` with the session's proxy secret; a sessiond started with `PG_PROXY_SECRET` answers every request without the right value with 401 `unauthenticated`, except `GET /healthz` (deployment.md §9.3).

### 3.1 `GET /pty` (WebSocket)

Query: `cols`, `rows`. Binary frames carry terminal bytes in both directions. Text frames are control messages:

```json
{"t":"resize","cols":120,"rows":36}
{"t":"ping"}
```

Server text frames: `{"t":"attached"}` once tmux is attached, `{"t":"replaced"}` before closing when another viewer took over. Close codes: `4000` replaced, `4001` emacs unavailable.

### 3.2 `GET /events` (WebSocket)

Query: `after=<seq>` to replay missed events. Server to client only, JSON text frames, one event per frame (§4). The first frame is always `hello`, followed by a full `state`. Client may send `{"t":"ping"}`.

### 3.3 `GET /vnc` (WebSocket)

Binary frames carry the RFB stream to `Xvnc`. Available only after `mode` is `gui`; otherwise closes with `4002 gui_not_active`.

### 3.4 RPC (`POST /rpc/{op}`)

Request body JSON; response `{"ok":true,"result":…}` or a problem+json error. All calls are idempotent unless noted.

| Op | Body | Result | Notes |
|---|---|---|---|
| `keys` | `{keys:"C-x C-f", frame?:"terminal"|"gui"}` | `{}` | tmux `send-keys` or xdotool; not idempotent |
| `eval` | `{form:"(buffer-name)", timeoutMs?:2000}` | `{value:any}` | JSON via `json-serialize`; non-serialisable values become strings |
| `rescue` | `{}` | `{did:["cancelled the prompt", …]}` | `playground-rescue` |
| `scenario` | `{lessonId, stepId?, reset?}` | `{step:{…}, index, count, modules, restarting}` | loads fixtures, modules, setup; takes checkpoint. With a `stepId` after the first step ("Go to this step", "Do this step again"), after the setup sessiond restores that step's checkpoint when it exists, else fast-forwards by playing the earlier steps' demos itself (nothing recorded) and then takes the step's checkpoint; a step checkpoint is never overwritten with the lesson's initial state. Without `stepId` it resumes at the first step neither `done` nor `skipped` |
| `check` | `{}` | `{conditions:[{id, ok, detail?}], complete}` | force an evaluation now |
| `hint` | `{stepId, level}` | `{}` | records hint usage for progress |
| `checkpoint` | `{label}` | `{ref, createdAt}` | manual checkpoint (projects) |
| `checkpoints` | `{}` | `{items:[{label, ref, createdAt, files}]}` | |
| `restore` | `{label}` or `{ref}`, or `{}` for the active step's checkpoint | `{restoredFiles:n}` | git restore + `reset-step`; not idempotent. `{}` is "Reset this step" and "Do it yourself": it clears the step's demo latch and emits `step {status:"reset"}` |
| `mode` | `{mode:"terminal"|"gui"}` | `{mode}` | starts Xvnc and the GUI frame on demand |
| `modules` | `{enabled:["base","agent","completion"]}` | `{restarting:bool}` | rewrites `modules.el`; daemon restart with buffer restore |
| `restart-emacs` | `{}` | `{}` | last resort; buffer restore from the last snapshot |
| `stuck` | `{note}` | `{events:[…]}` | returns the last 200 events and files a report |
| `status` | | `{emacs:{version, pid, uptimeMs, agent:bool}, frames:{terminal:bool, gui:bool}, mode, step, modules}` | also `GET /status` |
| `open` | `{path}` | `{buffer}` | visits a workspace-relative file in the learner's window (the project file tree); no author mode needed |
| `describe` | `{keys}` | `{cmd, doc}` | ticker glosses for commands missing from `tables.gloss` |
| `drop-step` | `{lessonId}` or `{}` | `{}` | internal: sent by the gateway after `POST /progress/reset`; the proxy refuses it from browsers. With a lesson id, forgets that lesson's saved progress and the active step if it belongs to it; with `{}`, forgets all saved progress and the active step. A forgotten active step emits `step {status:"dropped"}` |
| `demo` | `{stepId, phase:"start"\|"end"}` | `{restored:bool}` | the browser's "Watch it" (§4.1). `409` unless `stepId` is the active step. `start`: when a command ran since the step started or was last reset, restores the step's checkpoint first (as `restore {}`, emitting `step {status:"reset"}`, without counting a reset; `restored: true`), then sets the step's demo latch. `end`: the player finished or was stopped; the latch stays |
| `skip` | `{stepId}` | `{next:stepId\|null, demoCompleted:bool}` | "Skip this step" (§4.1). `409` unless `stepId` is the active step; not idempotent; lasts as long as the demo (at most 60 s). Restores the checkpoint when a command ran since the step started, plays the step's `demo` into the frame the learner sees (tmux, or xdotool in GUI mode) at playctl pacing (170 ms per chord, 70 ms per character) without recording a completion, records `skipped`, emits `step {status:"skipped"}`, takes the next step's checkpoint and emits its `loaded`. `demoCompleted` tells whether the conditions held within 8 s after the demo |

### 3.5 `GET /preview/{path}`

Serves a workspace file. HTML gets a sandboxing CSP; everything is `no-store`. `GET /preview/{dir}/` returns a JSON listing `{entries:[{name, dir, size, mtime}]}` for the file tree.

### 3.6 `GET /healthz`

`200` when the HTTP server is up and the agent has connected; `503` otherwise, with `{reason}`.

## 4. Event catalog (`/events`)

Common envelope: `{"t":"<type>","seq":123,"ts":1761472000123, …fields}`.

| Type | Fields | When |
|---|---|---|
| `hello` | `protocol:1, sessionId, mode, emacs:{version, uptimeMs}, frames:{terminal, gui}, step:{lessonId, stepId}|null, modules:[…], replayed:bool` | on connect |
| `key` | `keys, cmd, buf, mode, frame, prefixArg?` | after each command (agent) |
| `prefix` | `keys, source:"tap"|"agent"` | a prefix is pending (tap); `keys: ""` clears it. A `prefix` never follows the `key` event of the command that consumed its keys (the tap drops a lone ESC that Emacs already used) |
| `state` | see §5 | after commands and UI changes (agent), coalesced |
| `echo` | `text` | echo area message of interest (agent filters noise) |
| `minibuffer` | `open:bool, prompt, depth, kind:"prompt"|"isearch"` | minibuffer enter or exit |
| `emacs` | `event:"frame-closed"|"frame-opened"|"daemon-restarted"|"suspended"|"resumed"|"unresponsive"|"stopping", frame?:"terminal"|"gui", reason?` | supervisor |
| `file` | `path, event:"created"|"modified"|"deleted"` | watcher |
| `check` | `lessonId, stepId, conditions:[{id, ok, detail?}], complete` | step engine |
| `step` | `lessonId, stepId, status:"loaded"|"complete"|"reset"|"demonstrated"|"skipped"|"dropped", index, count, lessonComplete?` | step engine (§4.1) |
| `nudge` | `id, kind:"calm"|"ask", title, body` | stuck rules (server-side ones; the browser adds its own) |
| `checkpoint` | `label, ref, createdAt` | checkpoint taken |
| `mode` | `mode` | frame switch |
| `modules` | `enabled, restarting` | module change |
| `gap` | `from, to` | subscriber overflow; the browser should request a `state` via `check` or reconnect |
| `error` | `code, message` | anything the learner should see |

### 4.1 Step lifecycle

The step engine (backend.md §3.8) moves the active step through these events:

| Event status | When | Afterwards |
|---|---|---|
| `loaded` | a scenario load, or the engine advanced | the step is active; its checkpoint exists |
| `reset` | `restore {}` (Reset this step, Do it yourself), or `demo start` restored the checkpoint | the step is active again; checks for it are cleared |
| `complete` | every condition holds and no demo latch is set | progress `done`; the engine takes the next step's checkpoint and emits its `loaded`, or sets `lessonComplete` on the last step |
| `demonstrated` | every condition holds while the demo latch is set (emitted once per latch) | progress `demonstrated`; the step stays active, no advance; the browser offers "Do it yourself" (`restore {}`) |
| `skipped` | `skip` finished | progress `skipped`; the next step's `loaded` follows (or `lessonComplete` on the last step) |
| `dropped` | `drop-step` forgot the active step (a progress reset) | no step is active until the next `scenario` |

The demo latch is set by `demo {phase:"start"}` and cleared by `restore {}`, a `scenario` load of the step and `drop-step`; only a completion without the latch counts as `done`. Stuck rules are not evaluated while a demo plays (between `demo start` and `demo end`). Keys sent with `keys` (inline "try it", playctl) are learner input.

Example:

```json
{"t":"key","seq":418,"ts":1761472000123,"keys":"C-x C-f","cmd":"find-file","buf":"main.rs","mode":"rust-ts-mode","frame":"terminal"}
```

## 5. State object

Emitted by the agent, forwarded unchanged.

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
  "readOnlyReason": null, "diagAtPoint": null
}
```

`minibuffer`, when open: `{"depth":1,"prompt":"Find file: ","contents":"~/hello/src/","kind":"prompt"}`. `layout` is a nested list mirroring `window-state-get` simplified to splits and window ids; the browser uses it for layout checks in Part 3 lessons.

## 6. Scenario object

The parsed form of `scenario.yaml` (CURRICULUM.md Part C), as sent by `GET /curriculum/lessons/{id}` and used by sessiond. Condition results refer to conditions by their index-derived `id` (`<stepId>/<n>`) unless an explicit `id` is given.

## 7. Agent protocol (Unix socket, JSON lines)

Agent → sessiond:

| Type | Fields |
|---|---|
| `hello` | `protocol:1, agent:"0.1.0", emacs:"31.1", pid` |
| `key` | as in §4 without `seq`/`ts` (sessiond adds them) |
| `state` | §5 |
| `echo` | `text` |
| `minibuffer` | §4 |
| `frame` | `event:"opened"|"closed", kind:"terminal"|"gui"` |
| `ping` | every 2 s |
| `reply` | `id, ok:bool, value?, error?` |

sessiond → agent:

| Type | Fields | Agent behaviour |
|---|---|---|
| `call` | `id, op, args, timeoutMs` | runs the op, replies |
| `pong` | | |

Ops: `eval {form}`, `check {forms:[…]}` → `{results:[bool|"unknown"]}`, `rescue` → `{did:[…]}`, `reset-step {spec}`, `snapshot` → buffer and window spec, `set-modules {enabled}` (writes `modules.el`, replies, then sessiond restarts the daemon), `open {files, layout}`, `frame {kind:"gui"}`, `describe {keys}` → `{cmd, doc}`.

Transport rules: UTF-8 JSON, one object per line, no line longer than 1 MB. The agent must never block editing: writes are asynchronous through `process-send-string` on a non-blocking process, and a disconnected socket drops events (not commands) after buffering 1000.

## 8. Type generation

`internal/protocol` structs carry `json` and `jsonschema` tags. `playctl protocol typescript` renders them through `invopop/jsonschema` and `json-schema-to-typescript` into `web/src/protocol.gen.ts`. CI regenerates and fails on diff.
