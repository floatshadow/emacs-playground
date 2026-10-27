# Deployment, configuration and security

Images, Compose files, the configuration file, networking, storage, operations and the security model for both deployment modes. Items marked *planned* are specified here but not implemented yet; the code carries a `TODO(<area>, deployment.md §N)` for each.

## 1. Images

| Image | Dockerfile | Contents |
|---|---|---|
| `ghcr.io/floatshadow/emacs-playground/gateway` | `images/gateway/Dockerfile` | static gateway binary with the embedded web bundle (`-tags embedweb`), the curriculum bundle (`/srv/curriculum`, built strictly by `playctl curriculum build`) and the web fonts (`/srv/fonts`); distroless |
| `ghcr.io/floatshadow/emacs-playground/session` | `images/session/Dockerfile` | Emacs 31.1 built from source with native compilation, tmux, TigerVNC, toolchains, the pinned Emacs packages, sessiond; about 3.4 GB |
| `ghcr.io/floatshadow/emacs-playground/remote-box` | `images/remote-box/Dockerfile` | sshd and the same toolchain pins, no Emacs |

Build contexts are the repository root. Pins live in `images/session/versions.env` (copied into the image as `/opt/playground/versions.env` and reported by sessiond's `/status`), the `ARG`s of the Dockerfiles, `emacs.d/packages.lock`, `go.mod` and `web/package.json` (third-party.md).

GitHub Actions builds every image; nobody needs to build locally to run or deploy:

| Workflow | Runs on | Does |
|---|---|---|
| `ci.yml` | every push to `main` and pull request | gofmt, `go vet`, `go build`, `go test -race`, protocol drift check, curriculum validation; web lint, prettier, typecheck, vitest, build, bundle size gate |
| `image-emacs.yml` | pushes and pull requests touching `images/session`, `images/remote-box`, `emacs.d`, `cmd/sessiond`, `internal`, `go.mod`; manual dispatch | builds the session and remote-box images with the BuildKit cache in the registry (`…/session:buildcache`); after pushing, runs the agent's and the configuration's ERT suites in the pushed image. A cold build takes 40 to 60 minutes on a standard runner |
| `image-web.yml` | pushes and pull requests touching `web`, `cmd/gateway`, `cmd/playctl`, `internal`, `curriculum`, `images/gateway`, `go.mod` | builds the gateway image and smoke-tests it in local mode (`/healthz`, the curriculum index with 15 parts, the app shell); pull requests build without pushing |
| `release.yml` | a `v*` tag | runs both image workflows with push |

Tags: `main`, `main-<sha>` and `latest` follow the main branch; a release adds `X.Y.Z`, `X.Y` and `latest`. Pushed images carry a provenance attestation and an SBOM. Pulling needs no login while the packages are public; forks publish under `ghcr.io/<owner>/<repo>/…` and select them with `IMAGE_BASE` in `.env`. *Planned:* the gateway refuses a session image whose `org.opencontainers.image.version` has another major version.

## 2. Compose files

| File | Adds |
|---|---|
| `compose.yaml` | the gateway (`gateway-data:/data`, `deploy/config.json` at `/etc/playground/config.json`, health check `/gateway healthcheck`), networks `front` and the internal `sessions` |
| `compose.local.yaml` | `PG_MODE=local`, port `127.0.0.1:8080`; the session container (`./workspace:/work`, `PG_SESSION_ID`/`PG_SESSION_TOKEN` `local`, `cap_drop: ALL`, `no-new-privileges`, `pids_limit` 2048, tmpfs `/tmp` and `/run`, health check `sessiond healthcheck`); the remote box with the network alias `box` |
| `compose.server.yaml` | `PG_MODE=server`; Caddy (ports 80 and 443, `deploy/Caddyfile`), the Docker socket proxy on the internal `docker` network, `deploy/invites.txt` and the OIDC client secret for the gateway |
| `compose.dev.yaml` | for `make dev`: the session on the host network (`PG_LISTEN=127.0.0.1:7000`, `PG_GATEWAY_URL=http://127.0.0.1:8080`) |

`.env` selects the mode through `COMPOSE_FILE` (written by `make local` or `make server`), so plain `docker compose up` works afterwards. It holds only `COMPOSE_FILE`, `TAG`, `IMAGE_BASE` and `DOMAIN`; everything else is in `deploy/config.json` (§3). Do not set `hostname:` on the gateway in server mode: it finds its own container through its hostname (§7).

Caddy obtains certificates for `DOMAIN`, passes WebSockets through unchanged and answers `/internal/*` with 404 (sessiond reaches `/internal` only over the session network; the gateway also refuses `/internal` requests that carry forwarding headers in server mode).

In server mode the gateway creates session containers itself: the configured image, name and hostname `pg-session-<id>`, labels `playground.session`, `playground.user`, `playground.version`, `playground.role`, the session's own network (§7), volume `pg-ws-<user>` at `/work` (a hash of the user id when the id is not a valid volume name), env `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_PROXY_SECRET`, `PG_GATEWAY_URL`, `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`, and the profile of §9.2. *Planned (M6):* a remote box per learner created the same way with the learner's public key.

## 3. Configuration file

The gateway reads one JSON file: `-config`, else `$PG_CONFIG`, else `/etc/playground/config.json` if it exists. `deploy/config.json` is the minimal local file, `deploy/config.dev.json` the `make dev` file, `deploy/config.example.json` a complete server example. Unknown keys are errors.

Precedence, lowest to highest: built-in defaults, the file, `PG_*` environment variables, flags (`-set key.path=value` and shortcuts such as `-listen`). Environment names come from the JSON path with camelCase split: `sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`, `publicUrl` → `PG_PUBLIC_URL`; the unsplit form (`PG_PUBLICURL`) and the short aliases in `config.Aliases` (`PG_IDLE_STOP_MINUTES`, `PG_MAX_SESSIONS`, `PG_SESSION_URL`, `PG_AUTH_MODE`, …) also work; lists are comma-separated. Secrets are files (`*File` keys), so they can come from Docker secrets and never sit in the JSON.

| Key | Default | Meaning |
|---|---|---|
| `mode` | `local` | `local` (one fixed session) or `server` (one container per learner) |
| `listen` | `:8080` | gateway listen address |
| `publicUrl` | `http://localhost:8080` | cookies, OIDC redirect, Origin checks; in local mode `localhost` and `127.0.0.1` are interchangeable |
| `dataDir` | `/data` | store, cookie and proxy secrets |
| `auth.mode` | `none` | `none` (local mode only), `invite`, `oidc` (*planned*: answers 501) |
| `auth.invites.file` | | one code per line, with optional label, `uses=N`, `until=YYYY-MM-DD` (`deploy/invites.example.txt`); codes are hashed in the store on first load |
| `auth.oidc.issuer`, `clientId`, `clientSecretFile`, `allowedEmailDomains` | | OIDC provider |
| `auth.cookieSecretFile` | `<dataDir>/cookie.secret` | HMAC key; generated on first start if missing |
| `auth.sessionDays` | `30` | cookie lifetime |
| `sessions.backend` | `static` in local mode, `docker` in server mode | `static` uses the session at `local.sessionUrl`; `docker` is experimental |
| `sessions.dockerHost` | | Engine API endpoint, normally `tcp://socket-proxy:2375` |
| `sessions.image`, `sessions.remoteBox.image` | the `:latest` images | images for sessions and remote boxes |
| `sessions.isolation` | `network` | `network`: a network per session (§7); `shared`: all sessions on `sessions.network` |
| `sessions.subnetPool` | `10.231.0.0/16` | IPv4 range the per-session `/28` subnets come from (4096 sessions); empty lets the daemon choose from its `default-address-pools` (about 30 networks unless configured) |
| `sessions.gatewayContainer` | the gateway's hostname | the gateway's own container, which joins every per-session network; Docker sets the hostname to the container id, so the default works unless Compose sets `hostname:` |
| `sessions.network` | `emacs-playground_sessions` | the shared network for `isolation: shared` |
| `sessions.runtime` | `runc` | `runsc` for gVisor |
| `sessions.limits.cpus`, `memory`, `pids`, `disk` | `2`, `3g`, `1024`, `4g` | per session; `disk` is a volume size hint, enforced only on storage drivers with quotas |
| `sessions.idleStopMinutes` | `30` | stop a session with no WebSocket for this long |
| `sessions.maxSessions` | `50` | refuse new sessions beyond this |
| `sessions.volumeRetentionDays` | `30` | remove workspace volumes this long after their session stopped |
| `sessions.egress` | `registries` | `none`, `registries`, `all` (§9.2) |
| `sessions.remoteBox.enabled` | `true` | provision an SSH box per learner (*planned*, M6) |
| `sessions.gui.enabled`, `geometry` | `true`, `1600x900` | GUI frame availability and size |
| `sessions.terminal.initialSize` | `120x36` | tmux window size until the browser resizes |
| `local.sessionUrl` | `http://session:7000` | the fixed session in local mode |
| `local.sessionId`, `local.sessionToken` | `local`, `local` | the fixed session's id and `/internal` token (match `PG_SESSION_ID` and `PG_SESSION_TOKEN` in `compose.local.yaml`) |
| `local.workspace` | `./workspace` | documentation for the Makefile; Compose mounts it |
| `curriculum.dir`, `defaultLocale` | `/srv/curriculum`, `en` | the built bundle |
| `logging.level`, `format` | `info`, `json` | slog |
| `metrics.enabled`, `listen` | `false`, `:9090` | Prometheus endpoint |
| `features.stuckReports` | `true` | allow "I'm stuck" reports |
| `features.telemetryUpload` | `false` | never on by default |
| `features.authorMode` | `false` | condition results and raw state in lessons, `rpc eval`, curriculum hot reload |

`deploy/config.schema.json` is generated from the Go struct (`playctl config schema`); `playctl config check <file>` validates a file without starting anything; `gateway config` prints the effective configuration with secrets redacted, which the gateway also logs at start.

sessiond is configured only by environment (`protocol.SessiondConfig`, defaults in its `default` tags; the full list is in backend.md §3.13). The ones a deployment sets: `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_PROXY_SECRET`, `PG_GATEWAY_URL`, `PG_LISTEN` (`:7000`), `PG_VNC_GEOMETRY` and `PG_TERM_SIZE` (from `sessions.gui.geometry` and `sessions.terminal.initialSize`), `PG_GUI_ENABLED`, `PG_AUTHOR_MODE`.

`PG_PROXY_SECRET` is `hex(HMAC-SHA256(key, "playground session proxy\0" + id))` with a key the gateway keeps in `<dataDir>/session-proxy.secret` (created on first use; separate from the cookie secret so rotating that one does not lock the gateway out of running sessions). In local mode it is unset and sessiond serves without it.

## 4. Local mode

```
git clone https://github.com/floatshadow/emacs-playground && cd emacs-playground
make local            # writes .env and a minimal deploy/config.json, creates ./workspace, pulls images (BUILD=1 builds)
docker compose up
open http://localhost:8080
```

- `./workspace` is initialised as a git repository on first start, and the Emacs configuration is copied from the image to `./workspace/emacs.d` on the first start only (after changing `emacs.d/` in the repository, rebuild the session image and `make reset-workspace`). The session runs as uid 1000 whatever the host uid, so `make local` creates the workspace world-writable.
- `make shell` attaches a native terminal to the same Emacs (`tmux -L pg attach-session -t emacs` in the session container); the browser stays attached too and both show the same frame.
- `make reset-workspace` moves `./workspace` aside (`workspace.bak-<time>`) and starts fresh.
- `make dev` runs the session container on the host network, the gateway from source with `deploy/config.dev.json` and Vite on :5173 (backend.md §4, frontend.md §15).

## 5. Server mode

The docker backend is experimental in slice 1.

```
cp deploy/config.example.json deploy/config.json   # edit publicUrl, auth, limits, images
cp deploy/invites.example.txt deploy/invites.txt   # or configure OIDC (planned)
make server DOMAIN=emacs.example.edu
docker compose up -d
```

- Host: Linux with Docker Engine 27 or later, 4 GB RAM plus 1.5 GB per concurrent session, 10 GB disk for images plus 2 to 4 GB per learner. Ports 80 and 443 open; point `DOMAIN` at the host before the first start so Caddy can obtain a certificate.
- Upgrades: `TAG=X.Y.Z docker compose pull && docker compose up -d`. Running sessions keep their image until they stop; the session info's `updateAvailable` lets the web app show a "restart to update" chip.
- Monitoring: `GET /healthz`, Prometheus on `metrics.listen` when enabled (`playground_sessions` by state, `playground_websockets`, `playground_curriculum_loaded`, Go and process metrics; *planned:* creation and proxy latency histograms), structured logs on stdout; successful polling requests are logged at debug level. sessiond has `/healthz` and `/status` inside the session network.
- Backups: the gateway data volume nightly (SQLite, small). Workspace volumes are learner data. *Planned:* `playctl export-user <id>` for a tarball of a workspace and its progress.
- Capacity: about 1.5 GB of memory and a quarter of a CPU core per concurrent session at the defaults, more during builds; `sessions.maxSessions` is the guard.
- `images/gateway/test/server-mode.sh <gateway image>` (from the repository root) smoke-tests server mode, including the network isolation and the re-attach after replacing the gateway container.

## 6. Remote box

A minimal Debian container with sshd, a `deploy` user and the session toolchains. TRAMP lessons use `/ssh:box:`; Project P6's deploy task and P5's publish task target it. In local mode one box starts with the stack under the network alias `box` and sshd runs with the keys in `./workspace/.playground/ssh` (none yet). *Planned (M6):* sessiond generates a key pair per session and writes the `box` Host alias to the learner's `~/.ssh/config`; on servers each learner gets their own box on the session's network.

## 7. Networking

| From | To | Port | Purpose |
|---|---|---|---|
| internet | Caddy | 443 | HTTPS and WSS |
| Caddy | gateway | 8080 | HTTP |
| gateway | sessiond | 7000 | proxy, health, RPC, status; every request but `/healthz` carries the session's proxy secret (§9.3) |
| gateway | socket proxy | 2375 | Engine API |
| sessiond | gateway | 8080 | fixtures, progress, stuck reports, heartbeats (`/internal`, bearer session token) |
| session | remote box | 22 | TRAMP |
| session | internet | 443 | per `sessions.egress` (§9.2) |

With `sessions.isolation: network` (the default) every session container sits alone on its own bridge network `pg-net-<id>`, labelled `playground.role=network`. The gateway creates it before the container, with a `/28` from `sessions.subnetPool`, `internal` unless `sessions.egress` is `all`; joins it under the alias `gateway` (so `PG_GATEWAY_URL=http://gateway:8080` resolves); and removes it with the container. When the gateway container is replaced, the new one re-joins the running sessions' networks while it rebuilds its registry; the rebuild and the reaper remove networks of sessions without a container. Sessions therefore reach the gateway and nothing else of the playground, and cannot resolve or reach each other. `isolation: shared` puts every session on the Compose `sessions` network instead. The socket proxy allows `NETWORKS` and `POST` for this.

## 8. Storage

| Volume | Content | Backup |
|---|---|---|
| `gateway-data` | `playground.db` (server) or `local.json` (local), cookie secret, session proxy key | nightly |
| `pg-ws-<user>` (server) or `./workspace` (local) | learner files, config, checkpoints, playground state | by policy |
| `caddy-data` | certificates | optional |

## 9. Security

### 9.1 Trust boundaries and threats

The learner is untrusted: they run arbitrary code inside their session (that is the point). The gateway host and the operator are trusted. Threats, in priority order: a learner escaping the container or reaching another learner's session; a learner reaching the Docker API; resource abuse (mining, fork bombs, disk filling); a learner's workspace content attacking the web app (served previews); account and session theft; supply-chain compromise of images.

### 9.2 Session container profile

Applied by the gateway in server mode (`docker.Harden`); `compose.local.yaml` applies the parts that do not get in the way locally.

```
user: 1000:1000 (learner), no sudo; init: true
read_only rootfs; tmpfs: /tmp (1g, exec: builds need it), /run (64m, noexec), /home/learner/.cache (2g)
volumes: pg-ws-<user>:/work only
cap_drop: ALL; security_opt: no-new-privileges:true; Docker's default seccomp and AppArmor profiles
pids_limit: sessions.limits.pids (1024); cpus 2; memory 3g; memory-swap = memory; ulimit nofile 65536; no nproc ulimit
network: pg-net-<id> (internal; the session and the gateway only, §7); no published ports; no Docker socket
restart policy: none (the gateway re-creates); stop timeout 20 s
runtime: runc, or runsc (gVisor) for public deployments
```

There is no `nproc` ulimit: `RLIMIT_NPROC` counts every process and thread of a uid on the whole host, and every session runs as uid 1000 (runc has no user namespace here), so it would be one budget shared by all sessions. The cgroup `pids_limit` bounds each container on its own. (`userns-remap` or a uid per session would make a per-session `nproc` meaningful.)

Egress: session networks are internal (no default route) unless `sessions.egress` is `all`, which gives a NAT route. `none` keeps sessions offline; lessons still work (emacs.md §9). *Planned:* `registries` adds an egress proxy container on the session networks (Squid with an allowlist: crates.io, static.crates.io and index.crates.io, opam.ocaml.org and GitHub release URLs for opam, pypi.org and files.pythonhosted.org, packages.typst.org, CTAN mirrors) reached through `HTTP(S)_PROXY` with `NO_PROXY=gateway`; until then `registries` behaves like `none`.

### 9.3 Gateway and web application

- Authentication and cookies as in backend.md §2.6: HMAC-signed cookie, `HttpOnly`, `Secure`, `SameSite=Lax`; constant-time invite comparison; invite codes hashed at rest; OIDC with PKCE and issuer verification (*planned*).
- CSRF: `Origin` must match `publicUrl` on every state-changing request and WebSocket upgrade. No CORS.
- Session isolation: `/s/{id}` is served only to the session's owner (any user for the shared local session); at most 6 WebSockets per user; `/internal` session tokens are random 256-bit values, stored as SHA-256, scoped to one session. The proxy strips `Cookie` and `Authorization` and drops `Set-Cookie` from sessiond; `rpc eval` is refused unless author mode is on, and internal ops (`drop-step`) are refused from browsers.
- sessiond answers only its gateway: with `PG_PROXY_SECRET` set, every request but `GET`/`HEAD /healthz` must carry it in `X-Playground-Proxy` (constant-time comparison; the header is not passed on), else 401. The gateway sends it from the proxy, RPC calls and status calls. With the per-session networks (§7), a learner's code cannot drive another learner's Emacs, read their files through `/preview` or call their RPCs.
- Preview content: files are read through `os.Root`, so paths cannot escape the workspace; active content is served with a sandboxing CSP and `nosniff`; the preview iframe is sandboxed.
- Headers: HSTS (via Caddy), `X-Frame-Options: DENY` and `frame-ancestors 'none'` for the app, a strict CSP for the app bundle (self, the fonts path, WebSockets to the public origin, `blob:` for the PDF worker), `nosniff`.
- Rate limits: login attempts (10 per IP and 20 per code each minute); session creation (10 per user and minute); stuck reports (10 per user and hour) whether they come from the browser (`POST /api/v1/stuck`) or from sessiond (`/internal`, reachable by a learner with their own session's token); identical reports within 30 s are merged; a report keeps at most 200 events, 512 KiB of them and a 4000-byte note. Progress reports are accepted only for lessons and steps of the loaded curriculum bundle, so a session cannot grow the store with made-up ids.
- Docker API: only through the socket proxy with the minimal endpoint set; the gateway never runs privileged containers, never mounts host paths into sessions, and only acts on containers, volumes and networks carrying its own labels.

### 9.4 Data and privacy

Event logs stay inside the session (`/work/.playground/events.log`, rotated at 10 MB) unless the learner clicks "I'm stuck", which sends the last 200 events to the gateway. Logs never include file contents, cookie values or tokens. `features.telemetryUpload` is off by default and only ever sends stuck reports to a configured URL. *Planned:* learners export and delete their data (`DELETE /api/v1/me` removes the user, sessions, progress and volumes after a confirmation).

### 9.5 Supply chain

Images are built in CI from pinned base images, pinned toolchain versions (`versions.env`, with checksums) and `emacs.d/packages.lock` (archive tarballs with SHA-256, MELPA tarballs vendored in `emacs.d/vendor/`); each pushed image carries an SBOM and a provenance attestation. *Planned:* `govulncheck` and `pnpm audit` in CI and automated pin updates.

### 9.6 Residual risks

Container escapes through kernel bugs remain possible on any shared host; gVisor reduces the surface. Learners can exhaust their own session; the limits bound the blast radius. The remote box (M6) is a second container per learner with the same profile and an sshd whose only key is the learner's.
