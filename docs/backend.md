# Backend (Go)

Two services and one CLI in a single Go module. The gateway fronts everything; sessiond runs inside each session container; playctl is the developer and CI tool. The wire contracts they implement are in api.md; each package's `doc.go` carries the detail this document summarises.

## 1. Module layout

```
go.mod                               module github.com/floatshadow/emacs-playground, go 1.26
cmd/gateway, cmd/sessiond, cmd/playctl   flag parsing and wiring only
internal/
  protocol/      events, state, RPC, agent, scenario, curriculum and config types; the single source of truth
    tsgen/       TypeScript generator (api.md §8)
  config/        defaults < JSON file < PG_* environment < flags; validation; sessiond's env config
  httpx/         problem+json errors, JSON helpers, middleware, WebSocket helpers
  curriculum/    scenario and lesson loader, validator and bundle builder (gateway and playctl)
  docker/        thin wrapper over the Engine client; container hardening
  buildinfo/, deps/
  gateway/
    server.go    mux, middleware, API handlers, /internal; static.go: web bundle and fonts
    auth/        auth modes, cookies, invite codes
    sessions/    Manager (Service), Backend (static, docker), registry, reaper, proxy secrets
    proxy/       per-session reverse proxy
    store/       SQLite (server) and JSON file (local) implementations of Store
    curriculum/  bundle serving with ETags
    progress/    progress aggregation, stuck reports
    limit/       rate limiters
  sessiond/
    server.go    mux, startup, shutdown; rpc.go: RPC dispatch; unsaved.go: buffer and session-variable capture
    supervisor/  Emacs daemon, tmux, frames, Xvnc, heartbeat, restarts
    pty/         tmux attach on a pty, single viewer
    agent/       Unix socket listener, JSON lines codec, call correlation
    inputtap/    terminal and RFB key decoders, prefix tracker
    events/      bus, ring buffer, /events handler, event log
    steps/       step engine: conditions, stuck rules, demo latch, skip
    checkpoint/  git-based checkpoints and restore
    preview/     workspace file server and fsnotify watcher
    vnc/         WebSocket ↔ Xvnc bridge with key tap
    scenario/    fixture fetch and extraction, modules, setup, "Go to this step"
    gwclient/    client for the gateway's /internal API
web/embed.go     embeds the web bundle into the gateway binary (build tag embedweb)
```

Rules: `cmd/*` only wires; `internal/protocol` depends only on the standard library; packages under `gateway` and `sessiond` never import each other (both may use `protocol`, `config`, `httpx` and `curriculum`); everything that talks to a subprocess or the network sits behind an interface so it can be faked in tests.

## 2. Gateway

### 2.1 Responsibilities

- Serve the web bundle, the fonts and the curriculum bundle.
- Authenticate learners (none, invite code, OIDC) and issue session cookies.
- Own the session registry: one session per learner; create and stop containers in server mode, point at the fixed session in local mode.
- Proxy `/s/{id}/…` to the session's sessiond, WebSockets included.
- Store progress, stuck reports and an audit log.
- Housekeeping: stop idle sessions, remove old volumes, rebuild the registry from container labels after a restart.

### 2.2 HTTP surface

| Prefix | Handler | Notes |
|---|---|---|
| `/` | static web bundle | SPA fallback to `index.html`, security headers |
| `/fonts/` | web fonts | |
| `/api/v1/*` | JSON API | api.md §2 |
| `/s/{id}/*` | reverse proxy to sessiond | authenticated; the session must belong to the user |
| `/internal/*` | sessiond → gateway | bearer token per session (api.md §2.6) |
| `/healthz`, `/metrics` | operations | metrics on an optional separate listener |

`net/http.ServeMux` with method patterns. Middleware is a plain `func(http.Handler) http.Handler` chain: request id, logging (successful polling requests at debug level), recover, Origin check for state-changing requests and WebSocket upgrades, auth per route group.

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

type Backend interface {                                        // DockerBackend, StaticBackend
    Create(ctx context.Context, spec Spec) (Handle, error)
    Start(ctx context.Context, h Handle) error
    Stop(ctx context.Context, h Handle, grace time.Duration) error
    Remove(ctx context.Context, h Handle, keepVolume bool) error
    Inspect(ctx context.Context, h Handle) (Status, error)
    List(ctx context.Context) ([]Handle, error)                 // by label, for registry rebuild
    Endpoint(h Handle) *url.URL                                 // http://pg-session-<id>:7000
}

type Networker interface {                                      // DockerBackend with per-session networks
    Attach(ctx context.Context, h Handle) error                 // join a running session's network again
    Prune(ctx context.Context, keep func(id string) bool) error // remove networks of vanished sessions
}

