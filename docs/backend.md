# Backend (Go)

Two services and one CLI in a single Go module. The gateway fronts everything; sessiond runs inside each session container; playctl is the developer and CI tool. The wire contracts they implement are in api.md.

## 1. Module layout

```
go.mod                               module github.com/floatshadow/emacs-playground, go 1.25
cmd/gateway/main.go                  flag and env parsing, wiring, run
cmd/sessiond/main.go
cmd/playctl/main.go
internal/
  protocol/      event, state, RPC and scenario types; JSON tags; the single source of truth
  gateway/
    server.go    mux, middleware, static assets
    auth/        cookie sessions, invite codes, OIDC
    sessions/    Manager, Backend (docker, static), lifecycle, reaper
    proxy/       per-session reverse proxy
    store/       SQLite (server) and JSON file (local) implementations of Store
    curriculum/  bundle loading, index and lesson endpoints, fixture tarballs
    progress/    progress aggregation and reporting
  sessiond/
    server.go    mux, startup sequence, shutdown
    supervisor/  Emacs daemon, tmux, frames, Xvnc; restart policy
    pty/         tmux attach on a pty, resize, single viewer
    agent/       Unix socket listener, JSON lines codec, RPC correlation
    inputtap/    terminal byte and RFB key decoders → key names
    events/      bus, ring buffer, /events handler with replay
    steps/       scenario, step and condition evaluation
    checkpoint/  git-based checkpoints and restore
    preview/     workspace file server and fsnotify watcher
    vnc/         TCP ↔ WebSocket bridge with key tap
    scenario/    fixture fetch and extraction, setup orchestration
  docker/        thin wrapper over the Engine client used by gateway/sessions
  curriculum/    schema, loader and validator shared by gateway and playctl
  httpx/         WebSocket helpers, JSON helpers, error type, middleware
web/embed.go     embeds the built web bundle into the gateway binary
```

Rules: `cmd/*` only wires; `internal/protocol` has no dependencies beyond the standard library; packages under `sessiond` and `gateway` do not import each other; everything that talks to a subprocess or the network sits behind an interface so it can be faked in tests.

## 2. Gateway

### 2.1 Responsibilities

- Serve the web bundle and the curriculum bundle.
- Authenticate learners (none, invite code, OIDC) and issue session cookies.
- Own the session registry: ensure one session per learner, create and stop containers in server mode, point at the fixed session in local mode.
- Proxy `/s/{id}/…` to the session's sessiond, including WebSockets.
- Store progress, stuck reports and an audit log.
- Housekeeping: stop idle sessions, remove old volumes, rebuild the registry from container labels after a restart.

### 2.2 HTTP surface

| Prefix | Handler | Notes |
|---|---|---|
| `/` | static web bundle | SPA fallback to `index.html` |
| `/api/v1/*` | JSON API | api.md §2 |
| `/s/{id}/*` | reverse proxy to sessiond | authenticated, session must belong to the user |
| `/internal/*` | sessiond → gateway | bearer token per session |
| `/healthz`, `/metrics` | operations | metrics optional |

The mux is `net/http.ServeMux` with method patterns (`"POST /api/v1/sessions"`). Middleware is a plain `func(http.Handler) http.Handler` chain: request id, logging, recover, auth, origin check for state-changing requests.

### 2.3 Core interfaces

```go
// internal/gateway/sessions
type Manager interface {
    Ensure(ctx context.Context, userID string) (*Session, error) // get running, or create
    Get(ctx context.Context, id string) (*Session, error)
    Stop(ctx context.Context, id string) error
    Touch(ctx context.Context, id string)                       // activity for the reaper
    Reap(ctx context.Context) error                             // idle stop, retention
}

type Backend interface {                                        // dockerBackend, staticBackend
    Create(ctx context.Context, spec Spec) (Handle, error)
    Start(ctx context.Context, h Handle) error
    Stop(ctx context.Context, h Handle, grace time.Duration) error
    Remove(ctx context.Context, h Handle, keepVolume bool) error
    Inspect(ctx context.Context, h Handle) (Status, error)
    List(ctx context.Context) ([]Handle, error)                 // by label, for registry rebuild
    Endpoint(h Handle) *url.URL                                 // http://pg-session-<id>:7000
}

type Spec struct {
    ID, UserID, Token string
    Image, RemoteImage string
    Limits Limits                       // CPUs, Memory, Pids
    Network string
    Env map[string]string
}

// internal/gateway/store
type Store interface {
    Users() UserRepo
    Sessions() SessionRepo
    Progress() ProgressRepo
    Stuck() StuckRepo
    Invites() InviteRepo
    Audit(ctx context.Context, e AuditEvent) error
}

// internal/gateway/auth
type Authenticator interface {
    Middleware(next http.Handler) http.Handler        // sets the user on the context or 401s
    Routes(mux *http.ServeMux)                        // login, callback, logout
}
```

