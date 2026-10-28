# Backend (Go)

One Go module: the gateway fronts everything, sessiond runs in each session container, playctl is the developer and CI tool. Wire contracts are in api.md; each package's `doc.go` has the detail.

## 1. Module layout

```
cmd/{gateway,sessiond,playctl}   flag parsing and wiring only
internal/protocol    all wire, scenario, curriculum, config types; tsgen/ (api.md §8)
internal/config      defaults < JSON file < PG_* env < flags
internal/httpx       problem+json, JSON, middleware, WebSocket helpers
internal/curriculum  loader, validator, bundle builder
internal/docker      Engine client wrapper, hardening
internal/gateway/    auth sessions proxy store curriculum progress limit
internal/sessiond/   supervisor pty agent inputtap events steps checkpoint preview vnc scenario gwclient
web/embed.go         bundle in the gateway binary (tag embedweb)
```

Rules: `cmd/*` only wires; `protocol` uses only the standard library; `gateway` and `sessiond` never import each other; anything talking to a subprocess or the network sits behind an interface so tests can fake it.

## 2. Gateway

### 2.1 Responsibilities

Serve the web bundle, fonts and curriculum; authenticate and issue cookies; own the session registry (one session per learner); proxy `/s/{id}/…` to sessiond; store progress, stuck reports and an audit log; stop idle sessions, expire volumes, rebuild the registry from container labels at start.

### 2.2 HTTP surface

- `/`: web bundle, SPA fallback, security headers; `/fonts/`.
- `/api/v1/*`: api.md §2. `/internal/*`: sessiond → gateway (api.md §2.6).
- `/s/{id}/*`: reverse proxy to sessiond, owner only.
- `/healthz`, `/metrics` (optionally on a separate listener).

`net/http.ServeMux` with method patterns; middleware: request id, logging (successful polling at debug), recover, Origin check, auth per route group.

### 2.3 Sessions, backends, proxy secret

Interfaces in `internal/gateway/sessions` (`Manager`, implemented by `Service`; `Backend`; `Networker`), `store.Store`, `auth.Authenticator`.

- **StaticBackend** (local): one pre-existing session at `local.sessionUrl` shared by all users, id and token `local.sessionId`/`local.sessionToken` (default `local`). Lifecycle calls are no-ops; `Inspect` probes `/healthz`.
- **DockerBackend** (server, experimental): container `pg-session-<id>` labelled `playground.{session,user,version,role}`, volume `pg-ws-<user>` at `/work`, `docker.Harden` (deployment.md §9.2). Tokens are 256-bit random, stored as SHA-256.
- **Isolation** `sessions.isolation`: `network` (default) gives each session an internal network `pg-net-<id>` (a `/28` from `sessions.subnetPool`), which the gateway container (`sessions.gatewayContainer`, default its hostname) joins as `gateway`. The registry rebuild re-attaches a new gateway container; rebuild and reaper prune networks without a session. `shared` uses `sessions.network`.
- **Proxy secret**: all gateway → session traffic (proxy, `Service.Call`, `Service.Status`) carries `X-Playground-Proxy: hex(HMAC-SHA256(key, "playground session proxy\0" + id))`, key in `<dataDir>/session-proxy.secret`; stable across restarts and re-creation; given to the container as `PG_PROXY_SECRET` (§3.13). The static session has none.

### 2.4 Session lifecycle

`Ensure` is idempotent and serialised per user: a healthy running session is returned; a stopped or failed one is re-created on its volume with a fresh token; else volume, remote box and container are created and `/healthz` polled up to 30 s. Failure marks it `failed`, removes the container, keeps the volume. `sessions.maxSessions` bounds running sessions.

Presence: the proxy's WebSocket counts plus sessiond's 30 s heartbeat. The reaper (every minute) marks sessions without WebSockets `idle`, stops them after `sessions.idleStopMinutes` (volume kept), and removes volumes stopped longer than `sessions.volumeRetentionDays` (audited).

### 2.5 Proxying

