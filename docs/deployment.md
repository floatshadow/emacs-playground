# Deployment, configuration and security

*Planned* items carry a `TODO(<area>, deployment.md §N)` in the code.

## 1. Images

| Image (`ghcr.io/floatshadow/emacs-playground/…`) | Contents |
|---|---|
| `gateway` | distroless; gateway with the embedded web bundle (`-tags embedweb`), the curriculum bundle (`/srv/curriculum`, strict build), web fonts (`/srv/fonts`) |
| `session` | Emacs 31.1 (native compilation), tmux, TigerVNC, toolchains, pinned packages, sessiond; about 3.4 GB |
| `remote-box` | sshd and the session's toolchain pins, no Emacs |

Dockerfiles: `images/<name>/Dockerfile`, context the repository root. Pins: third-party.md; `images/session/versions.env` is reported by sessiond's `/status`.

GitHub Actions builds everything: `ci.yml` (Go, web and curriculum checks), `image-emacs.yml` (session and remote-box images, then their ERT suites; cold build 40–60 min), `image-web.yml` (gateway image, smoke-tested in local mode), `release.yml` (`v*` tags). Pull requests build without pushing.

Tags: `main`, `main-<sha>`, `latest`; releases add `X.Y.Z`, `X.Y`. Forks set `IMAGE_BASE` in `.env`. *Planned:* the gateway refuses a session image of another major version.

## 2. Compose files

| File | Adds |
|---|---|
| `compose.yaml` | gateway with `gateway-data` and `deploy/config.json`; networks `front`, internal `sessions` |
| `compose.local.yaml` | local mode on `127.0.0.1:8080`; session (`./workspace:/work`, id and token `local`); remote box (alias `box`) |
| `compose.server.yaml` | server mode; Caddy (80, 443); Docker socket proxy on the internal `docker` network; invites and OIDC secret |
| `compose.dev.yaml` | `make dev`: session on the host network, `127.0.0.1:7000` |

`.env` holds only `COMPOSE_FILE`, `TAG`, `IMAGE_BASE`, `DOMAIN`. Never set `hostname:` on the gateway in server mode: it finds its own container by hostname (§7). Caddy answers `/internal/*` with 404; the server-mode gateway also refuses `/internal` requests with forwarding headers.

Server-mode session containers: `pg-session-<id>` (name and hostname), `playground.*` labels, the session network (§7), volume `pg-ws-<user>` at `/work`, the §9.2 profile, and `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_PROXY_SECRET`, `PG_GATEWAY_URL` plus geometry from the config.

## 3. Configuration file

File: `-config`, else `$PG_CONFIG`, else `/etc/playground/config.json`. Precedence: defaults < file < `PG_*` environment < flags (`-set key.path=value`). Environment names split camelCase (`sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`); unsplit names and `config.Aliases` also work. Unknown keys are errors; secrets are files (`*File` keys). All keys and defaults: `config.Defaults`, the generated `deploy/config.schema.json`, `deploy/config.example.json`; `gateway config` prints the effective configuration, secrets redacted.

Keys with non-obvious behaviour:

| Key | Default | Notes |
|---|---|---|
| `publicUrl` | `http://localhost:8080` | cookies, OIDC redirect, Origin checks; locally `localhost` and `127.0.0.1` are interchangeable |
| `auth.mode` | `none` | `none` is local only; `invite`; `oidc` answers 501 (*planned*) |
| `auth.invites.file` | | per line: code, optional label, `uses=N`, `until=YYYY-MM-DD`; hashed into the store on first load |
| `sessions.backend` | `static` local, `docker` server | |
| `sessions.isolation` | `network` | per-session networks (§7); `shared` uses `sessions.network` |
| `sessions.subnetPool` | `10.231.0.0/16` | per-session `/28`s; empty falls back to the daemon's pools (about 30 networks) |
| `sessions.gatewayContainer` | own hostname | joins every session network |
| `sessions.runtime` | `runc` | `runsc` for gVisor |
| `sessions.limits.disk` | `4g` | enforced only by storage drivers with quotas |
| `sessions.egress` | `registries` | §9.2 |
| `local.sessionId`, `sessionToken` | `local` | must match `compose.local.yaml` |
| `features.authorMode` | `false` | raw conditions and state, `rpc eval`, hot reload |

sessiond reads environment only (`protocol.SessiondConfig`, backend.md §3.13). `PG_PROXY_SECRET` is derived per session (backend.md §2.3) from `<dataDir>/session-proxy.secret`, a key separate from the cookie secret so rotating cookies does not lock out running sessions. Unset locally: sessiond then serves without it.

## 4. Local mode

`make local && docker compose up`, then http://localhost:8080 (`BUILD=1` builds instead of pulling).

- `./workspace` becomes a git repository on first start and receives the image's Emacs configuration then (emacs.md §3).
- The session runs as uid 1000, so `make local` makes the workspace world-writable.
- `make shell` attaches a native terminal to the browser's frame.

## 5. Server mode (experimental)

Copy `deploy/config.example.json` and `deploy/invites.example.txt` to `deploy/config.json` and `deploy/invites.txt`, edit, then `make server DOMAIN=… && docker compose up -d`.

