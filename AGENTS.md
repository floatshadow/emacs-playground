# AGENTS.md: how to build, test and run Emacs Playground

The operating manual for anyone (human or agent) working in this repository. The specification is in `docs/` (start with `docs/DESIGN.md`); this file covers the machine, the commands, who owns what, and the decisions taken while scaffolding that the docs do not spell out.

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

## 3. The machine

- Repository: `/home/zsy/emacs-playground`, branch `main`. Commit with `git -c user.name=zsy -c user.email=zsy83229089@gmail.com commit …`.
- Host tools: Node 24 (npm, corepack), python3, cargo/rustc, git, docker and docker compose. **Not installed on the host: tmux, emacs.** Go is installed in the user's home (below). Never use apt or sudo.
- Your uid on the host is 1109200046, not 1000. The session container runs as `learner` (uid 1000), so any directory a container must write to through a bind mount must be world-writable: `mkdir -p d && chmod 777 d`. `make local`, `make dev` and `make reset-workspace` do this for `./workspace`.
- The Docker daemon runs on a different host. Only `/home/zsy` is shared with it: bind mounts and build contexts must be under `/home/zsy`. **`/tmp` is not shared.** Ports published by containers are reachable here at `127.0.0.1`. A container started with `--network host` binds on the daemon host and is reachable at `127.0.0.1` too, and it can reach processes listening on `127.0.0.1` here (verified: a headless Chrome container loaded a page served by `vite preview` on this host).
- Network access to GitHub, go.dev, npm, ELPA/MELPA, Docker Hub and GHCR works.

### PATH and environment

```sh
export PATH=/home/zsy/.local/go/bin:/home/zsy/go/bin:/home/zsy/.local/bin:$PATH
export COREPACK_ENABLE_DOWNLOAD_PROMPT=0
```

The Makefile sets both itself.

### Toolchain installation (already done; repeat only on a fresh machine)

```sh
# Go 1.26.10 into ~/.local/go (verify the sha256 from https://go.dev/dl/?mode=json)
mkdir -p ~/.cache/go-dl && cd ~/.cache/go-dl
curl -sSLO https://go.dev/dl/go1.26.10.linux-amd64.tar.gz
echo "ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5  go1.26.10.linux-amd64.tar.gz" | sha256sum -c -
tar -C ~/.local -xzf go1.26.10.linux-amd64.tar.gz

# pnpm through corepack: a shim in ~/.local/bin, default version 11.28.8
corepack enable --install-directory ~/.local/bin pnpm
corepack install -g pnpm@11.28.8
```

pnpm is pinned to **11.28.8** (`web/package.json` `packageManager`). pnpm 12.15.0, the current `latest`, fails on this machine with `ERR_PNPM_STORE_DIR_OPEN_OPERATION_LOCK … Read-only file system (os error 30)` whatever the store directory; 11.28.8 works here and inside the Docker build. Run pnpm from inside `web/` (`cd web && pnpm …`) so corepack resolves the pinned version. pnpm 11 enforces a minimum release age for new versions; when it adds entries to `web/pnpm-workspace.yaml` (`minimumReleaseAgeExclude`), commit them with the lockfile.

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
| `make shell` | attach your terminal to the local session's Emacs (`tmux -L pg attach -t emacs`) |
| `make reset-workspace` | move `./workspace` aside and start fresh |

### Go (module `github.com/zsy/emacs-playground`, Go 1.26)

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
```

Configuration precedence: defaults < JSON file < `PG_*` environment < flags (`internal/config`). The file is `-config`, else `$PG_CONFIG`, else `/etc/playground/config.json` if it exists. Environment names are derived from the JSON path with camelCase split: `publicUrl` → `PG_PUBLIC_URL`, `sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`, `sessions.idleStopMinutes` → `PG_SESSIONS_IDLE_STOP_MINUTES`; the unsplit form and the short aliases in `config.Aliases` (`PG_IDLE_STOP_MINUTES`, `PG_MAX_SESSIONS`, `PG_SESSION_URL`, …) also work. Unknown keys in the file are errors. `go run ./cmd/playctl config check <file>` validates a file.

`deploy/config.json` is the minimal local file; `deploy/config.dev.json` is for `make dev` (gateway on `127.0.0.1:8080`, `publicUrl` `http://localhost:5173` so the Vite-proxied Origin passes, data and curriculum under `.dev/`, author mode on); `deploy/config.example.json` is the complete server example.

### sessiond