One cached `httputil.ReverseProxy` per session, pruned when the session goes. `Rewrite` strips `/s/{id}`; sets `X-Playground-User`, the request id and `X-Playground-Proxy` (a browser value is dropped); removes `Cookie` and `Authorization`; drops sessiond's `Set-Cookie`. At most 6 WebSockets per user; sessions not `ready`/`idle` answer `503 session_unavailable`. `rpc eval` needs `features.authorMode`; `rpc drop-step` is always refused.

### 2.6 Authentication

- `none`: single user `local`, local mode only.
- `invite`: codes stored as SHA-256, use counts and expiry, rate limited per IP and code; redeeming creates a user labelled by the code. The invites file (`deploy/invites.example.txt`) is synced at start and on SIGHUP.
- `oidc`: login routes answer 501. TODO(gateway, backend.md §2.6): go-oidc with PKCE, user id = issuer + `sub`, `allowedEmailDomains`.
- Cookie `pg_session` = `<b64 user>.<expiry>.<b64 HMAC-SHA256>` keyed by `auth.cookieSecretFile` (generated on first start); `HttpOnly`, `SameSite=Lax`, `Secure` on https; `auth.sessionDays`, renewed at most daily.
- CSRF: `Origin` check only (api.md §1).

### 2.7 Store

Server: SQLite `<dataDir>/playground.db` (`modernc.org/sqlite`, no cgo), one writer, WAL, `busy_timeout` 5 s; schema in the embedded `store/migrations/`. Local: the same interface over `<dataDir>/local.json`, rewritten atomically, newest 50 stuck reports.

### 2.8 Curriculum serving

Serves `playctl curriculum build` output from `curriculum.dir` (layout in `protocol/curriculum.go`) through `os.Root` with strong ETags. A wrong `protocol.CurriculumFormat` stops the gateway; a missing bundle makes the curriculum endpoints answer 503 until it appears (SIGHUP, or automatically in author mode).

### 2.9 Progress

sessiond reports progress (step start, completion, demonstrated, skipped, hints via `rpc hint`, resets), so it survives a tab closing mid-step. `progress.Service.Apply` only raises a status (api.md §2.4), takes absolute `hintsUsed`/`resets`, and rejects lessons or steps outside the loaded bundle (the learner can read their session token). `POST /progress/reset` then sends the session `drop-step` (best effort, 5 s).

## 3. sessiond

### 3.1 Responsibilities

Keep Emacs, tmux and Xvnc alive; serve PTY, events, VNC, RPC and preview; tap input; evaluate the active step; checkpoint and restore; load scenarios; report progress. Runs as `learner` under `tini`. `sessiond -fake` replaces Emacs, tmux and Xvnc with bash and a fake agent.

### 3.2 Startup sequence

1. Parse the environment; open the agent socket.
2. Initialise the workspace if needed: `git init`, `.playground/`, pristine config to `/work/emacs.d` (first start only), `modules.el` = `base agent`.
3. Start HTTP; `/healthz` answers 503 until step 4 is done.
4. Start the daemon and wait for `hello` (60 s, then restart once).
5. Start tmux and the terminal frame.
6. Resume the step in `.playground/state.json`, if any, without fixtures or checkpoint: re-open `snapshot.json` when it shows a file under the lesson root, else run setup without `reset`. Until then snapshots are held (`Supervisor.HoldSnapshots`), so the fresh `*scratch*` never replaces the learner's windows.

### 3.3 Supervisor

