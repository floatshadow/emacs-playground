# AGENTS.md: how to build, test and run Emacs Playground

The operating manual for anyone (human or agent) working in this repository. The specification is in `docs/` (start with `docs/DESIGN.md`); this file covers the development environment, the commands, who owns what, and the decisions taken while implementing that the docs do not spell out.

## 1. The rule

**Component agents never run `go mod tidy` and never edit files outside their area; integration does.**

If you need a new Go module, an npm package, a protocol type, a config key, a Makefile target or a Compose change, say so in your report (what, why, exact version) and let integration apply it. Every Go module from `docs/third-party.md` §5 is already in `go.mod` (pinned by `internal/deps/deps.go`) and every frontend package from §6 is already in `web/package.json`; you should rarely need anything else.

## 2. Ownership areas

| Area | Owner | Paths |
|---|---|---|
| Shared contracts | integration | `internal/protocol/**` (including `tsgen/`), `internal/config/**`, `internal/httpx/**`, `internal/deps/**`, `internal/buildinfo/**`, `go.mod`, `go.sum`, `web/embed*.go`, `web/src/protocol.gen.ts` (generated), `Makefile`, `compose*.yaml`, `.env.example`, `.gitignore`, `.dockerignore`, `deploy/**`, `AGENTS.md`, `README.md`, `docs/**` |
| Gateway | gateway agent | `cmd/gateway/**`, `internal/gateway/**`, `internal/docker/**`, `images/gateway/**` |
| sessiond | sessiond agent | `cmd/sessiond/**`, `internal/sessiond/**` |
| playctl and curriculum loader | playctl agent | `cmd/playctl/**` (keep the existing `protocol typescript` and `config check` subcommands), `internal/curriculum/**` |
| Web app | frontend agent | `web/**` except the integration files above (`web/package.json` and `web/pnpm-lock.yaml` change only through integration) |
| Emacs side | Emacs agent | `images/session/**`, `images/remote-box/**`, `emacs.d/**` |
| Curriculum content | curriculum agent | `curriculum/**` |

Packages under `internal/gateway` and `internal/sessiond` must not import each other (backend.md §1). Both may import `internal/protocol`, `internal/config`, `internal/httpx` and `internal/curriculum`.

TODO markers use `TODO(<area>, <doc section>)`, for example `TODO(sessiond, backend.md §3.6)`. Leave one wherever you stop short of the spec.

## 3. Development environment

- Required on the machine: Git, Docker Engine 27 or later with Compose v2, Go 1.26 (`go.mod` pins the minor), Node 24 with corepack (pnpm 11.28.8 is pinned in `web/package.json` `packageManager`), python3 with PyYAML for `curriculum/tools/validate.py`. Emacs and tmux are not needed on the host: everything that needs them runs inside the session image.
- `make` adds `~/.local/go/bin`, `~/go/bin` and `~/.local/bin` to `PATH`, so a Go installed under `~/.local/go` and a pnpm shim installed with `corepack enable --install-directory ~/.local/bin pnpm` work without further setup. Run pnpm from inside `web/` so corepack resolves the pinned version.
- Commits use the identity configured in the repository (`git config user.name` and `user.email`); never pass `-c user.*` flags or personal details.
- Containers run as uid 1000 (`learner`). When your uid differs, a directory a container writes to through a bind mount must be world-writable; `make local`, `make dev` and `make reset-workspace` do this for `./workspace`.
- If your Docker daemon runs elsewhere (a remote host or a VM), bind mounts and build contexts must be on a path the daemon can see, `/tmp` is usually not shared, and published ports are where the daemon is. Keep such machine-specific notes in `AGENTS.local.md` (git-ignored), read after this file; never put local paths or account details in tracked files.
- Network access needed for builds: GitHub, go.dev, npm, GNU and NonGNU ELPA, MELPA, Docker Hub, GHCR.

### Toolchain installation (fresh machine)

```sh
# Go 1.26.x into ~/.local/go (take the sha256 from https://go.dev/dl/?mode=json)
mkdir -p ~/.cache/go-dl && cd ~/.cache/go-dl
curl -sSLO https://go.dev/dl/go1.26.10.linux-amd64.tar.gz
echo "ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5  go1.26.10.linux-amd64.tar.gz" | sha256sum -c -
tar -C ~/.local -xzf go1.26.10.linux-amd64.tar.gz

# pnpm through corepack: a shim in ~/.local/bin, default version 11.28.8
corepack enable --install-directory ~/.local/bin pnpm
corepack install -g pnpm@11.28.8
```

pnpm 11 enforces a minimum release age for new versions; when it adds entries to `web/pnpm-workspace.yaml` (`minimumReleaseAgeExclude`), commit them with the lockfile.