Configured only by environment (`protocol.SessiondConfig`, defaults in its `default` tags): `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_GATEWAY_URL`, `PG_LISTEN` (`:7000`), `PG_WORKSPACE` (`/work`), `PG_RUN_DIR`, `PG_AGENT_SOCKET`, `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`, `PG_PRISTINE_CONFIG`, `PG_EMACS_SERVER`, `PG_TMUX_SOCKET`, `PG_DISPLAY`, `PG_VNC_PORT`, `PG_GUI_ENABLED`, `PG_*_BIN` (binaries, overridable for tests), `PG_VERSIONS_FILE`, `PG_LOG_LEVEL`, `PG_LOG_FORMAT`, `PG_AUTHOR_MODE`.

```sh
PG_WORKSPACE=$HOME/pg-work PG_RUN_DIR=$HOME/pg-run go run ./cmd/sessiond config
```

The host has no Emacs or tmux: anything that needs them runs inside the session image (`docker run` with the repository bind-mounted, or `make dev`). Unit tests must fake subprocesses behind interfaces (backend.md §1).

### playctl

```sh
go run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts [-check]
go run ./cmd/playctl config check deploy/config.json
go run ./cmd/playctl curriculum validate curriculum       # TODO(playctl)
go run ./cmd/playctl curriculum build curriculum -o artifacts/curriculum   # TODO(playctl)
```

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
docker run -d --rm --name pg-chrome --network host zenika/alpine-chrome:latest \
  --headless --no-sandbox --disable-gpu --remote-debugging-address=0.0.0.0 \
  --remote-debugging-port=9222 about:blank
cd web && PG_URL=http://127.0.0.1:8080 PG_SHOT=/home/zsy/emacs-playground/artifacts/shot.png pnpm e2e
docker rm -f pg-chrome
```

`web/e2e/smoke.mjs` connects with `playwright-core` (`chromium.connectOverCDP('http://127.0.0.1:9222')`), loads the page, waits for the first `h1`, reports page errors and optionally takes a screenshot. Wait for `load` plus a selector; `networkidle` hangs with this Chrome. Write screenshots under `/home/zsy/…` (for example `artifacts/`, git-ignored), never `/tmp`. To serve a built bundle without the gateway: `cd web && pnpm preview --port 8080 --host 127.0.0.1 --strictPort`.

### Images and Compose

```sh
docker build -f images/gateway/Dockerfile -t ghcr.io/zsy/emacs-playground/gateway:dev .
docker build -f images/session/Dockerfile -t ghcr.io/zsy/emacs-playground/session:dev .
docker compose -f compose.yaml -f compose.local.yaml config -q         # validate
make local && docker compose up                                          # http://localhost:8080
docker compose -f compose.yaml -f compose.local.yaml -f compose.dev.yaml up -d session   # dev
```

Build contexts are the repository root; `.dockerignore` keeps `node_modules`, `web/dist`, `workspace` and `.git` out. The gateway image builds the web bundle (node stage, pnpm 11 through corepack), the Go binaries (`-tags embedweb`), and the curriculum bundle (`playctl curriculum build`, so that stage fails until playctl implements it). The session Dockerfile is a stage skeleton (emacs.md §1) with a working `sessiond` stage. The remote box sits behind the Compose profile `remote` until it exists (M6).

## 5. Decisions taken while scaffolding

These refine the docs; the code is authoritative where they differ.

- Module path `github.com/zsy/emacs-playground`; images `ghcr.io/zsy/emacs-playground/{gateway,session,remote-box}`.
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

## 6. Slice 1 scope

What this round builds (everything else stays a TODO with its doc section):

- Session image: Emacs 31.1 from source (fallback 30.2, and say so), Rust and Python toolchains, ripgrep, fd, git, tmux 3.5, TigerVNC, xdotool, Iosevka Nerd Font, Noto CJK; modules base, agent, editing, completion, files, windows, ui, terminals (eat), lsp, lang-rust, lang-python, git. Other modules are stubs. Target under 4 GB.
- sessiond: all of backend.md §3 including the VNC bridge and the GUI frame.
- gateway: local mode complete; server-mode docker backend against the official client, unit-tested with a fake, marked experimental; OIDC returns 501.
- playctl: curriculum validate, build, play; config schema and check; protocol typescript (done).
- web: every screen and pane in frontend.md, including the GUI and preview panes.
- curriculum: Parts 0 to 3 complete (lesson.md, scenario.yaml with self-solving demos, fixtures), the five tables, and `curriculum/index.yaml` listing all 15 parts (done; later parts `planned`).