type Spec struct {
    ID, UserID, Token  string
    ProxySecret        string           // PG_PROXY_SECRET
    Image, RemoteImage string
    Limits             Limits           // CPUs, Memory, Pids
    Network, Runtime   string
    Volume             string           // workspace volume at /work
    Env                map[string]string
}

// internal/gateway/store
type Store interface {
    Users() UserRepo; Sessions() SessionRepo; Progress() ProgressRepo
    Stuck() StuckRepo; Invites() InviteRepo
    Audit(ctx context.Context, e AuditEvent) error
    AuditLog(ctx context.Context, limit int) ([]AuditEvent, error)
    Close() error
}

// internal/gateway/auth
type Authenticator interface {
    Middleware(next http.Handler) http.Handler   // sets the user on the context or 401s
    Routes(mux *http.ServeMux)                   // login, callback, logout
    Mode() protocol.AuthMode
}
```

`StaticBackend` (local mode) has a single pre-existing session at `local.sessionUrl`, shared by every user of the install; `Create`, `Start`, `Stop` and `Remove` are no-ops and `Inspect` probes `/healthz`, so the request path is the same in both modes. Its id and token are `local.sessionId` and `local.sessionToken` (default `local`, matching the Compose file).

`DockerBackend` (server mode, experimental in slice 1) runs one container per learner, `pg-session-<id>`, labelled `playground.session`, `playground.user`, `playground.version` and `playground.role`, with the workspace volume `pg-ws-<user>` at `/work` and the hardening of deployment.md §9.2 (`docker.Harden`; no `nproc` ulimit, because `RLIMIT_NPROC` counts per uid across the host and every session is uid 1000; `pids_limit` bounds each container). Session tokens are 256-bit random values; only their SHA-256 is stored.

Network isolation (`sessions.isolation`): with `network` (the default) each session gets its own internal bridge network `pg-net-<id>`, a `/28` from `sessions.subnetPool` (default `10.231.0.0/16`), created before the container and removed with it; the gateway container (`sessions.gatewayContainer`, default its hostname, which Docker sets to the container id) joins it under the alias `gateway`. The registry rebuild re-attaches the gateway to running sessions' networks (a new gateway container after an upgrade is not on them), and the rebuild and the reaper prune networks whose session has no container. With `shared` every session joins `sessions.network`.

Proxy secret: everything the gateway sends to a session's sessiond (proxy, `Service.Call`, `Service.Status`) carries `X-Playground-Proxy: hex(HMAC-SHA256(key, "playground session proxy\0" + id))`, with the key in `<dataDir>/session-proxy.secret`. The value is stable across gateway restarts and re-creations of the session, and the container gets it as `PG_PROXY_SECRET` (§3.13). The static session has none.

### 2.4 Session lifecycle

`Ensure` is idempotent and serialised per user with a keyed mutex: a running healthy session is returned; a stopped or failed one is re-created on the same volume with a fresh token; otherwise the volume, the remote box (when enabled) and the session container are created and started, and `/healthz` is polled for up to 30 s before the session is `ready`. Failures mark the session `failed` with a reason and remove the container; the volume is kept. `sessions.maxSessions` bounds running sessions.

Presence: the proxy reports WebSocket opens and closes per session; sessiond's 30 s heartbeat reports its own count. The reaper runs every minute: sessions without WebSockets turn `idle`, and after `sessions.idleStopMinutes` are stopped (container removed, volume kept); volumes of sessions stopped longer than `sessions.volumeRetentionDays` are removed with an audit entry. At start, `List` by label rebuilds the registry, so a gateway restart never orphans running sessions.

### 2.5 Proxying

One `httputil.ReverseProxy` per session, cached. `Rewrite` sets the target, strips the `/s/{id}` prefix, sets `X-Playground-User`, the request id and `X-Playground-Proxy` (any value from the browser is dropped), and removes `Cookie` and `Authorization`; `Set-Cookie` from sessiond is dropped. `ReverseProxy` handles the `101 Switching Protocols` upgrade, so no custom tunnelling is needed. The handler counts open WebSockets per session for the reaper and enforces 6 concurrent WebSockets per user. Only the owner gets through (anyone for the shared local session); sessions that are not `ready` or `idle` answer `503 session_unavailable`. `rpc eval` is refused unless `features.authorMode` is on, and `rpc drop-step` always.

### 2.6 Authentication

- `none`: a single user `local`, injected by the middleware; local mode only.
- `invite`: `POST /api/v1/auth/invite {code}` hashes the code (SHA-256; only hashes are stored), redeems it with use counting and expiry (constant-time comparison), creates a user with a random id and the code's label as display name, and sets the cookie. Rate limited per client IP and per code. The invites file (`deploy/invites.example.txt`: `<code> [label] [uses=N] [until=YYYY-MM-DD]`) is synced into the store at start and on SIGHUP.
- `oidc`: cookies are verified as for invites; the login routes answer `501 not_implemented` in slice 1. The design: `github.com/coreos/go-oidc/v3` with `golang.org/x/oauth2`, PKCE, user id issuer plus `sub`, display name from `name` or `email`, `allowedEmailDomains`.
- Cookie `pg_session`: `<base64url user id>.<expiry unix seconds>.<base64url HMAC-SHA256>` keyed by `auth.cookieSecretFile` (default `<dataDir>/cookie.secret`, generated on first start); `HttpOnly`, `SameSite=Lax`, `Secure` when the public URL is https, `Path=/`; `auth.sessionDays` long and renewed on use at most daily.
- CSRF: every state-changing request and WebSocket upgrade must carry an `Origin` equal to `publicUrl`'s origin (in local mode `localhost` and `127.0.0.1` are interchangeable). No tokens.

### 2.7 Store

Server mode: SQLite at `<dataDir>/playground.db` through `database/sql` with `modernc.org/sqlite` (no cgo). One writer connection, WAL, `busy_timeout` 5 s. Migrations are embedded SQL files (`store/migrations/`) applied at start with a `schema_migrations` table.

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
  status TEXT NOT NULL,                                -- in-progress | demonstrated | skipped | done
  hints_used INTEGER NOT NULL DEFAULT 0, resets INTEGER NOT NULL DEFAULT 0,
  completed_at INTEGER, updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, lesson_id, step_id));
CREATE TABLE stuck_reports (
  id INTEGER PRIMARY KEY, user_id TEXT NOT NULL, lesson_id TEXT, step_id TEXT,
  created_at INTEGER NOT NULL, note TEXT, events_json TEXT NOT NULL);
CREATE TABLE audit (
  id INTEGER PRIMARY KEY, at INTEGER NOT NULL, user_id TEXT, action TEXT NOT NULL, details TEXT);
```

