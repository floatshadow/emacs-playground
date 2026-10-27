# AGENTS.md: operating manual for Emacs Playground

Emacs Playground is a browser-based Emacs course on real, per-learner Emacs containers: a Go gateway and sessiond, a React web app, an Emacs agent and a YAML curriculum. This file covers who owns what, the environment, the commands and the invariants that bite. Read `AGENTS.local.md` (git-ignored, machine-specific notes) after it if it exists.

| Doc | What it holds |
|---|---|
| `docs/DESIGN.md` | overview, goals, decision records, roadmap: start here |
| `docs/architecture.md` | components, flows, state machines, failure modes, budgets |
| `docs/deployment.md` | images, Compose, configuration file, local and server mode, security |
| `docs/backend.md` | gateway, sessiond, playctl: packages, interfaces, step engine, checkpoints |
| `docs/api.md` | every HTTP/WebSocket endpoint, event, RPC op and the agent socket protocol |
| `docs/emacs.md` | session image, configuration modules, frames, `playground-agent.el` |
| `docs/frontend.md` | web app: screens, state, panes, advice engine, testing |
| `docs/CURRICULUM.md` | the course and the lesson authoring format |
| `docs/third-party.md` | every dependency with version and license |
| `docs/notes.md` | developer workflow, longer commands, toolchain install, slice-1 scope, known limitations, and a map of where each detail is specified |
| `docs/history/` | completed reviews and fix plans (historical record only) |

## 1. The rule

**Component agents never run `go mod tidy` and never edit files outside their area; integration does.** Need a Go module, npm package, protocol type, config key, Makefile target or Compose change? Say what, why and the exact version in your report. Every Go module of third-party.md §5 is already in `go.mod` and every frontend package of §6 in `web/package.json`.

## 2. Ownership areas

| Area | Owner | Paths |
|---|---|---|
| Shared contracts | integration | `internal/protocol/**` (incl. `tsgen/`), `internal/config/**`, `internal/httpx/**`, `internal/deps/**`, `internal/buildinfo/**`, `go.mod`, `go.sum`, `web/embed*.go`, `web/src/protocol.gen.ts`, `Makefile`, `compose*.yaml`, `.env.example`, `.gitignore`, `.dockerignore`, `deploy/**`, `AGENTS.md`, `README.md`, `docs/**` |
| Gateway | gateway agent | `cmd/gateway/**`, `internal/gateway/**`, `internal/docker/**`, `images/gateway/**` |
| sessiond | sessiond agent | `cmd/sessiond/**`, `internal/sessiond/**` |
| playctl, curriculum loader | playctl agent | `cmd/playctl/**`, `internal/curriculum/**` |
| Web app | frontend agent | `web/**` except the integration files above (`package.json` and `pnpm-lock.yaml` change only through integration) |
| Emacs side | Emacs agent | `images/session/**`, `images/remote-box/**`, `emacs.d/**` |
| Curriculum content | curriculum agent | `curriculum/**` |

`internal/gateway` and `internal/sessiond` never import each other; both may import `internal/protocol`, `internal/config`, `internal/httpx` and `internal/curriculum`. Mark unfinished work `TODO(<area>, <doc section>)`, e.g. `TODO(sessiond, backend.md §3.6)`.

## 3. Development environment

- Host: Git, Docker Engine 27+ with Compose v2, Go 1.26 (`go.mod` pins it), Node 24 with corepack (pnpm 11.28.8 pinned in `web/package.json`), python3 with PyYAML. Emacs and tmux are **not** needed on the host: everything that needs them runs in the session image. Install steps for Go and pnpm: `docs/notes.md` §4.
- `make` puts `~/.local/go/bin`, `~/go/bin` and `~/.local/bin` on `PATH`. Run pnpm from inside `web/` so corepack picks the pinned version.
- Containers run as uid 1000 (`learner`). If your uid differs, a bind-mounted directory a container writes to must be world-writable; `make local`, `make dev` and `make reset-workspace` do this for `./workspace`.
- With a Docker daemon on another machine, bind mounts and build contexts must be on a path the daemon shares (usually not `/tmp`), and published ports are where the daemon is. Write screenshots and test output under `artifacts/` (git-ignored). Keep such notes in `AGENTS.local.md`; never put local paths, hostnames or personal details in tracked files.
- Commit with the identity configured in the repository; never pass `-c user.*`.

## 4. Commands

`make help` lists every target.

| Target | What it does |
|---|---|
| `make check` | everything CI runs: Go build, gofmt, vet, protocol drift, Go tests, eslint, prettier, `tsc -b`, vitest, `vite build`, bundle size gate |
| `make test` / `make lint` | unit tests (Go and web) / linters and the protocol drift check |
| `make protocol` | regenerate `web/src/protocol.gen.ts` from `internal/protocol` (integration only) |
| `make build` / `make gateway` | Go binaries into `bin/` and `web/dist` / the gateway with the bundle embedded |
| `make local` / `make server` | write `.env` for a deployment mode; `BUILD=1 make local` builds images instead of pulling |
| `make images`, `make image-gateway`, `make image-session` | build images (`TAG=dev` for a local tag) |
| `make dev` | session container on the host network, gateway from source, Vite on :5173 |
| `make shell` | attach your terminal to the local session's Emacs |
| `make reset-workspace` | move `./workspace` aside and start fresh |
| `make emacs-test` | the agent's and the configuration's ERT tests inside the session image |
| `make e2e` | browser smoke scenarios against the running local stack (Chrome on CDP :9222) |
| `make curriculum-validate`, `curriculum-build` | validate `curriculum/`; build the bundle into `artifacts/curriculum` |
| `make curriculum-play` / `curriculum-watch` | replay every ready lesson twice / Part 0 (`PART=…`) as "Watch it", then as the learner |
| `make fonts`, `make config-schema`, `make clean` | dev fonts into `.dev/fonts`; regenerate `deploy/config.schema.json`; remove build output |

