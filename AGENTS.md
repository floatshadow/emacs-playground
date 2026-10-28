# AGENTS.md: operating manual for Emacs Playground

A browser Emacs course on real per-learner Emacs containers: Go gateway and sessiond, React web app, Emacs agent, YAML curriculum. Read `AGENTS.local.md` (git-ignored, machine-specific) after it if it exists.

The specification is in `docs/`: start with `DESIGN.md` (overview, decisions, roadmap, reading guide), then `architecture.md`, `deployment.md`, `backend.md`, `api.md`, `emacs.md`, `frontend.md`, `CURRICULUM.md` and `third-party.md`. `docs/notes.md` holds the workflow, test tools, parallel QA, scope and known limitations.

## 1. The rule

**Component agents never run `go mod tidy` and never edit outside their area; integration does.** Need a Go module, npm package, protocol type, config key, Makefile target or Compose change? Report what, why and the exact version. Everything in third-party.md §5 and §6 is already in `go.mod` and `web/package.json`.

## 2. Ownership areas

| Area | Owner | Paths |
|---|---|---|
| Shared contracts | integration | `internal/{protocol,config,httpx,deps,buildinfo}/**`, `go.mod`, `go.sum`, `web/embed*.go`, `web/src/protocol.gen.ts`, `Makefile`, `compose*.yaml`, `.env.example`, `.gitignore`, `.dockerignore`, `deploy/**`, `AGENTS.md`, `README.md`, `docs/**` |
| Gateway | gateway agent | `cmd/gateway/**`, `internal/gateway/**`, `internal/docker/**`, `images/gateway/**` |
| sessiond | sessiond agent | `cmd/sessiond/**`, `internal/sessiond/**` |
| playctl, curriculum loader | playctl agent | `cmd/playctl/**`, `internal/curriculum/**` |
| Web app | frontend agent | `web/**` except the integration files (`package.json`, `pnpm-lock.yaml` via integration) |
| Emacs side | Emacs agent | `images/session/**`, `images/remote-box/**`, `emacs.d/**` |
| Curriculum | curriculum agent | `curriculum/**` |

`internal/gateway` and `internal/sessiond` never import each other; both may import `internal/{protocol,config,httpx,curriculum}`. Mark unfinished work `TODO(<area>, <doc section>)`, e.g. `TODO(sessiond, backend.md §3.6)`.

## 3. Development environment

- Host: Git, Docker Engine 27+ with Compose v2, Go 1.27, Node 24+ with corepack (pnpm from `packageManager`), python3 with PyYAML. **No Emacs or tmux on the host**: whatever needs them runs in the session image. Install: notes.md §4.
- `make` puts `~/.local/go/bin`, `~/go/bin`, `~/.local/bin` on `PATH`. Run pnpm inside `web/`.
- Containers run as uid 1000 (`learner`). If your uid differs, bind-mounted directories a container writes must be world-writable; `make local`, `make dev` and `make reset-workspace` do this for `./workspace`.
- With a remote Docker daemon, bind mounts and build contexts must be on a path it shares (usually not `/tmp`), and published ports are on its host. Write test output under `artifacts/` (git-ignored). When a sandbox refuses `docker run`/`exec` with inline shell code, use a script file.
- Never put local paths, hostnames or personal details in tracked files; commit with the repository's configured identity, never `-c user.*`.

## 4. Commands

`make help` lists every target.

| Target | Does |
|---|---|
| `make check` | all CI: Go build, gofmt, vet, protocol drift, Go tests, eslint, prettier, `tsc -b`, vitest, `vite build`, bundle size |
| `make test` / `make lint` | unit tests / linters and protocol drift |
| `make protocol` | regenerate `web/src/protocol.gen.ts` (integration only) |
| `make build` / `make gateway` | binaries and `web/dist` / gateway with embedded bundle |
| `make local` / `make server` | write `.env` for a mode; `BUILD=1 make local` builds images |
| `make images`, `image-gateway`, `image-session` | build images (`TAG=dev` for a local tag) |
| `make dev` | session container on the host network, gateway from source, Vite on :5173 |
| `make shell` / `make reset-workspace` | attach to the local Emacs / start with a fresh `./workspace` |
| `make emacs-test` | agent and configuration ERT tests in the session image |
| `make e2e` | browser smoke test against the running stack (Chrome on CDP :9222) |
| `make curriculum-play` / `curriculum-watch` | replay every lesson twice / Part 0 (`PART=…`) as Watch it, then as the learner |

```sh
go build ./... && go vet ./... && go test ./...      # no Docker, Emacs or tmux needed
go run ./cmd/gateway -config deploy/config.dev.json  # `gateway config` prints the effective config
PG_WORKSPACE=$HOME/pg-work PG_RUN_DIR=$HOME/pg-run go run ./cmd/sessiond config
go run ./cmd/playctl curriculum validate curriculum && python3 curriculum/tools/validate.py
go run ./cmd/playctl curriculum play 2.3 -session http://127.0.0.1:8080/s/local   # part:N, all, -watch, -repeat N
cd web && pnpm install --frozen-lockfile && pnpm dev # build, test, lint, format, typecheck
```

Configuration precedence: defaults < JSON file < `PG_*` environment < flags; `PG_*` names follow the JSON path (`sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`); unknown file keys are errors (deployment.md §3).

### Testing

- **Unit:** `make test`. Go tests fake every subprocess behind interfaces (backend.md §1).
- **Emacs:** `make emacs-test` (`TAG=dev` for a local image).
- **Browser:** no browser on the host; run Chrome in Docker with `--shm-size=1g` (the default 64 MB makes later page loads fail with `net::ERR_INSUFFICIENT_RESOURCES`) and drive it with playwright-core over CDP:

  ```sh
  docker run -d --rm --name pg-chrome --network host --shm-size=1g chromedp/headless-shell:156.0.8078.48  # CDP :9222
  make e2e && (cd web && node e2e/review.mjs)
  ```

  Wait for `load` plus a selector (`networkidle` hangs). Read the canvas terminal with `window.__pgScreenText()`.
- **Replays:** `make curriculum-play`, `make curriculum-watch` on a fresh workspace.
- **Integration and parallel QA** (one isolated stack per browser agent): notes.md §3.

## 5. Important invariants

- **Code is authoritative**; fix a doc that disagrees.
- **`web/src/protocol.gen.ts` is generated** by `internal/protocol/tsgen`; never edit it. `go test ./internal/protocol/...` fails when stale.
- **Go is the reference for shared semantics:** `protocol.Predicate`, `MatchLayout` and `ExpandTemplate`; the ports in `web/src/advice/` must match exactly.
- **Lesson ids contain one slash** (`02-buffers-and-files/03-opening-files`), literal in URLs; routes use `{lessonId...}`.
- **Condition ids are 1-based:** `<stepId>/<n>`, stuck rules `<stepId>/stuck/<n>` (`protocol.ConditionID`).
- **Modules unlock monotonically:** scenario `modules` are added, never removed, so every lesson must pass with more modules than it lists; checkpoint restores keep `emacs.d/modules.el`.
- **Agent ops never prompt** (`inhibit-interaction`): a command that would ask returns an error.
- **`eval` is author-mode only**, in sessiond and the gateway proxy.
- **The web bundle is embedded only with `-tags embedweb`**, so `go build ./...` never needs `web/dist`.
- **The Emacs configuration lives in the session image**, copied to `./workspace/emacs.d` on first start only: rebuild the image and `make reset-workspace` after changing `emacs.d/`.

## 6. Scope

Slice 1: notes.md §5; roadmap: DESIGN.md §10.