`staticBackend` (local mode) implements `Backend` with a single pre-existing session whose endpoint is `PG_SESSION_URL`; `Create` and `Remove` are no-ops. This keeps the request path identical in both modes.

The docker backend gives each session its own internal bridge network, `pg-net-<id>` with a `/28` from `sessions.subnetPool`, created before the container and removed with it; the gateway joins it under the alias `gateway` (`sessions.gatewayContainer`, default its hostname). After a gateway restart the registry rebuild joins the running sessions' networks again (a new gateway container after an upgrade is not on them yet), and the rebuild and the reaper remove networks whose session has no container (deployment.md §7). `sessions.isolation: shared` puts every session on `sessions.network` instead.

Every request the gateway sends to a session's sessiond (proxy, `Call`, `Status`) carries the session's proxy secret in `X-Playground-Proxy`: `hex(HMAC-SHA256(key, "playground session proxy\0" + id))`, with the key in `<dataDir>/session-proxy.secret`. The container gets the same value as `PG_PROXY_SECRET` (§3.13). The static session has none.

### 2.4 Session lifecycle

`Ensure` is idempotent per user and serialised per user with a keyed mutex. Steps in server mode: look up the registry; if a running container exists and is healthy, return it; if a stopped session exists, re-create the container on the same volume; otherwise create the volume, the remote box (if `PG_REMOTE_IMAGE` is set) and the session container, start it, poll `GET /healthz` up to 30 s, then mark `ready`. Failures mark the session `failed` with a reason and remove the container; the volume is kept.

The reaper runs every minute: sessions with no WebSocket for `PG_IDLE_STOP_MINUTES` move to `stopping` then `stopped` (container removed, volume kept); volumes unused for `PG_VOLUME_RETENTION_DAYS` are removed with an audit entry. WebSocket presence is tracked by the proxy (connections opened and closed per session).

On gateway start, `List` by label rebuilds the registry so that a gateway restart never orphans running sessions.

### 2.5 Proxying

One `httputil.ReverseProxy` per session, cached, with `Rewrite` setting the target, `X-Playground-User` and `X-Playground-Proxy` (the session's proxy secret, §2.3; any value from the browser is dropped) and stripping the cookie and `Authorization`. `ReverseProxy` handles the `101 Switching Protocols` upgrade for WebSockets since Go 1.12, so no custom tunnelling is needed. The proxy wraps the response writer to count open WebSockets per session for the reaper, and enforces a per-user limit of 6 concurrent WebSockets.

### 2.6 Authentication

- `none`: a single user `local`; the middleware injects it. Only allowed when `PG_MODE=local`.
- `invite`: `POST /api/v1/auth/invite {code}` checks the code (constant-time compare, use counting, expiry), creates a user with a random id and the code's label as display name, sets the cookie.
- `oidc`: `github.com/coreos/go-oidc/v3` with `golang.org/x/oauth2`; PKCE; the user id is the issuer plus `sub`; display name from `name` or `email`.
- Cookie `pg_session`: value `userID.expiry.signature` with HMAC-SHA256 over `PG_COOKIE_SECRET`; `HttpOnly; Secure (when public URL is https); SameSite=Lax; Path=/`; 30 days, renewed on use.
- CSRF: every state-changing request must carry an `Origin` header equal to `PG_PUBLIC_URL`'s origin; WebSocket upgrades are checked the same way. No tokens needed.

### 2.7 Store

SQLite through `database/sql` with `modernc.org/sqlite` (no cgo). One writer connection, WAL mode, `busy_timeout` 5 s. Migrations are embedded SQL files applied at start with a `schema_migrations` table.

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL,            -- local | invite | oidc
  subject TEXT UNIQUE, display_name TEXT,
  created_at INTEGER NOT NULL, last_seen_at INTEGER);