## 4. Commands

`make help` lists every target. The ones you will use:

| Command | What it does |
|---|---|
| `make check` | everything CI runs: `go build ./...`, gofmt, `go vet ./...`, protocol drift check, `go test ./...`, eslint, prettier, `tsc -b`, vitest, `vite build` |
| `make test` | `go test ./...` and `cd web && pnpm test` |
| `make lint` | gofmt, go vet, protocol drift, eslint, prettier check, typecheck |
| `make protocol` | regenerate `web/src/protocol.gen.ts` from `internal/protocol` (integration only) |
| `make build` | Go binaries into `bin/` and the web bundle into `web/dist` |
| `make gateway` | `web/dist`, then `bin/gateway` with the bundle embedded (`-tags embedweb`) |
| `make local` / `make server` | write `.env` for the deployment mode (deployment.md §2); `BUILD=1 make local` builds images instead of pulling |
| `make images`, `make image-gateway`, `make image-session` | build images |
| `make dev` | session container on the host network, gateway from source, Vite on :5173 |
| `make e2e` | browser smoke scenarios against the running local stack (`web/e2e/smoke.mjs`, needs Chrome on CDP :9222) |
| `make curriculum-play` | replay every ready lesson's demos twice against the running stack (`playctl curriculum play all -repeat 2`; progress reset afterwards) |
| `make curriculum-watch` | replay Part 0 (`PART=…`) as "Watch it" demonstrations, then as the learner (`playctl curriculum play -watch`) |
| `make emacs-test` | the agent's and the config's ERT tests inside the session image |
| `make fonts` | web fonts for `make dev` into `.dev/fonts` (the gateway image's fonts stage) |
| `make shell` | attach your terminal to the local session's Emacs (`tmux -L pg attach -t emacs`) |
| `make reset-workspace` | move `./workspace` aside and start fresh |

### Go (module `github.com/floatshadow/emacs-playground`, Go 1.26)

```sh
go build ./...                     # web/ builds without web/dist thanks to the embedweb build tag
go vet ./...
go test ./...                      # unit tests; no Docker, Emacs or tmux needed
go test ./internal/protocol/...    # includes the protocol.gen.ts drift test
gofmt -l cmd internal web/*.go     # must print nothing
```

`go.mod` has `ignore ./web/node_modules` so `./...` never descends into npm packages.

### Gateway

```sh
go run ./cmd/gateway config -config deploy/config.json          # effective config, secrets redacted
go run ./cmd/gateway -config deploy/config.dev.json             # run (local mode, static backend)
go run ./cmd/gateway -config deploy/config.json -set sessions.limits.memory=4g -listen :9000
go run ./cmd/gateway -config deploy/config.dev.json -web web/dist -fonts .dev/fonts   # bundle and fonts from disk (PG_WEB_DIR, PG_FONTS_DIR)
go test -tags dockerintegration ./internal/docker/                   # opt-in: the docker backend against the real Engine
images/gateway/test/server-mode.sh ghcr.io/floatshadow/emacs-playground/gateway:dev   # server-mode smoke test (from the repo root)
```

Stores: local mode keeps `<dataDir>/local.json`, server mode `<dataDir>/playground.db` (SQLite). In local mode the loopback names are interchangeable: a `publicUrl` of `http://localhost:8080` also accepts the Origin `http://127.0.0.1:8080` (and the CSP allows WebSockets to both).

Configuration precedence: defaults < JSON file < `PG_*` environment < flags (`internal/config`). The file is `-config`, else `$PG_CONFIG`, else `/etc/playground/config.json` if it exists. Environment names are derived from the JSON path with camelCase split: `publicUrl` → `PG_PUBLIC_URL`, `sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`, `sessions.idleStopMinutes` → `PG_SESSIONS_IDLE_STOP_MINUTES`; the unsplit form and the short aliases in `config.Aliases` (`PG_IDLE_STOP_MINUTES`, `PG_MAX_SESSIONS`, `PG_SESSION_URL`, …) also work. Unknown keys in the file are errors. `go run ./cmd/playctl config check <file>` validates a file.

`deploy/config.json` is the minimal local file; `deploy/config.dev.json` is for `make dev` (gateway on `127.0.0.1:8080`, `publicUrl` `http://localhost:5173` so the Vite-proxied Origin passes, data and curriculum under `.dev/`, author mode on); `deploy/config.example.json` is the complete server example.

### sessiond

Configured only by environment (`protocol.SessiondConfig`, defaults in its `default` tags): `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_GATEWAY_URL`, `PG_LISTEN` (`:7000`), `PG_WORKSPACE` (`/work`), `PG_RUN_DIR`, `PG_AGENT_SOCKET`, `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`, `PG_PRISTINE_CONFIG`, `PG_EMACS_SERVER`, `PG_TMUX_SOCKET`, `PG_DISPLAY`, `PG_VNC_PORT`, `PG_GUI_ENABLED`, `PG_*_BIN` (binaries, overridable for tests), `PG_VERSIONS_FILE`, `PG_LOG_LEVEL`, `PG_LOG_FORMAT`, `PG_AUTHOR_MODE`.

```sh
PG_WORKSPACE=$HOME/pg-work PG_RUN_DIR=$HOME/pg-run go run ./cmd/sessiond config
```

The host has no Emacs or tmux: anything that needs them runs inside the session image (`docker run` with the repository bind-mounted, or `make dev`). Unit tests must fake subprocesses behind interfaces (backend.md §1).

Real-process integration test (sessiond against the session image, on the host network):

```sh
docker run -d --name pg-it --network host --tmpfs /run:mode=1777 --tmpfs /tmp:mode=1777 --tmpfs /work:mode=1777,exec \
  -e PG_LISTEN=127.0.0.1:17200 -e PG_GATEWAY_URL=http://127.0.0.1:17180 -e PG_AUTHOR_MODE=true \
  ghcr.io/floatshadow/emacs-playground/session:dev
PG_SESSIOND_URL=http://127.0.0.1:17200 PG_TEST_FAKE_GATEWAY=127.0.0.1:17180 PG_TEST_GUI=1 \
  go test -tags integration ./internal/sessiond/ -run Integration -v
docker rm -f pg-it
```

Without `PG_SESSIOND_URL` the test starts `PG_SESSION_IMAGE` through testcontainers but skips the lesson and GUI parts (a bridge-networked container cannot reach the in-test gateway).

Fast iteration on the running stack without rebuilding images: build `CGO_ENABLED=0` binaries into a directory the Docker daemon can see, then add a Compose overlay that bind-mounts them over `/gateway` and `/usr/local/bin/sessiond` (and `web/dist` with `PG_WEB_DIR`, a built curriculum over `/srv/curriculum`), and `docker compose -f compose.yaml -f compose.local.yaml -f <overlay> up -d --force-recreate`. The Emacs configuration lives in the image (`/opt/playground/emacs.d`, agent from there) and is copied to `./workspace/emacs.d` on the first start only: after changing `emacs.d/modules`, rebuild the session image (about 15 s when only the config changed) and `make reset-workspace`.

### playctl

```sh
go run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts [-check]
go run ./cmd/playctl config check deploy/config.json
go run ./cmd/playctl curriculum validate curriculum
go run ./cmd/playctl curriculum build curriculum -o artifacts/curriculum [-lenient]   # -lenient drops broken lessons (test builds only)
go run ./cmd/playctl curriculum play <lesson id | 2.3 | part:N | part dir | all> -session http://127.0.0.1:8080/s/local [-timeout 30s] [-speed 1] [-watch] [-repeat N] [-reset-progress=false]
go run ./cmd/playctl fonts -o .dev/fonts
python3 curriculum/tools/validate.py          # the curriculum agent's extra content checks (PyYAML)
```

`curriculum play` loads each lesson through `/rpc/scenario`, plays every step's demo through `/rpc/keys` with playctl's pacing (170 ms per chord, 70 ms per character) and requires each step to complete; it prints a table, lists the failed steps with their unmet conditions again, and exits non-zero on failures. With a gateway session path (`…/s/<id>`) it resets each played lesson's progress afterwards (`-reset-progress=false` keeps it), so replays do not pose as the learner's progress. `-watch` first plays each step as the browser's "Watch it" (`demo start`, the demo, `demo end`), requires a `step demonstrated` event on `/events` with the step still active, resets it with `restore {}` and then plays the keys as the learner; `-repeat N` plays every lesson N times in a row. The session URL is sessiond itself (`http://127.0.0.1:7000` under `make dev`) or the gateway's session path. Run it on a fresh workspace (`make reset-workspace`) for results that match a learner going through the course in order: configuration modules unlock monotonically (emacs.md §4), so a lesson replayed after later parts runs with more modules than its scenario lists.

### Web (`web/`)

```sh
cd web
pnpm install --frozen-lockfile
pnpm dev            # Vite on :5173, proxies /api, /s (WebSockets too), /fonts, /internal to $PG_GATEWAY or http://127.0.0.1:8080
pnpm build          # tsc -b && vite build → web/dist
pnpm test           # vitest (jsdom)
pnpm lint           # eslint (typescript-eslint, react-hooks, jsx-a11y)
pnpm format         # prettier --write; format:check in CI
pnpm typecheck
```

Never edit `src/protocol.gen.ts`; `make protocol` regenerates it and `go test ./internal/protocol/...` fails when it is stale.

### Browser testing (no browser on the host)

```sh
docker run -d --rm --name pg-chrome --network host --shm-size=1g zenika/alpine-chrome:latest \
  --headless --no-sandbox --disable-gpu --remote-debugging-address=0.0.0.0 \
  --remote-debugging-port=9222 about:blank
cd web && pnpm e2e:smoke            # PG_URL defaults to http://127.0.0.1:8080
docker rm -f pg-chrome
```

`web/e2e/review.mjs` is the acceptance test for the newcomer-review fixes (docs/feedback/2026-10-27-fix-plan.md): one check per review item (B1..B18, L1, X3, S1..S5, R1, R9, R10), each in a fresh browser context against the running local stack. `cd web && node e2e/review.mjs` runs all of them (about ten minutes), `PG_REVIEW_ONLY=B1,B4 node e2e/review.mjs` a subset; it writes `artifacts/review/<id>.png` and `results.json` and resets all progress when it ends.

Give the Chrome container `--shm-size=1g`: with Docker's default 64 MB `/dev/shm`, the terminal's WebGL renderer and the VNC canvas exhaust shared memory after a few page loads and Chrome fails later loads with `net::ERR_INSUFFICIENT_RESOURCES` (a blank page).

`web/e2e/smoke.mjs` (`pnpm e2e:smoke`, `make e2e`) connects with `playwright-core` (`chromium.connectOverCDP('http://127.0.0.1:9222')`) and runs the ten integration scenarios against the compose stack (services healthy, course map, lesson 2.3 with the ticker, Where am I, Watch it and Reset, the GUI frame, the Markdown preview of lesson 3.8, `C-x C-b` and Rescue in lesson 0.2, and `playctl curriculum play` for Parts 0-3), writing `artifacts/verify/<n>.png` and `results.json`. Besides the DOM it checks the `/events` frames the page receives (the step's `check`/`step` complete event, `frame` of a key typed in the GUI) and the workspace files on disk (`PG_WORKSPACE`, default `./workspace`). The `C-x C-b` scenario (6) sets Part 0's modules (`base agent`) and runs after scenario 9: from Part 2 on, the files module makes `C-x C-b` ibuffer, which takes over the selected window on purpose (lessons 2.1, 2.3, 3.4), so the second-window exit and the Rescue that closes it are a Parts 0-1 situation. `PG_SMOKE_ONLY=3,4` runs a subset, `PG_SMOKE_PLAY=0` skips the replay. It resets the progress of lessons 0.2, 2.3 and 3.8 first, so it can be re-run, also after a replay (setup with `reset` forgets save-place positions under the lesson root). The canvas-rendered terminal is read through `window.__pgScreenText()` (web/src/components/screen/hosts.ts). `web/e2e/lesson.mjs` (`pnpm e2e:lesson`) is the same kind of walk against the fake session (web/dev/README.md). Wait for `load` plus a selector; `networkidle` hangs with this Chrome. Write screenshots under the repository (`artifacts/`, git-ignored), not `/tmp`, which a remote daemon does not share. To serve a built bundle without the gateway: `cd web && pnpm preview --port 8080 --host 127.0.0.1 --strictPort`.

### Images and Compose

```sh
docker build -f images/gateway/Dockerfile -t ghcr.io/floatshadow/emacs-playground/gateway:dev .
docker build -f images/session/Dockerfile -t ghcr.io/floatshadow/emacs-playground/session:dev .
docker compose -f compose.yaml -f compose.local.yaml config -q         # validate
make local && docker compose up                                          # http://localhost:8080
docker compose -f compose.yaml -f compose.local.yaml -f compose.dev.yaml up -d session   # dev
```

Build contexts are the repository root; `.dockerignore` keeps `node_modules`, `web/dist`, `workspace` and `.git` out. The gateway image builds the web bundle (node stage, pnpm 11 through corepack), the Go binaries (`-tags embedweb`), the curriculum bundle (`playctl curriculum build`, strict; the build arg `CURRICULUM_FLAGS=-lenient` is for test builds only) and the web fonts (Iosevka Nerd Font, Source Serif 4). The session image (emacs.md §1) builds Emacs 31.1 from source with native compilation, the Rust and Python toolchains, the pinned Emacs packages (`emacs.d/packages.lock`, MELPA tarballs vendored in `emacs.d/vendor/`) and sessiond; it is about 3.4 GB, a cold build takes about 9 minutes and a config-only change about 15 seconds. The remote box starts with the local stack (alias `box` on the sessions network); logins need the per-session key, which is milestone M6.

Running the Emacs side's tests (also `make emacs-test`):

```sh
docker run --rm --entrypoint sh ghcr.io/floatshadow/emacs-playground/session:dev -c 'cd /opt/playground/emacs.d && emacs --batch -L lisp -l playground-agent -l test/agent-test.el -f ert-run-tests-batch-and-exit'
docker run --rm --entrypoint sh ghcr.io/floatshadow/emacs-playground/session:dev -c 'cd /opt/playground/emacs.d && emacs --batch -l early-init.el -l test/config-test.el -f ert-run-tests-batch-and-exit'
```

Wrap `docker run`/`docker exec` commands that carry inline shell code in a script file when a sandbox or worktree guard refuses them.

## 5. Decisions taken while scaffolding

These refine the docs; the code is authoritative where they differ.

- Module path `github.com/floatshadow/emacs-playground`; images `ghcr.io/floatshadow/emacs-playground/{gateway,session,remote-box}`.
- Versions: Go 1.26.10; React 19.3.1; Vite 8.3.5; TypeScript 5.9.3 (typescript-eslint 8.72 supports TypeScript below 6.1, so TypeScript 7 is out); ESLint 9.39.5 (eslint-plugin-jsx-a11y 6.10 does not declare ESLint 10); React Router 7.18.4 (the docs' major, though 8 exists); pdfjs-dist 5.7.284 (docs' major); noVNC 1.7.0; Vitest 5.0.5; xterm.js betas from one upstream commit (991857a, 2026-10-13): `@xterm/xterm` 6.1.0-beta.315 (has `vtExtensions.kittyKeyboard`), addon-fit 0.12.0-beta.311, addon-webgl 0.20.0-beta.309, addon-unicode11 0.10.0-beta.310, addon-clipboard 0.3.0-beta.313, addon-web-links 0.13.0-beta.310. Re-pin all six together.
- TypeScript types are generated directly from Go by `internal/protocol/tsgen` (reflection for structure, the Go AST for enum constants and doc comments), not through invopop/jsonschema and json-schema-to-typescript as api.md §8 sketched. It yields string-literal enums, an `Event` union discriminated by `t`, and an `RpcOps` map of request and result types per op. invopop/jsonschema remains the tool for `deploy/config.schema.json`.
- Lesson ids are the lesson directory relative to `curriculum/`: `02-buffers-and-files/03-opening-files`, `projects/p6-polyglot`. They contain one slash, which goes into URLs literally (`/api/v1/curriculum/lessons/02-buffers-and-files/03-opening-files`); routes use `{lessonId...}`-style trailing wildcards.
- Condition ids default to `<stepId>/<n>` with **1-based** `n` (`protocol.ConditionID`); stuck rules to `<stepId>/stuck/<n>`. `Scenario.AssignIDs` fills them in; the gateway serves scenarios with ids assigned.
- A condition result has `unknown: true` (with `ok: false`) when it could not be evaluated. The agent's `check` op returns `true`, `false` or `"unknown"` per form (`protocol.Truth`).
- `vsplit` stacks windows top to bottom (C-x 2, Emacs "vertical combination"); `hsplit` is side by side (C-x 3). The state's `layout` is a number for one window or `["vsplit", child, …]`. `protocol.ParseLayoutPattern` and `protocol.MatchLayout` implement layout conditions (`*` matches any window, names compare by uniquify base name, same-orientation nesting is flattened).
- Exit-table predicates (`when`) and state conditions use `protocol.Predicate`: dotted paths, scalars for equality, lists for membership, or one operator object (`in`, `not`, `gt`, `gte`, `lt`, `lte`, `exists`, `contains`, `regex`); paths over lists match any element; `windows.length`; virtual roots `otherWindows` and `selectedWindow`. CURRICULUM.md's `{ otherWindows.mode in [...] }` is written `{ "otherWindows.mode": { in: [...] } }`. `protocol.ExpandTemplate` fills `{path}` placeholders. The Go functions are the reference for the TypeScript port in `web/src/advice/exits.ts`.
- Localisable strings (`title`, `goal`, `detail`, `text`, `hint`, `say`) are `protocol.LocalText`: a plain string or a locale map; it marshals back to a plain string when only `en` is present.
- Demo strings (`protocol.ParseDemo`): whitespace-separated keys in Emacs notation, `"typed text"` with `\"`, `\\`, `\n`, `\t` escapes, `~N` pauses. Bare `F1`…`F35` are accepted and normalised to `<f1>`. Pacing constants `DemoChordDelayMs` 170 and `DemoCharDelayMs` 70.
- Extra RPC ops beyond api.md §3.4: `describe` (ticker glosses for unknown commands, frontend.md §6) and `drop-step` (internal, sent by the gateway after `POST /progress/reset`). `keys` accepts `text` (typed literally) as well as `keys`. Extra agent op: `setup` (runs a scenario's open, layout, eval and compile in one call).
- Extra events and fields: `emacs` events `responsive` and `restarting`; `step` events carry `index`, `count` and `lessonComplete`; `key` events may carry `mx: true`; `prefix` with `keys: ""` clears a pending prefix.
- Extra gateway endpoint: `GET /api/v1/auth/config` (unauthenticated; tells the login screen which auth mode is active).
- Extra config keys: `local.sessionId` and `local.sessionToken` (default `local`, matching `PG_SESSION_ID` and `PG_SESSION_TOKEN` in `compose.local.yaml`), so the static backend can authenticate `/internal` calls. `sessions.backend` defaults to `static` in local mode and `docker` in server mode; `auth.cookieSecretFile` defaults to `<dataDir>/cookie.secret`.
- `ProgressReport.hintsUsed` and `resets` are absolute totals for the step, not increments.
- The web bundle is embedded only with `-tags embedweb` (`web/embed.go`); without the tag `web.Dist()` reports no bundle, so `go build ./...` never needs `web/dist`.
- In `compose.local.yaml` the remote box is behind the `remote` profile until the image exists; the session gets `cap_drop: ALL`, `no-new-privileges` and tmpfs `/tmp` and `/run`.
- OIDC: the gateway answers its routes with `501 not_implemented` in slice 1 (`protocol.ErrNotImplemented`). The docker backend is experimental in slice 1.

### Decisions taken during integration

- Exit-table rows may also use the virtual root `session` (`protocol.ExitSession`): `session.prefix` (pending prefix from the tap), `session.emacs` (`frame-closed`, `suspended`, `restarting`, `unresponsive` or null), `session.mode` and `session.echo` (latest echo text until the next key). Only the browser evaluates it (`web/src/advice/exits.ts`); step conditions cannot use it. When the table has `session.*` rows they replace the web's built-in live rows for the same facts.
- Extra RPC op `open {path}` → `{buffer}`: visits a workspace-relative file in the learner's window. The project file tree uses it; `eval` stays author-mode only (in sessiond and in the gateway proxy).
- The rpc `stuck` op files a report through `/internal` only when the request carries `X-Forwarded-For` or `Forwarded` (a browser call through the gateway proxy); the gateway's own `POST /api/v1/stuck` stores the events itself. The gateway also merges identical reports within 30 s.
- Agent ops never prompt: they run with `inhibit-interaction`, so a command that would ask a question fails with an error reply instead of blocking the agent. `setup` first reverts (without asking) the buffers whose files changed on disk and kills those whose files are gone, because sessiond has just extracted the fixtures.
- Agent replies beyond `protocol/agent.go`, additive: `open` → `{opened, layoutDeferred?}` (`layoutDeferred` when no client frame exists yet, right after a daemon restart; the layout goes to the next client frame), `reset-step` → `{reverted, killed}`, `set-modules` → `{written}`, `frame` → `{frame}`, `setup` → `{ok}`; `describe` also accepts `{cmd}`; `eval` turns alists with symbol keys into JSON objects.
- tmux: the image's `tmux.conf` sets `remain-on-exit on` and a global `pane-died` hook (backend.md §3.3 said `off`; `respawn-pane` needs a dead pane). sessiond's session-level `pane-died` hook overrides the global one, so only sessiond respawns. The pane command is `emacsclient -t -s playground`.
- Extended keys in the browser: tmux never forwards Emacs's kitty-keyboard push to the outer terminal, so xterm.js stays in the legacy encoding. The terminal pane sends CSI u itself for exactly the chords the legacy encoding cannot carry (C-;, C-=, C-?, C->, C-1, C-S-a, C-RET, S-RET, C-DEL; `web/src/components/screen/extkeys.ts`); tmux (`extended-keys always`, `csi-u`) passes them to Emacs and the input tap decodes them.
- Git run by sessiond (checkpoints, shell conditions, supervised processes) gets `safe.directory=*` through `GIT_CONFIG_COUNT`, since a bind-mounted `/work` belongs to the host uid.
- Leaving GUI mode deletes the GUI frame before Xvnc stops; `/vnc` closes with 4002 when switching to the terminal and 4001 (reconnect) when the display exits while GUI mode is on. Missed heartbeats emit `emacs unresponsive` and SIGUSR2 to Emacs; when Rescue's C-g does not help within 10 s an `error` event with code `emacs_unavailable` offers a restart.
- `base` never lets `auto-save-visited-mode` save over a file that changed on disk (`auto-save-visited-predicate`), which would otherwise ask "Save anyway?" from a timer after a lesson reset.
- Configuration modules unlock monotonically (emacs.md §4): a scenario's `modules` are added, never removed (`rpc modules` on the session page can still set any set). Lessons must pass with more modules than they list: lesson 0.2's buffer-list step accepts both the plain buffer list and ibuffer, and the windows module closes a project's own tab after `C-x p k` (so 2.7 lands back in hello with project tabs on). Replaying Part 3 first and then Parts 0-2 passes. The smoke test still resets the modules to `base agent` before replaying Parts 0-3, as a learner going through the course in order would have them.
- The agent reports a frame `closed` only when the last frame of its terminal goes away, so `C-x 5 2` then `C-x 5 0` in the terminal (lesson 3.1) does not mark the terminal frame closed. (Until 2026-10-27 an unescaped quote in that predicate's docstring made it signal `void-variable` every time, so no `closed` was ever reported from `delete-frame-functions`; compile the agent with `byte-compile-error-on-warn` after editing docstrings.)
- A C-g that Emacs reads while redisplay runs Lisp (typically the refontification `jit-lock-context-time`, 0.5 s after an edit) quits that Lisp and is lost: no `keyboard-quit`, the region stays active, *Messages* says "Error during redisplay: (jit-lock-function N) signaled (quit)". The quit is raised on entry to the Lisp, so advice cannot catch it. Demos that press C-g right after an edit should pause about a second first (lesson 1.3's redo step lost its C-g about one replay in ten).
- `setup.compile` runs in the project root (project.el) of the first `setup.open` file, or that file's directory.
- `setup.reset: true` restores the lesson root to its fixtures: sessiond overwrites edited files and removes files and directories the fixtures do not contain (keeping `target`, `_build`, `.venv`, `node_modules`, `.git`), and the agent's `setup` (with `reset`) reverts the affected buffers, kills buffers of removed files, closes other tabs and clears unmodified leftovers (other files under the workspace, Dired and buffer lists with stale marks, empty stray buffers made by `C-x b`). Modified buffers outside the root are kept. Lessons are then replayable in any order, and "Go to this step" starts from the lesson's initial state.
- A lesson setup waits (up to 15 s) for a restart in progress and for the terminal frame, so layouts never land in the daemon's invisible initial frame; a restart reports `daemon-restarted` once the new terminal frame is open.
- OSC 52 clipboard writes from Emacs (kills through tmux `set-clipboard on`) go to the browser clipboard when the page has focus; refusals are ignored.
- In local mode the remote box starts with the stack (no Compose profile), so `docker compose ps` shows gateway, session and remote-box; it has no authorised key until M6.
- HTTP access logs: successful polling requests (`/healthz`, `/metrics`, `/status`, `/rpc/check`, heartbeats) are logged at debug level.

### Decisions taken in the review fixes (2026-10-27)

- sessiond authenticates its gateway: with `PG_PROXY_SECRET` set, every request but `GET`/`HEAD /healthz` must carry it in `X-Playground-Proxy` (`protocol.HeaderProxySecret`), else 401 (`sessiond.ProxyAuth`). The gateway derives the secret per session (`sessions.ProxySecret`: HMAC-SHA256 of the session id with the key in `<dataDir>/session-proxy.secret`), passes it to docker-backend containers and sends it from the proxy, `Sessions.Call` and `Sessions.Status`. The static (local) session has no secret and sessiond serves without one, so `make dev`, `playctl curriculum play -session http://127.0.0.1:7000` and the sessiond integration test work unchanged.
- The docker backend gives each session its own internal network `pg-net-<id>` (`sessions.isolation: network`, the default; `/28` subnets from `sessions.subnetPool`, default `10.231.0.0/16`), joined by the gateway as `gateway` (`sessions.gatewayContainer`, default the hostname, which Docker sets to the container id). The registry rebuild re-attaches a new gateway container; rebuild and reaper prune networks of sessions without a container. `isolation: shared` keeps the old shared `sessions.network`. The socket proxy already allows `NETWORKS` and `POST`. `images/gateway/test/server-mode.sh` checks the isolation (unreachable from the shared network, 401 from the session's own network without the secret) and the re-attach after replacing the gateway container.
- No `nproc` ulimit on session containers (`docker.Harden`): `RLIMIT_NPROC` is per uid across the host and every session is uid 1000; `pids_limit` bounds each container.
- `/internal/sessions/{id}/stuck` applies the per-user hourly stuck limit too, and reports keep at most 200 events, 512 KiB of them and a 4000-byte note. Progress reports for lessons not in a loaded bundle are refused with 400 (`progress.Steps` gained `Loaded`).
- The demo player stops only on learner input in the terminal: keydowns other than lone modifiers (from xterm.js's custom key handler) and pastes. xterm.js's `onData` also carries focus reports (tmux `focus-events on`), mouse reports and OSC 52 replies, so it no longer signals input.
- A setup with `reset` turns `auto-save-visited-mode` back on (when the base module is loaded) before the lesson's `eval`, so lessons 0.1, 2.3, 2.4 and 2.5 that turn it off do not leave it off. sessiond saves modified file buffers before a daemon restart (module change, `restart-emacs`) and at shutdown whatever the mode (`supervisor.FlushForm`; never over a file changed on disk, remote or read-only files skipped).
- Layouts, window states, `single` setups and Rescue never leave a side window as the frame's only window: they select a normal window before deleting the others (`playground--leave-side-window`), and a sole window still marked as a side window is made ordinary again (`playground--unside-sole-window`; Rescue says "made the window splittable again"). Splitting such a window recursed until `max-lisp-eval-depth`.

### Decisions for the newcomer-review fixes (2026-10-27)

The triage of docs/feedback/2026-10-27-newcomer-review.md assigned every item to an area and fixed the contracts the fixes share: progress reset ("Start over"), "Watch it" as a demonstration (`demo` op, `demonstrated` status), "Skip this step" (`skip` op, `skipped` status) and nudge suppression. The contracts are in api.md (§2.4, §3.4, §4.1) and docs/feedback/2026-10-27-fix-plan.md §3; the decisions taken there (§4) refine the docs until each area updates its own: the backend lands the protocol additions first and regenerates `web/src/protocol.gen.ts`, and the smoke, review and replay scripts reset progress when they finish.

Backend decisions from the fixes (backend.md §3.6, §3.8, §3.12):

- The engine's debounce is trailing, 40 ms after the last trigger and at most 120 ms after the first (`steps.DefaultDebounce`, `DefaultDebounceMax`); key → `check` is about 50 ms.
- "Go to this step" (`scenario {stepId}` for a step after the first, also the web's resume target) restores that step's checkpoint after the setup, else fast-forwards by replaying the earlier steps' demos into Emacs with the engine rehearsing them quietly (`Engine.Rehearse`), up to 10 s per step for its conditions and 3 minutes in all; the RPC returns when the step is active. The server-side demo player is `protocol.PlayDemo` (playctl pacing), shared by playctl, skip and the fast-forward.
- Every checkpoint restore keeps `emacs.d/modules.el` (modules only grow); before, a step reset could put an older module set on disk under a running Emacs that had the newer one loaded.
- The tap drops a lone `ESC` (or other late key) whose bytes arrived before the agent's last command, and clears the pending prefix, instead of feeding it to the tracker: a silently pending `ESC` would turn the learner's next `C-x` into `C-M-x` in the tracker and hide that prefix.
- Leaving GUI mode closes Emacs's X connections (`x-close-connection`) after deleting the X frames; if that fails, Xvnc keeps running rather than dropping under Emacs. Xvnc runs with `-AcceptSetDesktopSize=1`; the GUI frame command has no `fullscreen` parameter.
- `playctl curriculum play` resets each played lesson's progress afterwards when `-session` is a gateway session path; `-watch` replays as demonstrations; `-repeat N`; `make curriculum-play` replays every lesson twice, `make curriculum-watch` Part 0 as demonstrations.

## 6. Slice 1 scope

What this round builds (everything else stays a TODO with its doc section):

- Session image: Emacs 31.1 from source (fallback 30.2, and say so), Rust and Python toolchains, ripgrep, fd, git, tmux 3.5, TigerVNC, xdotool, Iosevka Nerd Font, Noto CJK; modules base, agent, editing, completion, files, windows, ui, terminals (eat), lsp, lang-rust, lang-python, git. Other modules are stubs. Target under 4 GB.
- sessiond: all of backend.md §3 including the VNC bridge and the GUI frame.
- gateway: local mode complete; server-mode docker backend against the official client, unit-tested with a fake, marked experimental; OIDC returns 501.
- playctl: curriculum validate, build, play; config schema and check; protocol typescript (all done).
- web: every screen and pane in frontend.md, including the GUI and preview panes.
- curriculum: Parts 0 to 3 complete (lesson.md, scenario.yaml with self-solving demos, fixtures), the five tables, and `curriculum/index.yaml` listing all 15 parts (done; later parts `planned`).