Local mode uses the same interface over `<dataDir>/local.json`, rewritten atomically on change (it keeps the newest 50 stuck reports), so a local install has no database file and its progress is human-readable and easy to copy.

### 2.8 Curriculum serving

The gateway serves the bundle built by `playctl curriculum build` from `curriculum.dir` (layout in `protocol/curriculum.go`): the index `curriculum[.<locale>].json`, lesson bundles `lessons/<id>/bundle[.<locale>].json`, assets, scenarios `lessons/<id>/scenario.json` and fixture tarballs `fixtures/<id>.tar.zst`. Files are opened through `os.Root` and served with strong ETags (SHA-256 of the content), so `If-None-Match` answers 304. A bundle whose format differs from `protocol.CurriculumFormat` stops the gateway; a missing bundle only logs a warning and the curriculum endpoints answer 503 until it appears (SIGHUP, or automatically in author mode when `curriculum.json` changes).

### 2.9 Progress

sessiond writes progress through `POST /internal/sessions/{id}/progress` when a step starts, completes, is demonstrated or skipped, when hints are shown (the browser calls `rpc hint` so sessiond can count them) and on resets. Writing from sessiond rather than the browser keeps progress correct when a tab closes mid-step. `progress.Service.Apply` keeps a step's status rising (api.md §2.4), takes `hintsUsed` and `resets` as absolute totals, and, while a bundle is loaded, refuses reports for lessons or steps it does not have with 400: the learner can call `/internal` with their own session's token, so ids are not trusted. `GET /api/v1/progress` aggregates per lesson against the bundle's scenarios. `POST /api/v1/progress/reset` clears one lesson or all of them and sends the user's running session `drop-step` (best effort, 5 s).

## 3. sessiond

### 3.1 Responsibilities

Keep Emacs, tmux and Xvnc alive; expose the PTY, events, VNC and RPC endpoints; tap input for instant prefix feedback; evaluate the active step; record and restore checkpoints; serve preview files and watch the workspace; load scenarios; report progress. It runs as the unprivileged `learner` user under `tini`. `sessiond -fake` substitutes bash for Emacs, tmux and Xvnc and runs an in-process fake agent, so the whole daemon runs on hosts without them.

### 3.2 Startup sequence