CREATE TABLE invites (
  code_hash TEXT PRIMARY KEY, label TEXT, max_uses INTEGER, uses INTEGER NOT NULL DEFAULT 0, expires_at INTEGER);
CREATE TABLE sessions (
  id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id),
  state TEXT NOT NULL, container_id TEXT, remote_container_id TEXT, volume TEXT, image TEXT,
  token_hash TEXT NOT NULL, created_at INTEGER NOT NULL, last_active_at INTEGER, stopped_at INTEGER, failure TEXT);
CREATE TABLE progress (
  user_id TEXT NOT NULL, lesson_id TEXT NOT NULL, step_id TEXT NOT NULL,
  status TEXT NOT NULL,                                -- in-progress | done
  hints_used INTEGER NOT NULL DEFAULT 0, resets INTEGER NOT NULL DEFAULT 0,
  completed_at INTEGER, updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, lesson_id, step_id));
CREATE TABLE stuck_reports (
  id INTEGER PRIMARY KEY, user_id TEXT NOT NULL, lesson_id TEXT, step_id TEXT,
  created_at INTEGER NOT NULL, note TEXT, events_json TEXT NOT NULL);
CREATE TABLE audit (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, user_id TEXT, action TEXT NOT NULL, details TEXT);
```

Local mode uses `jsonStore`: the same interface over `PG_DATA_DIR/local.json`, written atomically on change. It exists so that local installs have no database file to think about and so that the progress file is human-readable and easy to copy.

### 2.8 Curriculum serving

At start the gateway loads `PG_CURRICULUM_DIR/curriculum.json` (the index produced by `playctl curriculum build`) and validates its version against its own. Lesson bundles (`lesson.md` text, scenario JSON, assets) and fixture tarballs (`fixtures/<lesson>.tar.zst`) are served as static files with ETags. sessiond fetches fixtures through `/internal/curriculum/fixtures/{lesson}` with its session token.

### 2.9 Progress

Progress is written by sessiond through `POST /internal/sessions/{id}/progress` when a step completes, is demonstrated or skipped, when hints are shown (the browser calls `/rpc/hint` so sessiond can count), and on reset. A step's stored status only rises (`not-started` < `in-progress` < `demonstrated` < `skipped` < `done`); `GET /api/v1/progress` adds per-lesson counts, and a lesson is done only when every step of the bundle's scenario is done (api.md §2.4). `POST /api/v1/progress/reset` with no body clears every lesson of the user ("Start over") and sends sessiond `drop-step {}`. The browser reads `GET /api/v1/progress`. Writing from sessiond rather than the browser keeps progress correct when a tab is closed mid-step. While a curriculum bundle is loaded, reports for lessons or steps it does not have are refused with 400 (the learner can call `/internal` with their own session's token, so the ids are not trusted).

## 3. sessiond

### 3.1 Responsibilities

Keep Emacs, tmux and Xvnc alive; expose the PTY, events, VNC and RPC endpoints; tap input for instant prefix feedback; evaluate the active step; record and restore checkpoints; serve preview files and watch for changes; load scenarios; report progress. It runs as the unprivileged `learner` user; `tini` is PID 1.

### 3.2 Startup sequence

1. Parse env; create `/run/playground`; open the agent Unix socket.
2. Initialise the workspace if empty: `git init`, copy the pristine config from `/opt/playground/emacs.d` to `/work/emacs.d`, write `modules.el` with `base agent`, write `~/.ssh/config` for the remote box.
3. Start the Emacs daemon; wait for the agent's `hello` (timeout 60 s, then restart once and report).
4. Start the tmux server and the terminal frame.
5. Start the HTTP server on `PG_LISTEN`; `healthz` turns green only after step 3.
6. Load the last active scenario from `/work/.playground/state.json` if any; emit `hello` on new event connections.

### 3.3 Supervisor

A small process tree manager with per-child restart policies:

| Child | Command | On exit |
|---|---|---|
| daemon | `emacs --fg-daemon=playground` | restart with backoff 1, 2, 4, 8 s, max 5 per 10 min; then `emacs` event `unresponsive` and give up until `/rpc/restart-emacs` |
| tmux server | `tmux -L pg new-session -d -s emacs -x 120 -y 36` | restart immediately |
| terminal frame | `tmux send-keys`-free: the session's initial command is `emacsclient -t -s playground`; tmux `remain-on-exit on` (respawn-pane needs the dead pane) and sessiond's session-level `pane-died` hook re-runs `tmux respawn-pane` | respawn within 500 ms; emit `emacs {event: frame-closed}` then `frame-opened` |
| Xvnc | `Xvnc :1 -geometry 1600x900 -depth 24 -rfbport 5901 -localhost -SecurityTypes None -AcceptSetDesktopSize=1`; the geometry is only the initial size: the browser asks for its pane's size (noVNC `resizeSession`) and Xvnc resizes through RandR | restart if GUI mode is active |
| GUI frame | `emacsclient -c -s playground --frame-parameters='((width . W) (height . H))'` with `DISPLAY=:1` (no `fullscreen`: without a window manager it does nothing; the agent sizes GUI frames to their display, also after a RandR resize) | re-run while GUI mode is active |

Leaving GUI mode deletes the X frames inside Emacs and closes its X display connection (`x-close-connection`, safe in the image's Lucid build) before Xvnc stops; if Emacs cannot let go of the display, Xvnc is left running (the next switch to GUI reuses it) rather than stopped under Emacs, which would print "Connection lost to X server" in the terminal frame.

Health of the daemon is the agent heartbeat (`ping` every 2 s). Five missed heartbeats raise `emacs {event: unresponsive}`; Rescue then sends `C-g` through tmux and, if still unresponsive after 10 s, offers a daemon restart.

### 3.4 PTY

`/pty` runs `tmux -L pg attach-session -t emacs` on a pty from `github.com/creack/pty`. Bytes flow both ways; a text frame `{"t":"resize","cols":120,"rows":36}` calls `pty.Setsize`. A new `/pty` connection detaches the previous one (`attach -d`) so sizes never fight; the previous client receives a close reason `replaced`. Output bytes are also fed to the input tap's mode tracker (to detect the kitty keyboard protocol being enabled or disabled by Emacs). Input bytes are fed to the tap's key decoder before being written to the pty.

### 3.5 Agent connection and RPC

The agent connects to the Unix socket; messages are newline-delimited JSON (api.md §7). sessiond keeps at most one agent connection; a new one replaces the old (daemon restart). RPC calls are correlated by id with per-call timeouts (default 2 s; `eval` accepts up to 10 s). Calls are serialised through one goroutine so Emacs never receives two concurrently. `eval` results are JSON produced by Emacs's `json-serialize`.

### 3.6 Input tap

Decodes what the learner typed into Emacs key names, independently of Emacs, to drive `prefix` events.

Terminal decoder: a small state machine over input bytes. Legacy encoding: `0x01..0x1a` → `C-a..C-z`, `0x1b` followed by a byte within 50 ms → `M-<key>`, `0x1b [` → CSI sequence (arrows, function keys, `CSI u` codes), `0x7f` → `DEL`, `0x0d` → `RET`, `0x09` → `TAB`, `0x20` → `SPC`. Kitty encoding (when the output tracker has seen Emacs push kitty flags with `CSI > n u`): `CSI code ; modifiers u` → key with modifiers. The decoder emits key names using Emacs conventions (`C-x`, `M-x`, `C-M-x`, `<f3>`, `<insert>`).

RFB decoder: client messages of type 4 (KeyEvent: down flag and X keysym) on the `/vnc` bridge; modifier state tracked from `Control_L/R`, `Alt_L/R`, `Shift_L/R`; keysyms mapped through a generated table.

Prefix detection: the tap knows the prefix keys from a static list (`C-x`, `C-c`, `C-h`, `M-g`, `M-s`, `M-o`, `C-x 4`, `C-x 5`, `C-x t`, `C-x r`, `C-x n`, `C-x p`, `C-x 8`, `ESC`) and emits `prefix` when a sequence so far is a prefix; the next agent `key` event (or a `C-g`) clears it. The frontend treats `prefix` as advisory and `key` as authoritative.

A lone `ESC` is reported only when the 50 ms meta timeout ends (by a timer, or as stale when the next input arrives), while tmux (`escape-time 0`) hands the byte to Emacs at once. The tap remembers when the agent last reported a command; a key reported late whose bytes arrived before that moment was already used by Emacs (the third `ESC` of `ESC ESC ESC`, which runs `keyboard-escape-quit`), so it is dropped and the pending prefix is cleared instead of emitting `prefix ESC` after the command that consumed it.

### 3.7 Event bus

`events.Bus` fans out `protocol.Event` values to subscribers. Each event gets a monotonic `seq`. A ring buffer of 500 events supports `?after=<seq>` replay on reconnect. Subscribers have a 256-entry buffer; a slow subscriber gets a `gap` event and the buffer is reset rather than blocking producers. Producers: the agent reader, the input tap, the supervisor, the file watcher, the step engine.

### 3.8 Step engine

```go
type Engine struct {
    scenario  *protocol.Scenario
    step      int
    startedAt time.Time
    counts    map[string]int        // command name → runs since step start
    results   []protocol.CondResult
}
```

Triggers: agent `key` and `state` events, `file` events, a 2 s ticker. Evaluation runs in one goroutine, debounced on the trailing edge: 40 ms after the last trigger of a burst (a key event and the state the agent sends a few milliseconds later), and at most 120 ms after the burst's first trigger, so a stream of events cannot postpone it. A keystroke's check therefore arrives about 50 ms after its key event. Condition kinds:

| Kind | Evaluated by | Notes |
|---|---|---|
| `event` | counts | `command`, optional `min` |
| `elisp` | agent `check` call, batched | form must return non-nil; 2 s timeout → `unknown` |
| `file` | sessiond | `exists`, `contains`, `not_contains`, `regex`, `json_path`; paths relative to `/work`, read through `os.Root` |
| `shell` | sessiond | `sh -c` in the workspace, 5 s timeout, exit 0 is true; stdout captured for the `check` event (truncated at 4 KB) |
| `time` | sessiond | only inside `stuck` rules: `minibuffer_seconds`, `idle_seconds`, `step_seconds` |

Combinators `all`, `any`, `not`. Results are emitted as `check`. A step completes when every condition is true; the engine emits `step {status: complete}`, reports progress, takes the next checkpoint and advances. Stuck rules are evaluated on the ticker and emit `nudge` events with ids the browser can dismiss.

The step lifecycle is api.md §4.1. Besides completing, a step can be:

- **demonstrated**: `rpc demo {stepId, phase: start}` (the browser's "Watch it") first restores the step's checkpoint when the engine counted any command since the step started or was last reset (files and windows as `restore {}`, a `step reset` event, no reset counted), then sets the step's demo latch and a playing flag. While the latch is set, a completion is recorded as `demonstrated` once (event and progress), the step stays active, nothing advances and no checkpoint is taken. `phase: end` clears the playing flag only; `restore {}`, a reload of the step and `drop-step` clear the latch, so the learner's own completion afterwards is `done`. Stuck rules wait while a demo plays.
- **skipped**: `rpc skip {stepId}` restores the checkpoint when commands ran, holds completions, plays the step's `demo` into the learner's frame with playctl's pacing (`protocol.PlayDemo`: 170 ms per chord, 70 ms per character), waits up to 8 s for the conditions (`demoCompleted`; false is logged as a curriculum defect), records `skipped`, emits `step {status: skipped}` and advances like a completion (next checkpoint, `loaded`). The call is bounded by 60 s and finishes even when the browser goes away.
- **dropped**: `drop-step {lessonId}` forgets that lesson's saved progress, `drop-step {}` all of it ("Start over"); a forgotten active step emits `step {status: dropped}`.

Resuming a lesson (`scenario` without `stepId`) starts at the first step that is neither done nor skipped; a demonstrated step is resumed so the learner practises it. The engine's saved state (`/work/.playground/state.json`) keeps done, skipped and demonstrated times per step.

### 3.9 Checkpoints

The workspace is a git repository whose `.playground/` directory is ignored. Checkpoints never touch the learner's index or branch:

```
export GIT_INDEX_FILE=/work/.playground/index.tmp
git add -A -- . ':!.playground'
tree=$(git write-tree)
commit=$(git commit-tree "$tree" -m "checkpoint <lesson>/<step>" ${parent:+-p $parent})
git update-ref refs/playground/<lesson>/<step> "$commit"
```

The buffer and window spec (visible files, point per buffer, layout tree) is captured from the agent and saved as `/work/.playground/checkpoints/<lesson>/<step>.json`, together with what the commit of the files on disk cannot hold: the text of file buffers with unsaved changes and each file buffer's undo history (`buffer-undo-list` reduced to entries that print readably), within 256 KiB of text and 128 KiB of history per buffer and 512 KiB in all. A step often starts with the previous step's unsaved edit (auto-save waits five seconds) and relies on its undo history (lesson 1.3 undoes a deletion within a region), so after the agent's `reset-step` a restore puts that text back with `replace-buffer-contents` and the recorded history, with undo recording off. The checkpoint also keeps the Emacs session variables steps rely on (`sessiond.GlobalVars`: the kill ring, the minibuffer, file, buffer, search and replace histories, the keyboard macro, its counter and the counter's initial value), printed readably within 256 KiB, and a restore sets them back: otherwise a "Watch it" followed by "Do it yourself" leaves the demo's kills, commands and macro count in them (1.2's `M-y`, 2.2's `M-p`, 1.6's counter). Restore runs `git restore --source=<ref> --worktree --staged -- .` followed by `git clean -fd -e target -e _build -e .venv -e node_modules -e .playground`, then the agent's `reset-step` with the spec. Build directories are kept so a reset does not cost a rebuild. The learner's staging area is reset to the checkpoint; the lesson text says so where it matters (Magit lessons take their own checkpoints after staging).

### 3.10 Preview and file watching

Files are served with `http.FileServerFS(root.FS())` where `root` is `os.OpenRoot("/work")`, which forbids escaping the workspace through `..` or symlinks. Headers: `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox allow-scripts allow-same-origin` for HTML so a learner's page cannot reach the app's cookies (the preview is served from the session path, not a separate origin, so the sandbox matters). `fsnotify` watches `/work` recursively, re-adding directories as they appear, ignoring `.git`, `target`, `_build`, `.venv`, `node_modules`, `.playground`; events are coalesced per path at 200 ms.

### 3.11 VNC bridge

`/vnc` copies bytes between the WebSocket and `127.0.0.1:5901` with `io.Copy` in both directions; client-to-server bytes pass through the RFB key decoder. Xvnc is started with `-SecurityTypes None` and bound to localhost; the only path to it is through sessiond, which the gateway authenticates.

### 3.12 Scenario loader

`/rpc/scenario {lesson, step}`: fetch scenario JSON and the fixture tarball from the gateway (cached by ETag under `/work/.playground/cache`), ensure modules (write `modules.el`, restart the daemon with buffer restore if changed), extract fixtures into `/work/<root>` (skip existing files unless `setup.reset`), call the agent for `setup.open`, `setup.layout`, `setup.eval`, take the checkpoint, set the engine's active step, emit `step {status: loaded}`.

For a step after the first ("Go to this step", "Do this step again", resuming mid-lesson) the step must start from the state its demo was written for, not from the lesson's initial state. After the setup the loader restores that step's checkpoint when it exists (files and windows, without retaking it). Otherwise it fast-forwards: the engine rehearses each earlier step (`Engine.Rehearse`: conditions evaluated, nothing recorded, emitted or persisted), the step's demo is played into Emacs at playctl pacing and the loader waits up to 10 s for its conditions; then it takes the target step's checkpoint and activates it. The replay is bounded by 3 minutes and survives the browser going away. A later step's checkpoint is never overwritten with the lesson's initial state.

Restoring a checkpoint (step reset, demo start, skip, "Go to this step") keeps `emacs.d/modules.el`: modules only grow (emacs.md §4), and a restored older set would disagree with the running Emacs.

### 3.13 Configuration

Flags mirror the `PG_*` environment variables (deployment.md §3). `sessiond healthcheck` is the Compose health command: it returns 0 when the HTTP server is up and the agent has said hello.

With `PG_PROXY_SECRET` set (server mode), a middleware in front of every route compares `X-Playground-Proxy` with it in constant time and answers 401 otherwise; only `GET`/`HEAD /healthz` is open (the gateway's start-up probe and the health check). The header is removed before the handlers run, and Emacs, tmux and shell conditions get the environment without `PG_PROXY_SECRET` and `PG_SESSION_TOKEN`. Without it (local mode, `make dev`) every request is served, as only the learner's own gateway and host reach the session.

## 4. playctl

| Command | Purpose |
|---|---|
| `playctl curriculum validate [dir]` | schema-check every `scenario.yaml`, lesson front matter, table files; check fixture paths and key names; exit non-zero on errors |
| `playctl curriculum build [dir] -o out/` | produce `curriculum.json`, lesson bundles and fixture tarballs |
| `playctl curriculum play <sel>... -session URL [-watch] [-repeat N] [-reset-progress]` | replay lessons' demos against a running session and require each step to complete; `-watch` first plays each step as a "Watch it" demonstration (`demo start`, demo, `demo end`, expect `step demonstrated` with the step still active, then `restore {}`); `-repeat N` plays every lesson N times; with a gateway session URL each played lesson's progress is reset afterwards |
| `playctl images build [session|gateway|remote-box]` | `docker buildx bake` wrapper with cache settings |
| `playctl dev` | run the gateway from source against the Compose session, with the Vite dev server proxied |
| `playctl session shell` | `tmux attach` into the local session |
| `playctl export-user <id>` | tarball of workspace and progress (server) |
| `playctl protocol typescript -o web/src/protocol.gen.ts` | generate TypeScript types from `internal/protocol` through JSON Schema |
| `playctl config schema`, `playctl config check <file>` | emit the configuration JSON Schema; validate a configuration file |

## 5. Cross-cutting

- Configuration: `internal/config` loads defaults, then the JSON file (`PG_CONFIG` or `/etc/playground/config.json`), then environment overrides (`PG_` plus the upper-cased path), then flags; validates the result (`playctl config check`) and logs it with secrets redacted. The struct is the source of `deploy/config.schema.json`. See deployment.md §3.
- Logging: `log/slog` JSON to stdout with `request_id`, `session_id`, `user_id` where known. Never log cookie values, tokens or workspace file contents.
- Metrics (optional): Prometheus counters and histograms: sessions by state, session create duration, WebSocket connections, events per second, RPC durations, step evaluations, checkpoint durations, proxy latency.
- Errors: `httpx.Error{Status int; Code string; Message string}`; handlers return errors, a wrapper renders `application/problem+json`. Codes are stable strings listed in api.md §1.
- Timeouts: every subprocess runs with a context; every RPC has a deadline; HTTP server read header timeout 10 s, idle 120 s; WebSockets ping every 20 s.
- Shutdown: on SIGTERM sessiond emits `emacs {event: stopping}`, asks the agent to snapshot the buffer spec, lets `auto-save-visited-mode` flush, sends SIGTERM to Emacs, waits up to 15 s, then exits. The gateway drains proxies for 10 s.
- Resource hygiene: subprocess output is bounded; event buffers are bounded; file reads for checks are capped at 1 MB.

## 6. Testing

- Unit: table tests for the input tap decoders (legacy, kitty, RFB), the step engine (conditions, combinators, debounce, stuck rules), checkpoint command building, cookie signing, origin checks, the JSON store.
- Agent integration: a Go test starts `emacs --batch -l playground-agent.el` against a test socket server and asserts the event stream for scripted commands; runs in CI inside the session image.
- Service integration: `testcontainers-go` starts the session image, exercises `/healthz`, `/pty` (sends keys, expects screen bytes), `/events`, a scenario load, a check and a restore.
- End to end: Playwright against `docker compose` in local mode (frontend.md §14).
- Load: a small Go program opening N sessions on a test host to validate `PG_MAX_SESSIONS` and memory budgets.

## 7. Conventions

`gofmt`, `go vet`, `staticcheck`, `govulncheck` in CI. Context first argument everywhere. No global state except the slog default. Errors wrapped with `%w` and inspected with `errors.Is/As`. Public surface of each package documented in its `doc.go`. Generated files carry a header and are regenerated in CI; a diff fails the build.