- Host: 4 GB RAM plus about 1.5 GB and a quarter core per concurrent session; 10 GB disk plus 2–4 GB per learner; ports 80 and 443; `DOMAIN` resolving before first start.
- Upgrade: `TAG=X.Y.Z docker compose pull && docker compose up -d`; running sessions keep their image until they stop.
- Monitoring: `/healthz`, optional Prometheus (*planned:* latency histograms), JSON logs.
- Backups: §8; *planned:* `playctl export-user`.
- `images/gateway/test/server-mode.sh <image>` smoke-tests server mode, isolation and gateway replacement.

## 6. Remote box

sshd, a `deploy` user and the session toolchains; TRAMP lessons use `/ssh:box:`, P5 and P6 deploy to it. Locally one box starts with the stack and has no authorised key yet. *Planned (M6):* a key pair per session with a `box` alias in `~/.ssh/config`; on servers a box per learner on the session network.

## 7. Networking

| From → To | Port | Purpose |
|---|---|---|
| internet → Caddy → gateway | 443 → 8080 | HTTPS, WSS |
| gateway → sessiond | 7000 | proxy, RPC, status; proxy secret on all but `/healthz` |
| gateway → socket proxy | 2375 | Engine API (allows `NETWORKS`, `POST`) |
| sessiond → gateway | 8080 | `/internal`, bearer session token |
| session → remote box | 22 | TRAMP |
| session → internet | 443 | per `sessions.egress` |

With `isolation: network`, each session is alone on `pg-net-<id>`, created before its container, `internal` unless egress is `all`, removed with it. The gateway joins as alias `gateway`. A replacement gateway re-joins running sessions' networks while rebuilding its registry; rebuild and reaper prune networks without a container. Sessions reach only the gateway.

## 8. Storage

| Volume | Content | Backup |
|---|---|---|
| `gateway-data` | `playground.db` (server) or `local.json` (local), secrets | nightly |
| `pg-ws-<user>` / `./workspace` | learner files, config, checkpoints | by policy |
| `caddy-data` | certificates | optional |

## 9. Security

### 9.1 Trust boundaries and threats

Learners are untrusted and run arbitrary code; host and operator are trusted. Threats, by priority: escaping or reaching another session; the Docker API; resource abuse; workspace content attacking the web app; account theft; supply chain.

### 9.2 Session container profile

`docker.Harden` (server mode); `compose.local.yaml` keeps `cap_drop: ALL`, `no-new-privileges`, `pids_limit` and tmpfs `/tmp`, `/run`.

```
user 1000 (learner), no sudo; init; read-only rootfs
tmpfs /tmp (1g, exec: builds), /run (64m, noexec), /home/learner/.cache (2g)
only pg-ws-<user>:/work; no Docker socket, no published ports
cap_drop ALL; no-new-privileges; default seccomp and AppArmor
sessions.limits; memory-swap = memory; nofile 65536; restart none; stop timeout 20 s
```

No `nproc` ulimit: it counts per uid across the host and every session is uid 1000, so it would be one budget for all; `pids_limit` bounds each container.

Egress: session networks have no route unless `egress: all` (NAT); lessons work offline. *Planned:* `registries` adds an allowlist proxy (crates.io, opam, PyPI, Typst packages, CTAN) via `HTTP(S)_PROXY`; until then it behaves like `none`.

### 9.3 Gateway and web application

- Cookies and login: backend.md §2.6. CSRF: `Origin` must match `publicUrl` on state-changing requests and WebSocket upgrades; no CORS.
- `/s/{id}` only for its owner; at most 6 WebSockets per user; `/internal` tokens are random 256-bit, stored hashed, per session. The proxy strips `Cookie`, `Authorization` and sessiond's `Set-Cookie`; `rpc eval` needs author mode; internal ops are refused from browsers.
- sessiond requires `X-Playground-Proxy` (constant-time, not forwarded) on all but `GET`/`HEAD /healthz` when `PG_PROXY_SECRET` is set, else 401; with §7, one learner cannot reach another's Emacs, previews or RPCs.
- Previews: read through `os.Root`; active content gets a sandboxing CSP, `nosniff`, a sandboxed iframe. Open question (DESIGN.md §12): the CSP keeps `allow-same-origin`, so a learner's HTML opened directly can make credentialed requests to `/api` and `/s`; closing it needs a separate origin or dropping that flag.
- App headers (`internal/gateway/static.go`): strict CSP, `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`; HSTS via Caddy.
- Rate limits: login 10 per IP and 20 per code a minute; `POST /api/v1/sessions` 10 per user a minute, made once per page load; stuck reports 10 per user an hour (browser and `/internal`; caps in api.md §2.5). Progress is accepted only for steps in the loaded bundle.
- Docker only through the socket proxy; no privileged containers or host mounts; only labelled objects.

### 9.4 Data and privacy

Event logs stay in the session (`/work/.playground/events.log`, rotated at 10 MB) except the last 200 sent with "I'm stuck"; they never hold file contents or secrets. `features.telemetryUpload` (off) only forwards stuck reports. *Planned:* data export and `DELETE /api/v1/me`.

### 9.5 Supply chain and repository hygiene

CI builds from pinned bases, `versions.env` (checksummed) and `packages.lock` (SHA-256, MELPA vendored); SBOM and provenance on each image. *Planned:* `govulncheck`, `pnpm audit`, automated pin updates. Open: no LICENSE file; `deploy/config.json` is tracked although `make local` writes it; two unused devDependencies (third-party.md §6).

### 9.6 Residual risks

gVisor narrows kernel escapes. A learner can exhaust only their own session. The remote box (M6) gets the same profile.