1. Parse the environment; create the run directory; open the agent Unix socket.
2. Initialise the workspace if needed: `git init`, `.playground/`, the pristine config copied from `/opt/playground/emacs.d` to `/work/emacs.d` (first start only), `modules.el` with `base agent`.
3. Start the HTTP server on `PG_LISTEN`. `/healthz` answers 503 "Emacs is starting" until step 4 completes, so the gateway and Compose can poll from the first second.
4. Start the Emacs daemon and wait for the agent's `hello` (60 s, then restart once and report).
5. Start the tmux server and the terminal frame.
6. Resume the step saved in `/work/.playground/state.json`, if any: no fixtures and no checkpoint, but the fresh Emacs gets the lesson back before the step is active. The last snapshot (`snapshot.json`) is re-opened with the agent's `open` when it shows a file under the lesson root, else the lesson's setup runs without `reset`. Until the resume ends, periodic and shutdown snapshots do not overwrite `snapshot.json` (`Supervisor.HoldSnapshots`), so a *scratch* snapshot of the fresh Emacs never replaces the learner's.

### 3.3 Supervisor

Per-child restart policies:

| Child | Command | On exit |
|---|---|---|
| daemon | `emacs --fg-daemon=playground --init-directory=/work/emacs.d` | restart with backoff 1, 2, 4, 8 s; after 5 restarts in 10 min give up (`/healthz` 503) until `rpc restart-emacs` |
| tmux server | `tmux -L pg new-session -d -s emacs -x 120 -y 36 '<frame command>'` | restarted after 500 ms |
| terminal frame | the pane command `emacsclient -t -s playground` | respawned with `tmux respawn-pane`, after a delay that grows when the pane keeps dying at once; `emacs frame-closed`, then `frame-opened` |
| Xvnc | `Xvnc :1 -geometry 1600x900 -depth 24 -rfbport 5901 -localhost -SecurityTypes None -dpi 110 -AlwaysShared -AcceptSetDesktopSize=1 -desktop playground` | restarted while GUI mode is active |
| GUI frame | `emacsclient -c -s playground -F '((width . W) (height . H))'` with `DISPLAY=:1` | re-run while GUI mode is active |

tmux: sessiond sets `remain-on-exit on` and a session-level `pane-died` hook that signals its wait channel; the session-level hook overrides the global one in the image's `tmux.conf`, so only sessiond respawns the pane. The Xvnc geometry is only the initial size: the browser asks for its pane's size (noVNC `resizeSession`) and Xvnc resizes through RandR. The GUI frame has no `fullscreen` parameter (it does nothing without a window manager); the config fits GUI frames to the screen (emacs.md §6).

Leaving GUI mode closes the `/vnc` bridges with 4002, deletes the X frames inside Emacs and closes its X connections (`x-close-connection`, safe in the Lucid build) before Xvnc stops. If Emacs cannot let go of the display, Xvnc keeps running (the next switch to GUI reuses it) rather than dropping under Emacs, which would print "Connection lost to X server" in the terminal frame.

Heartbeat: the agent pings every 2 s. Five missed pings emit `emacs unresponsive` and send SIGUSR2 to the daemon (`debug-on-event`); resumed pings emit `responsive`. Rescue then sends C-g through tmux and, if Emacs is still busy 10 s later, emits an `error` with code `emacs_unavailable` offering a restart.

Restarts (`rpc restart-emacs`, module changes): emit `restarting`, snapshot the windows, save modified file buffers (`supervisor.FlushForm`: never over a file changed on disk, remote and read-only files skipped, whatever `auto-save-visited-mode` says), stop, start, wait for hello, re-open the snapshot, respawn the terminal frame, emit `daemon-restarted` once the new terminal frame is open. A snapshot is also taken every 30 s.

### 3.4 PTY

`/pty` runs `tmux -u -L pg attach-session -d -t emacs` on a pty (`github.com/creack/pty`). Bytes flow both ways; `resize` calls `pty.Setsize`. One viewer at a time: a new connection sends `replaced` to the previous one and closes it with 4000. Input bytes go through the input tap's terminal decoder before reaching the pty; output bytes through its mode tracker.

### 3.5 Agent connection and RPC

The agent connects to the Unix socket and speaks api.md §7. sessiond keeps one connection; a new one replaces the old (daemon restart). Calls are correlated by id and serialised through one goroutine, so Emacs never receives a second call before replying to the first; each call carries `timeoutMs` for the agent's `with-timeout`, and sessiond waits that long plus a small grace before reporting a timeout (default 2 s; `eval` up to 10 s).

### 3.6 Input tap

Decodes what the learner types into Emacs key names, independently of Emacs, to drive `prefix` events within milliseconds; the agent's `key` events stay authoritative.

