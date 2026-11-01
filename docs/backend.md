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
internal/sessiond/   session supervisor pty agent inputtap events steps snapshot checkpoint progress preview vnc scenario gwclient sshbox
web/embed.go         bundle in the gateway binary (tag embedweb)
```

Rules: `cmd/*` only wires; `protocol` uses only the standard library; `gateway` and `sessiond` never import each other; anything talking to a subprocess or the network sits behind an interface so tests can fake it.

## 2. Gateway

### 2.1 Responsibilities

Serve the web bundle, fonts and curriculum; authenticate; own the session registry (one session per learner); proxy `/s/{id}/…` to sessiond; store progress, stuck reports and an audit log; stop idle sessions, expire volumes, rebuild the registry from container labels at start.

### 2.2 HTTP surface

- `/`: web bundle, SPA fallback, security headers; `/fonts/`.
- `/api/v1/*`: api.md §2. `/internal/*`: sessiond → gateway (api.md §2.6).
- `/s/{id}/*`: reverse proxy to sessiond, owner only.
- `/healthz`, `/metrics` (optionally on a separate listener).

`net/http.ServeMux` with method patterns; middleware: request id, logging (successful polling at debug), recover, Origin check, auth per route group.

### 2.3 Sessions, backends, proxy secret

Interfaces: `sessions.Manager` (implemented by `Service`), `Backend`, `Networker`; `store.Store`; `auth.Authenticator`.

- **StaticBackend** (local): one pre-existing session at `local.sessionUrl` shared by all users, id and token `local.sessionId`/`local.sessionToken` (default `local`). Lifecycle calls are no-ops; `Inspect` probes `/healthz`.
- **DockerBackend** (server, experimental): container `pg-session-<id>` labelled `playground.{session,user,version,role}`, volume `pg-ws-<user>` at `/work`, `docker.Harden` (deployment.md §9.2); 256-bit tokens stored as SHA-256. With `sessions.remoteBox.enabled`, a box `pg-box-<id>` (`docker.HardenBox`, alias `box`) gets the public half of a fresh ed25519 pair and sessiond the private half (`PG_BOX_KEY`, `PG_BOX_HOST`); a box that fails to start leaves the session without one.
- **Isolation** `sessions.isolation`: `network` (default) gives each session an internal network `pg-net-<id>` (a `/28` from `sessions.subnetPool`) that the gateway container (`sessions.gatewayContainer`) joins as `gateway`; the registry rebuild re-attaches a new gateway container and prunes orphan networks, as does the reaper. `shared` uses `sessions.network`.
- **Proxy secret**: all gateway → session traffic (proxy, `Service.Call`, `Service.Status`) carries `X-Playground-Proxy: hex(HMAC-SHA256(key, "playground session proxy\0" + id))`, key in `<dataDir>/session-proxy.secret`; stable across restarts and re-creation; given to the container as `PG_PROXY_SECRET` (§3.13). The static session has none.

### 2.4 Session lifecycle

`Ensure` is idempotent and serialised per user: a healthy running session is returned; a stopped or failed one is re-created on its volume with a fresh token; else volume, remote box and container are created and `/healthz` polled up to 30 s. Failure marks it `failed` and removes the container, keeping the volume. `sessions.maxSessions` bounds running sessions (`429 session_limit`), and under the same lock `sessions.groupLimits` bounds each group's (`429 group_quota_exceeded`, api.md §1). A learner's quota group is the first listed group among those stored at their last sign-in, else the `defaultGroupMaxActiveSessions` pool; each session records it, and the registry rebuild restores it.

Presence: the proxy's WebSocket counts plus sessiond's 30 s heartbeat. The reaper (every minute) marks sessions without WebSockets `idle`, stops them after `sessions.idleStopMinutes` (volume kept), and removes volumes stopped longer than `sessions.volumeRetentionDays` (audited).

### 2.5 Proxying

One cached `httputil.ReverseProxy` per session, pruned when the session goes. `Rewrite` strips `/s/{id}`; sets `X-Playground-User`, the request id and `X-Playground-Proxy` (a browser value is dropped); removes `Cookie` and `Authorization`; drops sessiond's `Set-Cookie`. At most 6 WebSockets per user; sessions not `ready`/`idle` answer `503 session_unavailable`. `rpc eval` needs `features.authorMode`; `rpc drop-step` is always refused.

### 2.6 Authentication

- `none`: single user `local`, local mode only; the OIDC routes answer 404.
- `oidc` (`auth/oidc.go`, any provider with discovery): `GET /auth/oidc/start` redirects to the authorization endpoint (code flow, PKCE S256, `state`, `nonce`) and sets `pg_oidc`, signing state, nonce, verifier and a same-origin `next` with a key derived from the cookie secret (10 min, path `/api/v1/auth/oidc/`). The callback checks cookie and state, exchanges the code (`golang.org/x/oauth2`), verifies the ID token (`coreos/go-oidc/v3`: signature, issuer, audience, expiry) and nonce, applies `allowedEmailDomains` (needs `email_verified` true) and `allowedGroups`, upserts the user and redirects to `next`, or to `/login?error=state|provider|token|forbidden`. User id `u-` + 24 hex of SHA-256(issuer `|` `sub`); name, username, email and groups are refreshed at every sign-in. Discovery runs in the background from start, shared by concurrent sign-ins; failures are cached for 15 s. Audit: `login.oidc`, `login.refused`, `logout`.
- Cookie `pg_session` = `<b64 user|epoch>.<expiry>.<b64 HMAC-SHA256>` keyed by `auth.cookieSecretFile` (generated on first start); `HttpOnly`, `SameSite=Lax`, `Secure` on https; valid `auth.sessionDays` from the sign-in, never extended. Every request rechecks the stored user against the allow-lists. Logout bumps the user's session epoch, revoking all their cookies, and answers `200 {redirectUrl}` (the provider's `end_session_endpoint` with `client_id` and `post_logout_redirect_uri` `<publicUrl>/login`, when `providerLogout`) or `204`.
- CSRF: `Origin` check only (api.md §1).

### 2.7 Store

Server: SQLite `<dataDir>/playground.db` (`modernc.org/sqlite`, no cgo), one writer, WAL, `busy_timeout` 5 s; schema in the embedded `store/migrations/`; `users` holds `username`, `email`, `groups_json` and `session_epoch` (0003). Local: the same interface over `<dataDir>/local.json` (version 3; older files are rewritten on open), rewritten atomically, newest 50 stuck reports.

### 2.8 Curriculum serving

Serves `playctl curriculum build` output from `curriculum.dir` (layout in `protocol/curriculum.go`) through `os.Root` with strong ETags. A wrong `protocol.CurriculumFormat` stops the gateway; without a bundle the curriculum endpoints answer 503 until one appears (SIGHUP, or automatically in author mode).

`curriculum.complete` contains the default index's SHA-256 and is published last; an absent or mismatched marker is retried, while invalid lessons in a completed publication are skipped with a warning.

### 2.9 Progress

sessiond reports progress (step start, completions, hints, resets, finished laps), so it survives a tab closing mid-step. `progress.Service.Apply` gives the same result however often a report arrives: a status only rises (api.md §2.4), `hintsUsed`/`resets` are absolute, a lap counts once per `lapId` (`laps.last_lap_id`; it must be `<lessonId>@<digits>`; a report without an id once per completion time and duration), and lessons or steps outside the loaded bundle are rejected (the learner can read their session token). Ingestion is serialized per user. `POST /progress/reset` issues `max(stored maximum + 1, gateway time in milliseconds)` as the reset generation, then sends the session `drop-step` (best effort, 5 s); the clock term makes a reset after a store loss win over a session's older generation. A report older than the gateway's generation is discarded with a warning; a newer one is accepted without advancing it. sessiond's side is the progress outbox (§3.8).

## 3. sessiond

### 3.1 Responsibilities

Keep Emacs, tmux and Xvnc alive; serve PTY, events, VNC, RPC and preview; tap input; own the lesson state and evaluate the active step; checkpoint and restore; load scenarios; report progress. Runs as `learner` under `tini`. `sessiond -fake` replaces Emacs and Xvnc with bash and the agent and tmux with the fake editor.

### 3.2 Startup sequence

1. Parse the environment; open the agent socket.
2. Initialise the workspace if needed: `git init`, `.playground/`, pristine config to `/work/emacs.d` (first start only), `modules.el` = `base agent`; the box key pair and alias (`sshbox`, deployment.md §6).
3. Start HTTP; `/healthz` answers 503 until step 4 is done.
4. Start the daemon and wait for `hello` (60 s, then restart once).
5. Start tmux and the terminal frame.
6. Sync the progress generations (≤ 5 s, best effort) and queue every saved outcome in the outbox (one op of the session core, §3.8), start its sender, then resume the step in `.playground/state.json`, if any, as the resume op (§3.12), without fixtures or checkpoint: re-open `snapshot.json` (§3.9) when it shows a file under the lesson root, else run setup without `reset`. A lesson loaded meanwhile wins. The autosave runs only while no op runs or waits, so the fresh `*scratch*` never replaces the learner's windows.

### 3.3 Supervisor

- Daemon `emacs --fg-daemon=playground --init-directory=/work/emacs.d`: backoff 1-8 s; after 5 restarts in 10 min give up (`/healthz` 503) until `restart-emacs`.
- tmux `-L pg`, session `emacs`, pane `emacsclient -t -s playground`: a session-level `pane-died` hook (emacs.md §5) lets only sessiond respawn the pane, with a growing delay when it keeps dying (`frame-closed`, `frame-opened`).
- Xvnc `:1` (localhost, no auth, `-AcceptSetDesktopSize=1`) and the GUI frame (`emacsclient -c`, no `fullscreen`, emacs.md §6): restarted while GUI mode is on. Leaving GUI closes `/vnc` with 4002, deletes the X frames and closes Emacs's X connections (agent `leave-display`) before Xvnc stops (if Emacs cannot let go, Xvnc keeps running); then `resume-loop` restarts the command loop, which would otherwise swallow the terminal's next input event (the ESC of `ESC [ I`, typing `[I`). The supervisor builds no Lisp: it calls these ops through `supervisor.AgentOps`.
- Heartbeat: agent pings every 2 s; five missed → `emacs unresponsive` and SIGUSR2; resumed → `responsive` and `reset-debug` (the signal leaves `debug-on-quit` set). Rescue then sends C-g via tmux; still busy 10 s later → `error emacs_unavailable` offering a restart.
- Restart (`restart-emacs`, module change; always inside a session-core op): `restarting`, `Hooks.BeforeStop` (the snapshot service writes `snapshot.json` and the agent's `flush-buffers` saves modified file buffers: never over a file changed on disk, skipping remote and read-only ones), restart, hello, `Hooks.AfterRestart` (the core reopens, inline in the op, the editor state of a restore still waiting for the agent, else `snapshot.json`), respawn the pane, `daemon-restarted` once the frame is open. A daemon that crashed gets `AfterRestart` on its first hello, which waits for the core's `recover` op doing the same.

### 3.4 PTY

`/pty` runs `tmux attach-session -d` on a pty. One viewer: a new one sends `replaced` to the old and closes it with 4000. Input goes through the tap's decoder, output through its mode tracker.

### 3.5 Agent connection and RPC

One agent connection (api.md §7), replaced by a new one. Calls are serialised through one goroutine; each carries `timeoutMs` and sessiond waits that plus a grace (default 2 s, `eval` ≤ 10 s). TODO(sessiond, backend.md §3.5): `rpcKeys` maps supervisor errors to 400/409 by message text instead of sentinel errors.

### 3.6 Input tap

Decodes input into Emacs keys to emit `prefix` within milliseconds; the agent's `key` stays authoritative.

- Terminal decoder: legacy bytes (`ESC`+byte within 50 ms → `M-`), CSI/SS3, `modifyOtherKeys`, CSI u (frontend.md §5); skips paste, mouse and terminal replies. A mode tracker watches output for kitty keyboard and `modifyOtherKeys` changes.
- RFB decoder on `/vnc`: KeyEvent and QEMU extended keys, keysyms via a generated table.
- Prefix tracker (`inputtap.DefaultPrefixes`; M-o is not a prefix): `prefix` while the sequence is a prefix, `prefix ""` as soon as a key ends it, since the agent's `key` may come much later (`C-h k`). `C-h` after a prefix keeps it pending one more key for which-key paging (`n`/`p`/`d` keep, `u` drops a key, `a` ends).
- Late keys: a lone `ESC` is reported when the 50 ms meta timeout ends, but tmux passes it on after 10 ms. A late key whose bytes arrived before the agent's last command was already consumed (third `ESC` of `ESC ESC ESC`), so it is dropped and the pending prefix cleared; else the next `C-x` would read as `C-M-x`.

### 3.7 Event bus

`events.Bus` stamps a monotonic `seq`, encodes once and shares the frame with subscribers, the 500-event replay ring, the stuck window (200) and the event log (deployment.md §9.4). A subscriber whose 256-entry buffer fills gets `gap` instead of blocking producers.

### 3.8 Steps: core, phases, evaluator

`session.Core` owns the lesson state (`session.Lesson`: scenario, active step, `Attempt`, `Phase`, demo latch, `Saved`) under `Core.mu`. Ops (§3.12), `Observe` (also while an op runs) and the demo bracket write it under the lock; an op never holds it across I/O, and after any I/O re-reads the attempt and phase in the same lock hold in which it writes, so `demo start` and `skip` enter their phase only while the step is still active and a completion observed first wins. `steps.Evaluator` evaluates the step of the attempt the core began and hands each result with the attempt's key-stream statistics to `Core.Observe`; it knows nothing of progress, laps or checkpoints.

Activation (load, resume, reset, advance): the attempt number rises, demo flags clear, the evaluator begins the attempt (step counts, quits and cached shell results start afresh, lesson counts with another lesson), `state.json` records the step, `step loaded|reset` goes out, an `in-progress` entry goes to the progress outbox (with a counted reset's totals; none for a step already finished or demonstrated), and an evaluation is scheduled. Observations, demos and skips carry the attempt; one of another attempt is ignored.

| Phase | A completion |
|---|---|
| Active | records `done`, emits `complete` (`lapMs` on a lap's last step), enters Advancing and queues the `advance` op first: the next step's checkpoint (when it takes one; a failure is logged) then its activation, or no step after the last |
| Watching (demo latch) | records `demonstrated` once per latch; the step stays active |
| Skipping | nothing: the skip op records `skipped` and advances inline |
| Rehearsing (fast-forward) | nothing, and no `check` either |
| Advancing, Idle | observations are ignored |

`check` goes out when the results change (always for `rpc check`). Incomplete observations run the stuck rules (`session/stuck.go`, edge-triggered, on the statistics) unless a demo plays, a skip runs or a lap is on.

Triggers: agent `key`/`state`, `file` events, a 2 s ticker (for asynchronous output). One goroutine with a trailing debounce: 40 ms after the last trigger, at most 120 ms after the first (`DefaultDebounce`, `DefaultDebounceMax`); `check` follows its `key` by about 50 ms.

Evaluators: `event` from command counts; `elisp` and `buffer` (turned into a form) by batched agent `check` (error or timeout → unknown); `file` through `os.Root`, fresh, ≤ 1 MB; `shell` as `sh -c` in `cwd` (default setup root), exit 0 true, timeout → unknown, stdout ≤ 4 KB in `detail`, cached until the next file event, nested `.git` change or attempt, one run per script at a time, unknown ("running") after 1.5 s while it finishes in the background. A run delivers its result once without caching it when the tree changes during the run, regardless of the writer; the next trigger re-evaluates, and a later file event discards an unconsumed result. A step about to complete on a shell result older than the last command re-runs it first (a save is seen before its file event). `host: box` conditions run over `ssh box` (exit 255 → unknown), cached 3 s; `layout` and `state` use the last agent state (none → unknown). `all`/`any`/`not` are three-valued. A step completes when every top-level condition is true.

Statuses and the demo latch: api.md §4.1. Internals:

- `demo start` (op, 30 s) restores when dirty (a command since activation, or a failed load changed the workspace under the step) and re-activates as an uncounted `reset`, then enters Watching with `Playing`. `demo end`, the page's pty closing and `DemoIdle` (checked on each observation) clear `Playing`; a latch that never demonstrated drops back to Active.
- `skip` (op, 60 s) restores when dirty, enters Skipping, plays the demo with `protocol.PlayDemo` through the supervisor, polls ≤ 8 s, records `skipped` and advances. A drop cancels it, demo included; its deadline still records the skip.
- The fast-forward (§3.12) activates each earlier step as Rehearsing (nothing recorded, emitted or persisted); a load that fails mid-rehearsal makes the step active before it active again (dirty, nothing emitted). A drop of a rehearsed step emits `step dropped`.
- Progress: every outcome (`in-progress` on activation, `done`, `demonstrated`, `skipped`, hint and reset totals) is recorded under `Core.mu` in `progress.Outbox`, stamped with the lesson's reset generation. The outbox holds at most one entry per step and finished lap, in the order they were first recorded; a report folds into its step's last entry as the gateway would apply both (the higher status keeps its time, the latest totals win, lap fields stay), unless both finish different laps, each of which counts. Every activation queues an `in-progress` (the gateway ignores one that adds nothing). One sender posts the head entry (15 s per attempt) until the gateway answers: 2xx removes it unless it changed meanwhile, 4xx drops it with a warning, anything else is retried with backoff from 1 s to 30 s, without a deadline. A report sent twice changes nothing at the gateway, so nothing else is acknowledged.
- Resets: the drop op (`drop-step`) raises the scope's generation in the outbox, purges its older entries and forgets the saved outcomes, lap and active step in one `Core.mu` hold. The generation sync (startup, and before each `scenario`) is one op: it fetches the gateway's generations (≤ 5 s), applies each newer one as a drop, then adopts the gateway's map exactly, lower generations included (a replaced store), and purges entries below it; being an op, no `drop-step` comes between the fetch and the adoption. At startup the same op then queues every saved outcome again with its original time, in one `Core.mu` hold (a gateway that lost its store gets them back; a drop arriving meanwhile runs after it and purges them), then the sender starts; at shutdown it delivers for up to 5 s, and what is left stays in `state.json`.
- `.playground/state.json` (version 2) has one writer, `Server.persist`: after every change it writes, outside `Core.mu`, what `Core.Persistent` read in one `Core.mu` hold: the active step, the running lap, per lesson the done, skipped and demonstrated times, hints and resets, the reset generations and the outbox entries (the `ProgressReport`s they are sent as). The core changes outcomes, generations and entries only under `Core.mu` (the sender only removes delivered entries), so the file always holds a state the session was in. Version 1 (no outbox) reads unchanged; its outcomes reach the gateway by the startup reconciliation.
- Laps: started by `scenario {lap}` with the id `<lesson>@<startedAt>`, kept on resume, ended by any other load or `drop-step`; hints, demos and skips answer 409 and stuck rules pause; the last completion records `lapMs` and the lap id.
- Locks: `Core.mu`, then the evaluator's state lock or the outbox's (leaves); the evaluator calls `Observe` holding neither, the outbox calls the gateway and `persist` holding none. Events go out and outcomes are recorded under `Core.mu`, in the order of the state changes; `state.json` is written after it.

### 3.9 Checkpoints and the snapshot service

The workspace is a git repository. A checkpoint is a commit at `refs/playground/<lesson>/<step>` made with a temporary index (`GIT_INDEX_FILE=.playground/index.tmp`), so the learner's index and branch are untouched and course files stay untracked. `.git/info/exclude` lists build output (`target`, `_build`, `.venv`, `node_modules`), `.playground/` and emacs.d's package and cache directories. sessiond's git gets `safe.directory=*` via `GIT_CONFIG_COUNT` (bind-mounted `/work` belongs to the host uid); checkpoints and shell conditions also get `GIT_OPTIONAL_LOCKS=0`, so their `git status` never holds `index.lock` while the learner's `git add` or `git commit` runs.

`snapshot.Service` is the only path to checkpoints and editor state: it drives `checkpoint.Manager` (git only) and the agent's typed ops through `snapshot.Editor` (`capture-editor`, `restore-editor`, `modified-files`, `open`, `visit`, `flush-buffers`; api.md §7). `.playground/checkpoints/<ref>.json` (`snapshot.Manifest`, version 1; version 0 files read unchanged) adds what git cannot hold:

- `spec`: the agent's `WindowSpec` (api.md §7).
- `buffers`: each file buffer's unsaved text (≤ 256 KiB) and readable undo entries (≤ 128 KiB), 512 KiB in all. A step usually starts with the previous step's edit still unsaved (auto-save waits 5 s).
- `globals`: the printed `snapshot.GlobalVars` (kill ring, minibuffer and search histories, last macro and counters; ≤ 256 KiB), so Watch it then Do it yourself leaves none of the demo's kills, history or macro counts. The list is curriculum knowledge and travels as `vars`; the agent applies the printing rules.
- `commit`: the commit the ref publishes (absent in version 0).
- `emptyDirs` (≤ 1000).
- `nested`: each nested git repository (≤ 50) as two commits, its working tree (its own ignores apply) and its `.git` (minus lock files), kept reachable as extra parents of the checkpoint commit.

Capture: the editor state first (one `capture-editor`, 5 s; none without an agent), then `git add`, `write-tree`, the nested repositories and `commit-tree` (`TakeTree`), then the manifest naming the commit, then `update-ref` (`PublishRef`), then the `checkpoint` event. The ref publishes: a ref is a checkpoint (`Exists`, `List`) only with a readable manifest, and for `Exists` and restores only when that manifest names the ref's commit (version 0 manifests trust the ref). A ref without one, older than this order or left by a first capture stopped between manifest and ref, and a recapture (every advance and fast-forward overwrites the step's checkpoint) stopped there, are invisible: `restore {ref}` answers 404 and the next load takes the checkpoint again. A learner's `checkpoint {label}` is an op of the session core (30 s) like the restores, so the two never interleave.

Restore: read the manifest (none, or not the ref's commit: `ErrNoCheckpoint`, 404); ask `modified-files`; remove the `.git` of nested repositories the checkpoint lacks; `git restore --worktree --staged` and `git clean -fd` against the temporary index, outside the checkpoint's nested repositories (removing exactly what the checkpoint lacks, except build output and `.playground`); restore each nested repository's `.git`, then its working tree; recreate empty directories; keep `emacs.d/modules.el` (emacs.md §4); one `restore-editor` (the step's start, then unsaved text with `replace-buffer-contents` and undo with recording off, point from the spec, then globals; each buffer and part on its own, the failed ones listed in `degraded` and logged); write the manifest's spec to `snapshot.json`. `restoredFiles` counts files changed on disk plus buffers whose unsaved edits were discarded. Failure classes: `ErrFiles` (git failed: 500, nothing activates) and `ErrEditor` (the agent is connected but `restore-editor` failed or the agent left during it: 500, the files are back, nothing activates, Reset works again). Whether the agent is there is decided before the git phase. Without it, or when it leaves during `restore-editor`, the files are restored and the restore reports `EditorDeferred`; the op goes on (a Reset activates) and the core keeps the ref for the attempt current after it. The agent's next hello queues the core's `reconnect` op, which runs after the op in progress (so a hello during the restore or before its step activated is not missed) and applies the manifest's editor state (`RestoreEditor`) only while that attempt is current: a later activation or restore makes it moot, so a later load never gets an earlier lesson's buffers.

`snapshot.json` is the one editor state every recovery path reopens: the core's `autosave` op writes the spec every 30 s when no op runs or waits, `BeforeStop` before a restart or shutdown, and every restore writes the checkpoint's spec; a restarted daemon (`AfterRestart`) and the `resume` op reopen it. The service keeps no recovery state and no lock: captures, restores and autosaves run only in core ops, one at a time, and manifests and `snapshot.json` are written through a temporary file of their own, then renamed.

Every restore entry point (Reset, Do it yourself, Watch it and Skip after a command, Go to this step, `restore` by label or ref) runs inside an op of the session core (§3.12), so files, Emacs restoration and the following step activation never interleave with another restore or load. External file buffers stay untouched. A started restore runs to the end whatever the request or a drop does. Git commands are serialised, detached and bounded by `checkpoint.GitTimeout`; a lock older than `checkpoint.StaleLockAge` (10 s) that blocks a command is removed and the command retried once.

Not captured: the staging area of `/work` itself (nested repositories keep theirs), minor modes a demo turned on, variables outside `GlobalVars` such as `display-buffer-alist`. `/work` being the checkpoint repository makes project.el treat it as a project (`emacs.d` in `project-files`); hiding it needs a separate `GIT_DIR`.

### 3.10 Preview and file watching

`/preview/` serves `/work` through `os.Root` (no `..` or symlink escape; `.playground/` hidden), headers in api.md §3.5. `fsnotify` watches `/work` recursively (ignoring `.git`, build output, `.playground`, lock and backup files), coalescing per path at 200 ms; a change at the top of a nested repository's `.git` (commit, staging, checkout) emits no event but drops cached shell results.

### 3.11 VNC bridge

`/vnc` copies bytes between the WebSocket and Xvnc on `127.0.0.1:5901`, feeding client bytes to the RFB decoder. sessiond is the only way to Xvnc.

### 3.12 Session core and scenario load

`session.Core` owns lesson orchestration. `scenario`, `restore`, `modules`, `drop-step` and the startup resume are ops that one worker runs one at a time, each detached from its request (a request that goes away changes nothing) and bounded from its start: load 3 min (fast-forward included), restore 2 min, resume 1 min, module change 3 min, labelled checkpoint 30 s, drop 10 s. An op checks its context between phases; once the context ends nothing activates. `drop-step` jumps the queue and, when its generation is newer than the one applied, cancels the running op when it concerns the same lesson (any lesson for `{}`), so a dropped lesson's fast-forward stops at once and loads nothing. While an op runs, `status` and `hello` carry `busy` (api.md §3.4). `demo start` (30 s), `skip` (60 s) and the `advance` after a completion (1 min, queued first) are ops too (§3.8); `demo start` and `skip` carry the attempt they were admitted for and answer 409 when a load or advance queued before them replaced it (another lesson's step may have the same id). So is recovery: the generation sync (§3.8), `restart-emacs`, the `reconnect` and `recover` editor ops (§3.9) and the autosave (only when nothing runs or waits), so none interleaves with a restore or capture. Every daemon restart sessiond asks for runs in an op, so `AfterRestart` reopens inline there; waiting for the queue from the worker would deadlock.

The load op, `rpc scenario {lessonId, stepId?, reset?, lap?}`:

1. Reloading the active lesson (same or no `stepId`, no `reset`) returns at once, never re-running setup.
2. Fetch scenario and fixtures, cached by ETag in `.playground/cache` and used from there when the gateway is down.
3. Extract fixtures into `/work/<setup.root>`, keeping existing files unless `reset` (request, `setup.reset` or `lap`), which makes the root match the fixtures exactly (except build output and `.git`). Then `setup.repos` (`git init`, re-created on reset) and `setup.run` (≤ 120 s); failures are `error` events.
4. Modules = current ∪ scenario's (emacs.md §4). On change: write `modules.el`, emit `modules {restarting: true}`, answer the request early with `restarting: true`, restart the daemon (≤ 3 min; once `modules.el` is written the restart runs to the end whatever a drop or the deadline does, so modified buffers are saved first) and go on; `modules {restarting: false}` follows the activation (the browser holds keys until then, so none land before setup). Requests arriving meanwhile queue behind the op and get final results.
5. Agent `setup` (emacs.md §10.3), after waiting ≤ 15 s for a restart and for the terminal frame, so layouts never land in the daemon's invisible frame. `compile` runs in the project root of the first `open` file. TODO(sessiond, CURRICULUM.md Part B.3): `.eld` layouts are ignored.
6. A later step (Go to this step, resume mid-lesson) starts from its checkpoint; without one the op fast-forwards, rehearsing each earlier step and playing its demo (≤ 10 s per step, 3 min total, leaving 20 s of the deadline for the checkpoint and activation; with less left it does not start). The step's checkpoint is taken only when every earlier step was replayed to completion; otherwise the step activates without one. A later step's checkpoint is never overwritten with the lesson's initial state.
7. Take the step's checkpoint if missing or fixtures were re-extracted, activate the step, emit `step loaded`, return.

### 3.13 Configuration and proxy auth

Environment only: `protocol.SessiondConfig` (names and defaults in its tags), each overridable by a flag named after its JSON key; `sessiond config` prints it, `sessiond healthcheck` is the Compose health command.

`sessiond.ProxyAuth`: with `PG_PROXY_SECRET` set, every request except `GET`/`HEAD /healthz` needs it in `X-Playground-Proxy` (constant-time), else 401; the header is stripped. Emacs, tmux and shell conditions see `PG_PROXY_SECRET` and `PG_SESSION_TOKEN` empty. Unset (local mode, `make dev`), everything is served.

## 4. playctl

- `curriculum validate [dir]`; `curriculum build [dir] -o out [-lenient]` (`-lenient` drops broken lessons, test builds only).
- `curriculum play <lesson id|2.3|P1|part:N|all>[@step]… [-session URL] [-timeout] [-speed] [-watch|-lap] [-repeat N] [-reset-progress]`: loads each lesson by `/rpc/scenario`, plays its demos by `/rpc/keys`, requires every step to complete, prints a table and the unmet conditions, exits non-zero on failure. `-session` is sessiond (default `$PG_PLAY_SESSION`) or a gateway session path (played lessons' progress is then reset). `-watch` plays each step first as Watch it and expects `step demonstrated` before `restore {}`; `@step` (id or task number) starts at that step; `-lap` expects a refused hint and the final `lapMs`.
- `config schema`, `config check <file>`; `protocol typescript [-check]` (api.md §8); `images build`, `dev`, `fonts`, `session shell` (notes.md §3); `export-user` is a TODO.

## 5. Cross-cutting

- Config: unknown keys are errors; logged with secrets redacted; the struct generates `deploy/config.schema.json`.
- Logging: `log/slog` JSON with `request_id`, `session_id`, `user_id`; never cookies, tokens or workspace contents.
- Metrics (optional): sessions, WebSockets, events, RPC, evaluation, checkpoint and proxy durations.
- Errors: handlers return `httpx` errors, rendered as problem+json with api.md §1's codes.
- Every subprocess has a context, every call a deadline; HTTP read-header 10 s, idle 120 s; WebSocket pings every 20 s; every buffer is bounded.
- Shutdown: sessiond stops the session core (the running op ends at its next phase, later ones answer 503), emits `emacs stopping`, runs `BeforeStop` (`snapshot.json`, `flush-buffers`), stops Emacs (≤ 15 s), closes WebSockets with 4003, lets the progress outbox deliver for ≤ 5 s; the gateway drains 10 s.

## 6. Testing

`go test ./...` needs no Docker, Emacs or tmux (subprocesses and the Engine are faked; sessiond's fake editor is validated against fixtures recorded from the agent, its lifecycle traces pin the order of events and progress reports, and `TestKilledDuringRestore` kills a fake-mode sessiond process with SIGKILL mid-restore and checks that the next one converges, notes.md §3); the `dockerintegration` and `integration` tags, `server-mode.sh`, ERT (emacs.md §10.6) and the browser scripts run against real images (notes.md §3).

## 7. Conventions

`gofmt`, `go vet`. Context first. No globals but the slog default. Wrap with `%w`, inspect with `errors.Is/As`. `doc.go` per package. Generated files carry a header and a drift check.