- Daemon `emacs --fg-daemon=playground --init-directory=/work/emacs.d`: backoff 1-8 s; after 5 restarts in 10 min give up (`/healthz` 503) until `restart-emacs`.
- tmux `-L pg`, session `emacs`, pane `emacsclient -t -s playground`: `remain-on-exit on` and a session-level `pane-died` hook (overriding the image's global one) so only sessiond respawns the pane, with a growing delay when it keeps dying (`frame-closed`, `frame-opened`). `escape-time` 10 ms.
- Xvnc `:1` (localhost, no auth, `-AcceptSetDesktopSize=1`) and the GUI frame (`emacsclient -c`, width and height only, no `fullscreen`): restarted while GUI mode is on. The browser resizes the screen through RandR; the config fits frames to it (emacs.md §6). Leaving GUI closes `/vnc` with 4002, deletes the X frames and closes Emacs's X connections before Xvnc stops; if Emacs cannot let go, Xvnc keeps running rather than dying under Emacs.
- Heartbeat: agent pings every 2 s; five missed → `emacs unresponsive` and SIGUSR2; resumed → `responsive`. Rescue then sends C-g via tmux; still busy 10 s later → `error emacs_unavailable` offering a restart.
- Restart (`restart-emacs`, module change): `restarting`, snapshot, save modified file buffers (`supervisor.FlushForm`: never over a file changed on disk, skipping remote and read-only, whatever `auto-save-visited-mode` says), restart, hello, re-open the snapshot, respawn the pane, `daemon-restarted` once the frame is open. Snapshots also every 30 s.

### 3.4 PTY

`/pty` runs `tmux attach-session -d` on a pty. One viewer: a new one sends `replaced` to the old and closes it with 4000. Input goes through the tap's decoder, output through its mode tracker.

### 3.5 Agent connection and RPC

One agent connection (api.md §7), replaced by a new one. Calls are serialised through one goroutine; each carries `timeoutMs` and sessiond waits that plus a grace (default 2 s, `eval` ≤ 10 s). TODO(sessiond, backend.md §3.5): `rpcKeys` maps supervisor errors to 400/409 by message text instead of sentinel errors.

### 3.6 Input tap

Decodes input into Emacs keys independently of Emacs to emit `prefix` within milliseconds; the agent's `key` stays authoritative.

- Terminal decoder: legacy bytes (`ESC`+byte within 50 ms → `M-`), CSI/SS3, `modifyOtherKeys`, CSI u (the browser sends it for chords legacy encoding lacks, frontend.md §5); skips paste, mouse and terminal replies. A mode tracker watches output for kitty keyboard and `modifyOtherKeys` changes.
- RFB decoder on `/vnc`: KeyEvent and QEMU extended keys, keysyms via a generated table.
- Prefix tracker (`inputtap.DefaultPrefixes`; M-o is not a prefix): `prefix` while the sequence is a prefix, `prefix ""` as soon as a key ends it, since the agent's `key` may come much later (`C-h k`). `C-h` after a prefix keeps it pending one more key for which-key paging (`n`/`p`/`d` keep, `u` drops a key, `a` ends).
- Late keys: a lone `ESC` is reported when the 50 ms meta timeout ends, but tmux passes it on after 10 ms. A late key whose bytes arrived before the agent's last command was already consumed (third `ESC` of `ESC ESC ESC`): it is dropped and the pending prefix cleared, else `ESC` would stay pending and turn the next `C-x` into `C-M-x`.

### 3.7 Event bus

`events.Bus` stamps a monotonic `seq`, encodes once and shares the frame with subscribers, the 500-event replay ring, the stuck window (`Bus.Recent`, 200) and the event log (deployment.md §9.4). A subscriber whose 256-entry buffer fills gets `gap` instead of blocking producers.

### 3.8 Step engine

`steps.Engine` holds the active step, counts commands since step and lesson start, evaluates conditions, emits `check`/`step`, reports progress, checkpoints the next step on completion and runs stuck rules.

Triggers: agent `key`/`state`, `file` events, a 2 s ticker (for asynchronous output). One goroutine with a trailing debounce: 40 ms after the last trigger, at most 120 ms after the first (`DefaultDebounce`, `DefaultDebounceMax`); `check` follows its `key` by about 50 ms.

Evaluators: `event` from command counts; `elisp` by agent `check`, batched (error or timeout → unknown); `file` through `os.Root`, fresh, ≤ 1 MB; `shell` as `sh -c` in `cwd` (default setup root), exit 0 true, timeout → unknown, stdout ≤ 4 KB in `detail`, cached until the next file event or step change; `layout` and `state` on the last agent state (no state → unknown). `all`/`any`/`not` are three-valued. A step completes when every top-level condition is true. Stuck rules run on the ticker, never while a demo plays.

Statuses and the demo latch: api.md §4.1. Internals:

- The restore of `demo start` or `skip` emits `step reset` without counting a reset.
- `skip` holds completions while `protocol.PlayDemo` plays, detached from the request.
- `Engine.Rehearse` (fast-forward, §3.12) activates a step with nothing recorded, emitted or persisted.
- `.playground/state.json` keeps the active step and per lesson the done, skipped and demonstrated times, hints and resets.

### 3.9 Checkpoints

The workspace is a git repository. A checkpoint is a commit at `refs/playground/<lesson>/<step>` made with a temporary index (`GIT_INDEX_FILE=.playground/index.tmp`), so the learner's index and branch are untouched and course files stay untracked. `.git/info/exclude` lists build output (`target`, `_build`, `.venv`, `node_modules`), `.playground/` and emacs.d's package and cache directories. sessiond's git gets `safe.directory=*` via `GIT_CONFIG_COUNT` (bind-mounted `/work` belongs to the host uid).

`.playground/checkpoints/<ref>.json` (`checkpoint.Meta`) adds what git cannot hold:

- `spec`: the agent's `WindowSpec` (api.md §7).
- `buffers`: each file buffer's unsaved text (≤ 256 KiB) and readable undo entries (≤ 128 KiB), 512 KiB in all. A step usually starts with the previous step's edit still unsaved (auto-save waits 5 s).
- `globals`: printed `sessiond.GlobalVars` (kill ring, minibuffer and search histories, last macro and counters; ≤ 256 KiB), so Watch it then Do it yourself leaves none of the demo's kills, history or macro counts. Add a variable when a lesson depends on more session state.
- `emptyDirs` (≤ 1000).

Restore: `git restore --worktree --staged` and `git clean -fd` against the temporary index (removing exactly what the checkpoint lacks, except build output and `.playground`); recreate empty directories; keep `emacs.d/modules.el` (modules only grow, emacs.md §4); agent `reset-step`; then unsaved text (`replace-buffer-contents`) and undo with recording off; then globals. `restoredFiles` counts files changed on disk plus buffers whose unsaved edits were discarded.

A started restore runs to the end without the request's cancellation (`Server.restoreCheckpoint`). Git commands are serialised, detached and bounded by `checkpoint.GitTimeout`; a lock older than `checkpoint.StaleLockAge` (10 s) that blocks a command is removed and the command retried once.

Limits and open items:

- Not captured: the staging area (TODO(sessiond, backend.md §3.9), for Magit lessons), minor modes a demo turned on, variables outside `GlobalVars` such as `display-buffer-alist`; a step whose state lives there lacks it after Go to this step or Watch it.
- `/work` being the checkpoint repository makes project.el treat it as a project (`emacs.d` in `project-files`); hiding it needs a separate `GIT_DIR`.

### 3.10 Preview and file watching

`/preview/` serves `/work` through `os.Root` (no `..` or symlink escape; `.playground/` hidden), headers in api.md §3.5. The CSP's `allow-same-origin` is an open question (deployment.md §9.3). `fsnotify` watches `/work` recursively (ignoring `.git`, build output, `.playground`, lock and backup files), coalescing per path at 200 ms.

### 3.11 VNC bridge

`/vnc` copies bytes between the WebSocket and Xvnc on `127.0.0.1:5901`, feeding client bytes to the RFB decoder. sessiond is the only way to Xvnc.

### 3.12 Scenario loader

`rpc scenario {lessonId, stepId?, reset?}`, serialised:

1. Reloading the active lesson (same or no `stepId`, no `reset`) returns at once, never re-running setup.
2. Fetch scenario and fixtures, cached by ETag in `.playground/cache` and used from there when the gateway is down.
3. Extract fixtures into `/work/<setup.root>`, keeping existing files unless `reset` (request or `setup.reset`); with reset the root matches the fixtures exactly (except build output and `.git`), so lessons replay in any order.
4. Modules = current ∪ scenario's (`rpc modules` sets exactly), so lessons must pass with more modules than they list. On change: write `modules.el`, emit `modules {restarting}`, return `restarting: true`, finish after the restart.
5. Agent `setup` (emacs.md §10.3), after waiting ≤ 15 s for a restart and for the terminal frame, so layouts never land in the daemon's invisible frame. `compile` runs in the project root of the first `open` file. TODO(sessiond, CURRICULUM.md Part B.3): `.eld` layouts are ignored.
6. A later step (Go to this step, resume mid-lesson) starts from its checkpoint; without one the loader fast-forwards, rehearsing each earlier step and playing its demo at playctl pacing (≤ 10 s per step, 3 min total, detached from the request). A later step's checkpoint is never overwritten with the lesson's initial state.
7. Take the step's checkpoint if missing or fixtures were re-extracted, activate the step, emit `step loaded`, return.

### 3.13 Configuration and proxy auth

Environment only: `protocol.SessiondConfig` (names and defaults in its tags), each overridable by a flag named after its JSON key. `sessiond config` prints it; `sessiond healthcheck` is the Compose health command.

`sessiond.ProxyAuth`: with `PG_PROXY_SECRET` set, every request except `GET`/`HEAD /healthz` needs it in `X-Playground-Proxy` (constant-time), else 401; the header is stripped. Emacs, tmux and shell conditions see `PG_PROXY_SECRET` and `PG_SESSION_TOKEN` empty. Unset (local mode, `make dev`), everything is served.

## 4. playctl

- `curriculum validate [dir]`; `curriculum build [dir] -o out [-lenient]` (`-lenient` drops broken lessons, test builds only).
- `curriculum play <lesson id|2.3|part:N|all>… [-session URL] [-timeout] [-speed] [-watch] [-repeat N] [-reset-progress]`: loads each lesson by `/rpc/scenario`, plays its demos by `/rpc/keys` at playctl pacing, requires every step to complete, prints a table and the unmet conditions, exits non-zero on failure. `-session` is sessiond (default `$PG_PLAY_SESSION`) or a gateway session path, after which played lessons' progress is reset. `-watch` plays each step first as Watch it (`demo start`, demo, `demo end`, expect `step demonstrated`, `restore {}`).
- `config schema`, `config check <file>`; `protocol typescript [-check]` (api.md §8); `images build`, `dev`, `fonts`, `session shell` (notes.md §3); `export-user` is a TODO.

## 5. Cross-cutting

- Config: unknown keys are errors; logged with secrets redacted; the struct generates `deploy/config.schema.json`.
- Logging: `log/slog` JSON with `request_id`, `session_id`, `user_id`; never cookies, tokens or workspace contents.
- Metrics (optional): sessions, WebSockets, events, RPC, evaluation, checkpoint and proxy durations. TODO(gateway, backend.md §5): creation duration, latency histograms.
- Errors: handlers return `httpx` errors, rendered as problem+json with api.md §1's codes.
- Every subprocess has a context, every call a deadline; HTTP read-header 10 s, idle 120 s; WebSocket pings every 20 s. Everything buffered is bounded.
- Shutdown: sessiond emits `emacs stopping`, snapshots, `FlushForm`, stops Emacs (≤ 15 s), closes WebSockets with 4003; the gateway drains 10 s.

## 6. Testing

- `go test ./...` needs no Docker, Emacs or tmux: decoders, engine, checkpoints, loader, capture, auth, stores, docker backend against a fake, protocol drift.
- `-tags dockerintegration ./internal/docker/`; `images/gateway/test/server-mode.sh` (isolation, re-attach).
- `-tags integration ./internal/sessiond/` against the session image (notes.md §3).
- Emacs ERT (emacs.md §10.6); `web/e2e/smoke.mjs` and `playctl curriculum play` (frontend.md §14).

## 7. Conventions

`gofmt`, `go vet`. Context first. No globals but the slog default. Wrap with `%w`, inspect with `errors.Is/As`. `doc.go` per package. Generated files carry a header and a drift check.