- Terminal decoder: legacy encoding (`0x01..0x1a` → `C-a..C-z`, `0x1b` followed by a byte within 50 ms → `M-<key>`, `0x7f` `DEL`, `0x0d` `RET`, `0x09` `TAB`), CSI and SS3 sequences, xterm `modifyOtherKeys`, and `CSI code ; modifiers u`. Bracketed paste, mouse reports and terminal replies are skipped. The browser's terminal sends CSI u itself for chords the legacy encoding cannot carry (frontend.md §5), and tmux passes them on.
- Mode tracker: watches terminal output for the kitty keyboard protocol being pushed, set or popped and for `modifyOtherKeys`, so the decoder knows how ESC is encoded.
- RFB decoder: KeyEvent messages (type 4) and QEMU extended key events on `/vnc`; X keysyms mapped through a generated table, modifier state tracked from the modifier keysyms.
- Prefix tracker: the static prefix list `C-x`, `C-c`, `C-h`, `M-g`, `M-s`, `C-x 4`, `C-x 5`, `C-x t`, `C-x r`, `C-x n`, `C-x p`, `C-x 8`, `C-x w`, `C-x x`, `C-x v`, `C-x a`, `C-x C-k`, `C-x RET`, `M-s h`, `ESC` (`inputtap.DefaultPrefixes`; M-o is ace-window, not a prefix). It emits `prefix` while the sequence so far is a prefix, and `prefix` with `keys: ""` as soon as a key ends a pending prefix (C-g, or a key that completes a command or leaves the list): the agent's `key` event can come much later (`C-h k` reads its own key first). `C-h` after a pending prefix keeps it pending for one more key: which-key's paging dispatcher reads that key (`n`, `p`, `d` keep the prefix, `u` drops its last key, `a` ends it) and reads the prefix again; without which-key, `describe-prefix-bindings` runs and its `key` event resets the tracker.

Late keys: a lone `ESC` is reported only when the 50 ms meta timeout ends (by a timer, or when the next input arrives), while tmux hands the byte to Emacs after its `escape-time` of 10 ms (`supervisor.EscapeTime`). The tap remembers when the agent last reported a command; a key reported late whose bytes arrived before that moment was already used by Emacs (the third `ESC` of `ESC ESC ESC`), so it is dropped and the pending prefix cleared. Otherwise a silently pending `ESC` would turn the learner's next `C-x` into `C-M-x` in the tracker.

### 3.7 Event bus

`events.Bus` fans out `protocol.Event` values. Each event gets a monotonic `seq` and is encoded once; subscribers, the 500-event replay ring (`?after=<seq>`), the stuck-report window (`Bus.Recent`, 200 events) and the event log (deployment.md §9.4) share the encoded frame. Subscribers have a 256-entry buffer; a slow subscriber gets a `gap` event instead of blocking producers. Producers: the agent reader, the input tap, the supervisor, the file watcher, the step engine, the scenario loader.

### 3.8 Step engine

`steps.Engine` holds the active scenario and step, counts commands since the step (and the lesson) started, evaluates conditions, emits `check` and `step` events, reports progress, takes the next step's checkpoint when a step completes, and evaluates stuck rules.

Triggers: agent `key` and `state` events, `file` events, a 2 s ticker. Evaluation runs on one goroutine, debounced on the trailing edge: 40 ms after the last trigger of a burst (a key event and the state the agent sends a few milliseconds later), and at most 120 ms after the burst's first trigger, so a stream of events cannot postpone it (`DefaultDebounce`, `DefaultDebounceMax`). A keystroke's `check` arrives about 50 ms after its `key`.

| Kind | Evaluated by | Notes |
|---|---|---|
| `event` | command counts | `command`, `min` (default 1), `since` step or lesson |
| `elisp` | agent `check`, batched per evaluation | non-nil is true; 2 s timeout or error → unknown |
| `file` | sessiond through `os.Root` | `exists`, `contains`, `not_contains`, `regex`, `json_path` + `equals`; paths relative to `/work`; reads capped at 1 MB, fresh each time |
| `shell` | sessiond | `sh -c` as the learner in `cwd` (default the setup root), timeout default 5 s → unknown; exit 0 is true; stdout (4 KB) in `detail`; cached until the next file event or step change, so the ticker does not re-run builds |
| `layout` | sessiond on the last state | `protocol.MatchLayout` |
| `state` | sessiond on the last state | `protocol.Predicate`; no state yet → unknown |

Combinators `all`, `any`, `not` use three-valued logic: `all` is false if any child is false, else unknown if any is unknown; `any` is true if any child is true, else unknown if any is unknown; `not` keeps unknown. A step completes only when every top-level condition is true: the engine emits `step complete`, reports `done`, takes the next step's checkpoint and emits its `loaded`. Stuck rules run on the ticker (`minibuffer_seconds`, `idle_seconds`, `step_seconds`, `quits_in_window`, `unbound_repeats`) and emit `nudge`, except while a demo plays.