Most-used commands per component (more in `docs/notes.md` §2 and §3):

```sh
go build ./... && go vet ./... && go test ./...      # no Docker, Emacs or tmux needed
gofmt -l cmd internal web/*.go                       # must print nothing
go run ./cmd/gateway -config deploy/config.dev.json  # gateway (local mode); `gateway config` prints the effective config
PG_WORKSPACE=$HOME/pg-work PG_RUN_DIR=$HOME/pg-run go run ./cmd/sessiond config
go run ./cmd/playctl curriculum validate curriculum && python3 curriculum/tools/validate.py
go run ./cmd/playctl curriculum play 2.3 -session http://127.0.0.1:8080/s/local   # also part:N, all, -watch, -repeat N
cd web && pnpm install --frozen-lockfile && pnpm dev # also: build, test, lint, format, typecheck
docker build -f images/session/Dockerfile -t ghcr.io/floatshadow/emacs-playground/session:dev .
```

Configuration precedence: defaults < JSON file < `PG_*` environment < flags. `PG_*` names derive from the JSON path (`sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`); unknown file keys are errors. Details in deployment.md §3.

### Testing

- **Unit**: `make test`. Go tests fake every subprocess behind interfaces (backend.md §1); web tests run under vitest/jsdom.
- **Emacs**: `make emacs-test` (`TAG=dev make emacs-test` for a locally built image).
- **Browser**: there is no browser on the host; run Chrome in Docker and drive it with playwright-core over CDP. Always give it `--shm-size=1g`: with the default 64 MB `/dev/shm` the WebGL terminal and VNC canvas make later page loads fail with `net::ERR_INSUFFICIENT_RESOURCES`.

  ```sh
  docker run -d --rm --name pg-chrome --network host --shm-size=1g zenika/alpine-chrome:latest \
    --headless --no-sandbox --disable-gpu --remote-debugging-address=0.0.0.0 --remote-debugging-port=9222 about:blank
  make e2e                       # web/e2e/smoke.mjs; PG_SMOKE_ONLY=3,4 for a subset, PG_SMOKE_PLAY=0 skips replays
  cd web && node e2e/review.mjs  # acceptance checks per behaviour; PG_REVIEW_ONLY=B1,B4 for a subset
  docker rm -f pg-chrome
  ```

  Wait for `load` plus a selector (`networkidle` hangs). The terminal is a canvas: read it with `window.__pgScreenText()`.
- **Curriculum replays**: `make curriculum-play` and `make curriculum-watch` on a fresh workspace (`make reset-workspace`); they reset the replayed lessons' progress afterwards.
- **Integration** (sessiond against the real image, docker backend against the real Engine, server-mode smoke test): `docs/notes.md` §3.

## 5. Important invariants

- **Code is authoritative.** When a doc and the code disagree, check the code and fix the doc.
- **`web/src/protocol.gen.ts` is generated** from Go by `internal/protocol/tsgen` (`make protocol`); never edit it. `go test ./internal/protocol/...` fails when it is stale.
- **Go is the reference for shared semantics.** `protocol.Predicate`, `protocol.MatchLayout` and `protocol.ExpandTemplate` define exit-table and condition semantics; the TypeScript ports in `web/src/advice/` must match them exactly.
- **Lesson ids contain one slash** (`02-buffers-and-files/03-opening-files`), which goes into URLs literally; routes use trailing wildcards (`{lessonId...}`).
- **Condition ids are 1-based**: `<stepId>/<n>`; stuck rules `<stepId>/stuck/<n>` (`protocol.ConditionID`).
- **Containers run as uid 1000**, whatever the host uid.
- **Configuration modules unlock monotonically**: a scenario's `modules` are added, never removed, so every lesson must pass with more modules than it lists. Checkpoint restores keep `emacs.d/modules.el`. Replay on a fresh workspace for results that match a learner going through the course in order.
- **Agent ops never prompt**: they run with `inhibit-interaction`, so a command that would ask a question returns an error reply instead of blocking.
- **`eval` is author-mode only**, in sessiond and in the gateway proxy.
- **The web bundle is embedded only with `-tags embedweb`**, so `go build ./...` never needs `web/dist`.
- **The Emacs configuration lives in the session image** and is copied to `./workspace/emacs.d` on first start only: after changing `emacs.d/`, rebuild the session image and `make reset-workspace`.

## 6. Scope

Slice 1 (Parts 0 to 3, local mode complete, the docker backend experimental, OIDC answering 501) is summarised in `docs/notes.md` §5; the roadmap is DESIGN.md §10.