The lifecycle is api.md §4.1. Besides completing, a step can be:

- **demonstrated**: `rpc demo start` ("Watch it") first restores the step's checkpoint when the engine counted a command since the step started or was last reset (as `restore {}`: a `step reset` event, but no reset counted), then sets the demo latch and a playing flag. While the latch is set, a completion is recorded once as `demonstrated` (event and progress); the step stays active, nothing advances and no checkpoint is taken. `demo end` clears only the playing flag; `restore {}`, a reload of the step and `drop-step` clear the latch, so the learner's own completion afterwards is `done`.
- **skipped**: `rpc skip` restores the checkpoint when commands ran, holds completions, plays the step's demo into the learner's frame (`protocol.PlayDemo`), waits up to 8 s for the conditions, records `skipped`, emits `step skipped` and advances like a completion. Bounded by 60 s; it finishes even when the browser goes away.
- **dropped**: `drop-step {lessonId}` forgets that lesson's saved progress, `drop-step {}` all of it; a forgotten active step emits `step dropped` and no step is active.
- **rehearsed**: during a fast-forward (§3.12) `Engine.Rehearse` makes a step active with nothing recorded, emitted or persisted.

Resuming a lesson (`scenario` without `stepId`) starts at the first step neither done nor skipped. The engine's saved state (`/work/.playground/state.json`) keeps the active step and, per lesson, the done, skipped and demonstrated times, hints and resets per step.

### 3.9 Checkpoints

The workspace is a git repository. Checkpoints and restores never touch the learner's index or branch; both use a temporary index:

```
export GIT_INDEX_FILE=/work/.playground/index.tmp
git add -A -- . ':!.playground'
tree=$(git write-tree)
commit=$(git commit-tree "$tree" -m "checkpoint <lesson>/<step>" ${parent:+-p $parent})
git update-ref refs/playground/<lesson>/<step> "$commit"
```

Build output (`target`, `_build`, `.venv`, `node_modules`), `.playground/` and emacs.d's package and cache directories are in `.git/info/exclude`, so checkpoints stay small and a restore never deletes installed packages. Git run by sessiond (checkpoints, shell conditions, supervised processes) gets `safe.directory=*` through `GIT_CONFIG_COUNT`, since a bind-mounted `/work` belongs to the host uid.

Next to each ref, `/work/.playground/checkpoints/<ref>.json` (`checkpoint.Meta`) keeps what the commit cannot:

| Field | Content | Why |
|---|---|---|
| `spec` | the agent's `WindowSpec` (api.md §7): files and points, window state, tabs, help buffers | windows, Dired and buffer lists come back as they were |
| `buffers` | per file buffer: unsaved text (≤ 256 KiB) and the undo history reduced to readable entries (≤ 128 KiB); 512 KiB in all | a step often starts with the previous step's unsaved edit (auto-save waits 5 s) and may rely on its undo history |
| `globals` | printed values of `sessiond.GlobalVars` (≤ 256 KiB): kill ring, minibuffer, file, buffer, search and replace histories, the keyboard macro and its counters | "Watch it" followed by "Do it yourself" must not leave the demo's kills, commands or macro counts behind. Add a variable here when a lesson depends on another piece of session state |
| `emptyDirs` | the workspace's empty directories (at most 1000) | git holds no empty directories |

Restore: `git restore --source=<ref> --worktree --staged -- .`, `git clean -fd` keeping build output and `.playground`, recreate the empty directories, keep `emacs.d/modules.el` as it is (modules only grow, emacs.md §4, and the running Emacs has the current set loaded), then the agent's `reset-step` with the spec, then the unsaved text (`replace-buffer-contents`) and undo histories with undo recording off, then the session variables. The restore commands run with `GIT_INDEX_FILE=/work/.playground/index.tmp`, which then holds the checkpoint, so `git clean` removes exactly the files the checkpoint does not have. The learner's index keeps whatever the learner staged, and course files stay untracked (no `A` in `git status`, no `Git@` in the mode line); `Manager.Init` resets an index older workspaces had filled once (marker `.playground/learner-index-reset`). `restoredFiles` counts the files restored or removed on disk and those whose unsaved changes the restore discarded. TODO(sessiond, backend.md §3.9): Magit lessons that rely on staged changes need the restore to put the staging area back too. Once started, a restore runs to the end without the request's cancellation: a page navigating away during "Go to this step" must not leave the step half restored.

Git commands run detached from the request, bounded by `checkpoint.GitTimeout` (2 min), and the manager serialises them. A git lock file older than `checkpoint.StaleLockAge` (10 s) that blocks a command belongs to a git process that died; it is removed and the command runs once more.

### 3.10 Preview and file watching

`/preview/` serves files through `os.Root("/work")`, which forbids escaping the workspace through `..` or symlinks; `.playground/` is hidden. Headers: `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, and `Content-Security-Policy: sandbox allow-scripts allow-same-origin` for HTML and other active documents (the preview is served from the session path, not a separate origin). `fsnotify` watches `/work` recursively, adding directories as they appear and ignoring `.git`, `target`, `_build`, `.venv`, `node_modules`, `.playground` and Emacs lock and backup files; events are coalesced per path at 200 ms.

### 3.11 VNC bridge

`/vnc` copies bytes between the WebSocket and Xvnc on `127.0.0.1:5901` in both directions; client-to-server bytes also pass through the RFB key decoder. Xvnc has `-SecurityTypes None` and listens on localhost only; the only path to it is through sessiond. Close codes are in api.md §3.1.

### 3.12 Scenario loader

`rpc scenario {lessonId, stepId?, reset?}`, serialised:

1. Reloading the active lesson (same or no `stepId`, no `reset`) answers at once without setup, so a page reload never re-runs setup or overwrites a checkpoint.
2. Fetch the scenario and the fixture tarball from the gateway, cached by ETag under `/work/.playground/cache` and used from the cache when the gateway is unreachable.
3. Extract fixtures into `/work/<setup.root>` through `os.Root`, keeping existing files unless `reset` (request or `setup.reset`). With reset the root is made to match the fixtures: edited files are overwritten and files and directories the fixtures lack are removed, except `target`, `_build`, `.venv`, `node_modules` and `.git`, so lessons are replayable in any order.
4. Modules: the enabled set becomes the current set plus the scenario's (`rpc modules` replaces it exactly). When it changes, write `modules.el`, emit `modules {restarting: true}`, return `restarting: true`, and finish the load after the daemon restart.
5. Setup in Emacs (agent `setup`: open, layout, eval, compile; emacs.md §10.3), after waiting up to 15 s for a restart in progress and for the terminal frame, so layouts never land in the daemon's invisible initial frame. `compile` runs in the project root of the first `open` file, or that file's directory.
6. Starting state. For the first step it is the setup. For a later step ("Go to this step", "Do this step again", resuming mid-lesson) it is the state the step's demo was written for: the step's checkpoint is restored when it exists; otherwise the loader fast-forwards, rehearsing each earlier step and playing its demo into Emacs at playctl pacing, waiting up to 10 s for its conditions (3 min in all, surviving the browser going away), and then takes the step's checkpoint. A later step's checkpoint is never overwritten with the lesson's initial state.
7. Take the step's checkpoint when it does not exist yet or the fixtures were re-extracted, activate the step and emit `step loaded`.

### 3.13 Configuration

sessiond is configured by `PG_*` environment variables (`protocol.SessiondConfig`, defaults in its `default` tags): `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_PROXY_SECRET`, `PG_GATEWAY_URL`, `PG_LISTEN` (`:7000`), `PG_WORKSPACE` (`/work`), `PG_RUN_DIR`, `PG_AGENT_SOCKET`, `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`, `PG_PRISTINE_CONFIG`, `PG_EMACS_SERVER`, `PG_TMUX_SOCKET`, `PG_DISPLAY`, `PG_VNC_PORT`, `PG_GUI_ENABLED`, `PG_VERSIONS_FILE`, `PG_LOG_LEVEL`, `PG_LOG_FORMAT`, `PG_AUTHOR_MODE`, and the binaries `PG_EMACS_BIN`, `PG_EMACSCLIENT_BIN`, `PG_TMUX_BIN`, `PG_XVNC_BIN`, `PG_XDOTOOL_BIN`, `PG_GIT_BIN` (overridable for tests). Each also has a flag named after its JSON key (`-listen`, `-workspace`, …) that overrides it. `sessiond config` prints the effective configuration; `sessiond healthcheck` is the Compose health command (exit 0 when `/healthz` is 200).

`sessiond.ProxyAuth`: with `PG_PROXY_SECRET` set (docker backend), a middleware in front of every route compares `X-Playground-Proxy` with it in constant time and answers 401 otherwise; only `GET`/`HEAD /healthz` is open (the gateway's start-up probe and the health check). The header is removed before the handlers run. Emacs, tmux and shell conditions get the environment with `PG_PROXY_SECRET` and `PG_SESSION_TOKEN` emptied. Without the variable (local mode, `make dev`) every request is served, as only the learner's own gateway and host reach the session.

## 4. playctl

| Command | Purpose |
|---|---|
| `playctl curriculum validate [dir]` | schema-check scenarios, lesson front matter, tables, fixture paths and key names; non-zero on errors |
| `playctl curriculum build [dir] -o out/ [-lenient]` | produce `curriculum.json`, lesson bundles and fixture tarballs; `-lenient` drops broken lessons (test builds only) |
| `playctl curriculum play <sel>... [-session URL] [-timeout 30s] [-speed 1] [-watch] [-repeat N] [-reset-progress=false]` | replay lessons' demos (a lesson id, `2.3`, `part:N`, a part directory or `all`) against a running session through `/rpc/scenario` and `/rpc/keys` at playctl pacing and require each step to complete; prints a table, lists the failed steps with their unmet conditions and exits non-zero on failures. `-session` is sessiond itself (`http://127.0.0.1:7000`, the default, under `make dev`) or a gateway session path; default `$PG_PLAY_SESSION`. `-watch` first plays each step as a "Watch it" demonstration (`demo start`, demo, `demo end`, expect `step demonstrated` with the step still active, then `restore {}`); `-repeat N` plays every lesson N times; with a gateway session path (`…/s/<id>`) each played lesson's progress is reset afterwards |
| `playctl config schema`, `playctl config check <file>` | emit the configuration JSON Schema; validate a configuration file |
| `playctl protocol typescript -o web/src/protocol.gen.ts [-check]` | generate the TypeScript types (api.md §8) |
| `playctl images build [session\|gateway\|remote-box]` | `docker build` wrapper |
| `playctl dev`, `playctl fonts`, `playctl session shell` | development helpers (notes.md §3) |
| `playctl export-user <id>` | tarball of a workspace and its progress (server); not implemented yet |

## 5. Cross-cutting

- Configuration: `internal/config` loads defaults, the JSON file (`-config`, `PG_CONFIG` or `/etc/playground/config.json`), `PG_*` environment overrides, then flags; unknown keys are errors. The result is validated and logged with secrets redacted. The struct is the source of `deploy/config.schema.json` (deployment.md §3).
- Logging: `log/slog` JSON to stdout with `request_id`, `session_id`, `user_id` where known. Never log cookie values, tokens or workspace file contents.
- Metrics (optional): sessions by state, session create duration, WebSocket connections, events per second, RPC durations, step evaluations, checkpoint durations, proxy latency.
- Errors: handlers return `httpx` errors, rendered as `application/problem+json` with the codes of api.md §1.
- Timeouts: every subprocess runs with a context; every RPC and agent call has a deadline; HTTP read-header timeout 10 s, idle 120 s.
- Shutdown: on SIGTERM sessiond emits `emacs stopping`, snapshots the windows, saves modified file buffers (`FlushForm`), stops Emacs (SIGTERM, up to 15 s), closes WebSockets with 4003 and stops HTTP. The gateway drains for 10 s.
- Resource hygiene: subprocess output, event buffers, agent lines, checkpoint metadata and file reads for checks are bounded.

## 6. Testing

- Unit (`go test ./...`, no Docker, Emacs or tmux): input tap decoders (legacy, kitty, RFB, late keys), the step engine (conditions, combinators, debounce, stuck rules, demo latch, skip), checkpoint commands and stale locks, the scenario loader ("Go to this step"), buffer and globals capture, cookie signing, origin checks, both stores, the docker backend against a fake client, the protocol drift test.
- Docker integration (`-tags dockerintegration ./internal/docker/`): the docker backend against a real Engine. `images/gateway/test/server-mode.sh` checks server mode end to end, including network isolation and the re-attach after replacing the gateway container.
- sessiond integration (`-tags integration ./internal/sessiond/ -run Integration`): sessiond in the session image with real Emacs, tmux and the agent (`/healthz`, `/status`, `/events`, `/pty`, keys, checkpoints and restore, frame respawn; with an in-test gateway, a whole lesson; optionally the GUI frame). notes.md §3 has the commands.
- Emacs side: ERT tests in the session image (emacs.md §10.6).
- End to end: `web/e2e/smoke.mjs` and `playctl curriculum play` against the Compose stack (frontend.md §14).

## 7. Conventions

`gofmt` and `go vet` in CI (`make lint`). Context first argument everywhere. No global state except the slog default. Errors wrapped with `%w` and inspected with `errors.Is/As`. Each package documents its public surface in `doc.go`. Generated files carry a header and are checked for drift in CI.
