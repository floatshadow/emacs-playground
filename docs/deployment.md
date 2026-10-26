# Deployment, configuration and security

Images, Compose files, the configuration file, networking, storage, operations and the security model for both deployment modes.

## 1. Images

| Image | Built from | Contents | Approximate size |
|---|---|---|---|
| `ghcr.io/ORG/emacs-playground/gateway` | `images/gateway/Dockerfile` | static Go binary with the embedded web bundle, curriculum bundle, fonts | 60 MB |
| `ghcr.io/ORG/emacs-playground/session` | `images/session/Dockerfile` | Emacs 31.1, toolchains, config, sessiond | 5 to 7 GB |
| `ghcr.io/ORG/emacs-playground/remote-box` | `images/remote-box/Dockerfile` | sshd, the same toolchains, no Emacs | 3 GB |

Tags: `vX.Y.Z` for releases, `main-<sha>` for CI builds, `latest` follows releases. The three images of a release share a tag; the gateway refuses a session image whose `org.opencontainers.image.version` label has a different major version.

Build: `docker buildx bake` with a registry cache. The session image is layered so that Emacs, toolchains, Emacs packages and the config are separate layers. CI publishes an SBOM with each image.

## 2. Compose files

`compose.yaml` holds what both modes share; `compose.local.yaml` and `compose.server.yaml` add the mode-specific parts. `.env` selects the mode through `COMPOSE_FILE`, written by `make local` or `make server`, so plain `docker compose up` works afterwards. `.env` holds only `COMPOSE_FILE`, `TAG` and `DOMAIN`; everything else is in `deploy/config.json` (§3).

```yaml
# compose.yaml
name: emacs-playground
services:
  gateway:
    image: ghcr.io/ORG/emacs-playground/gateway:${TAG:-latest}
    build: { context: ., dockerfile: images/gateway/Dockerfile }
    volumes:
      - gateway-data:/data
      - ./deploy/config.json:/etc/playground/config.json:ro
    networks: [front, sessions]
    healthcheck:
      test: ["CMD", "/gateway", "healthcheck"]
      interval: 10s
      timeout: 3s
      retries: 3
    restart: unless-stopped
volumes:
  gateway-data: {}
networks:
  front: {}
  sessions:
    internal: true
```

```yaml
# compose.local.yaml
services:
  gateway:
    ports: ["127.0.0.1:8080:8080"]
    environment: { PG_MODE: local }
    depends_on:
      session: { condition: service_healthy }
  session:
    image: ghcr.io/ORG/emacs-playground/session:${TAG:-latest}
    build: { context: ., dockerfile: images/session/Dockerfile }
    init: true
    environment:
      PG_SESSION_ID: local
      PG_SESSION_TOKEN: local
      PG_GATEWAY_URL: http://gateway:8080
    volumes:
      - ./workspace:/work
    networks: [sessions]
    healthcheck:
      test: ["CMD", "/usr/local/bin/sessiond", "healthcheck"]
      interval: 5s
      timeout: 2s
      retries: 20
    stop_grace_period: 20s
  remote-box:
    image: ghcr.io/ORG/emacs-playground/remote-box:${TAG:-latest}
    build: { context: ., dockerfile: images/remote-box/Dockerfile }
    environment:
      AUTHORIZED_KEYS_FILE: /keys/authorized_keys
    volumes:
      - ./workspace/.playground/ssh:/keys:ro
    networks: [sessions]
```

```yaml
# compose.server.yaml
services:
  caddy:
    image: caddy:2
    ports: ["80:80", "443:443"]
    environment: { DOMAIN: "${DOMAIN}" }
    volumes:
      - ./deploy/Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
    networks: [front]
    restart: unless-stopped
  socket-proxy:
    image: tecnativa/docker-socket-proxy:0.3
    environment: { CONTAINERS: 1, IMAGES: 1, VOLUMES: 1, NETWORKS: 1, EXEC: 1, POST: 1, INFO: 1 }
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
    networks: [docker]
    restart: unless-stopped
  gateway:
    environment: { PG_MODE: server }
    secrets: [oidc_client_secret]
    networks: [front, sessions, docker]
    depends_on: [socket-proxy]
secrets:
  oidc_client_secret:
    file: ./deploy/secrets/oidc_client_secret
volumes:
  caddy-data: {}
networks:
  docker:
    internal: true
```

`deploy/Caddyfile` is in the repository. Caddy obtains certificates automatically for `DOMAIN`; WebSockets pass through `reverse_proxy` unchanged.

In server mode the gateway creates session containers itself with: the configured image, name `pg-session-<id>`, labels `playground.session`, `playground.user`, `playground.version`, the sessions network, volume `pg-ws-<uid>:/work`, env `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_GATEWAY_URL`, and the hardening options from §9. A remote box per learner is created the same way with the learner's public key in `AUTHORIZED_KEYS`.

## 3. Configuration file

The gateway reads one JSON file, `deploy/config.json`, mounted at `/etc/playground/config.json` (path overridable with `PG_CONFIG`). `deploy/config.example.json` is a complete server example; copy it and edit. Precedence, lowest to highest: built-in defaults, the file, environment variables, command-line flags. Environment variables map to file paths by upper-casing and joining with underscores: `PG_SESSIONS_LIMITS_MEMORY=4g` overrides `sessions.limits.memory`. Secrets are referenced as files (`*File` keys) so they can come from Docker secrets or mounted files and never sit in the JSON.

| Key | Default | Meaning |
|---|---|---|
| `mode` | `local` | `local` (one fixed session) or `server` (orchestrator) |
| `listen` | `:8080` | gateway listen address |
| `publicUrl` | `http://localhost:8080` | cookies, OIDC redirect, Origin checks |
| `dataDir` | `/data` | SQLite database, local progress file, curriculum cache, cookie secret |
| `auth.mode` | `none` | `none` (local only), `invite`, `oidc` |
| `auth.invites.file` | | one code per line, with optional label, `uses=N`, `until=YYYY-MM-DD`; see `deploy/invites.example.txt`; codes are hashed in the database on first load |
| `auth.oidc.issuer`, `clientId`, `clientSecretFile`, `allowedEmailDomains` | | OIDC provider; the secret is read from the file |
| `auth.cookieSecretFile` | `<dataDir>/cookie.secret` | HMAC key; generated on first start if missing |
| `auth.sessionDays` | `30` | cookie lifetime |
| `sessions.backend` | `static` in local, `docker` in server | `static` points at `local.sessionUrl` |
| `sessions.dockerHost` | | Engine API endpoint, normally the socket proxy |
| `sessions.image`, `sessions.remoteBox.image` | release images | images for sessions and remote boxes |
| `sessions.network` | | network name for session containers |
| `sessions.runtime` | `runc` | `runsc` for gVisor |
| `sessions.limits.cpus`, `memory`, `pids`, `disk` | `2`, `3g`, `1024`, `4g` | per session; `disk` is a volume size hint, enforced only on storage drivers with quotas |
| `sessions.idleStopMinutes` | `30` | stop a session with no WebSocket for this long |
| `sessions.maxSessions` | `50` | refuse new sessions beyond this |
| `sessions.volumeRetentionDays` | `30` | remove workspace volumes unused for this long |
| `sessions.egress` | `registries` | `none`, `registries`, `all`; see §9 |
| `sessions.remoteBox.enabled` | `true` | provision an SSH box per learner |
| `sessions.gui.enabled`, `geometry` | `true`, `1600x900` | GUI frame availability and size |
| `sessions.terminal.initialSize` | `120x36` | tmux window size until the browser resizes |
| `local.sessionUrl` | `http://session:7000` | the fixed session in local mode |
| `local.workspace` | `./workspace` | documented for the Makefile; Compose mounts it |
| `curriculum.dir`, `defaultLocale` | `/srv/curriculum`, `en` | the built bundle |
| `logging.level`, `format` | `info`, `json` | slog |
| `metrics.enabled`, `listen` | `false`, `:9090` | Prometheus endpoint |
| `features.stuckReports` | `true` | allow "I'm stuck" reports |
| `features.telemetryUpload` | `false` | never on by default |
| `features.authorMode` | `false` | shows condition results and raw state in lessons |

sessiond is configured only by environment (set by the gateway or Compose): `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_GATEWAY_URL`, `PG_LISTEN` (`:7000`), `PG_WORKSPACE` (`/work`), `PG_AGENT_SOCKET`, `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`. The gateway derives the last two from `sessions.gui.geometry` and `sessions.terminal.initialSize`.

A JSON Schema for the file (`deploy/config.schema.json`) is generated from the Go config struct by `playctl config schema`, and `playctl config check deploy/config.json` validates a file without starting anything. The gateway prints its effective configuration at start with secrets redacted.

Minimal local file (everything else defaults):

```json
{ "mode": "local", "auth": { "mode": "none" } }
```

## 4. Local mode

```
git clone https://github.com/ORG/emacs-playground && cd emacs-playground
make local            # writes .env and a minimal deploy/config.json, pulls images (BUILD=1 builds)
docker compose up
open http://localhost:8080
```

- `./workspace` is created on first start and initialised as a git repository. The config lives in `./workspace/emacs.d`.
- `make shell` runs `docker compose exec session tmux attach -t emacs` for bring-your-own-terminal use. The browser keeps showing the lesson and the ticker; its terminal pane shows "attached elsewhere".
- `make reset-workspace` moves `./workspace` aside and starts fresh.
- Development loop: `make dev` runs the Vite dev server with the gateway from source against the Compose session container (backend.md §4, frontend.md §15).

## 5. Server mode

```
cp deploy/config.example.json deploy/config.json   # edit publicUrl, auth, limits
cp deploy/invites.example.txt deploy/invites.txt   # or configure OIDC
echo DOMAIN=emacs.example.edu >> .env
make server
docker compose up -d
```

- Host: Linux with Docker Engine 27 or later, 4 GB RAM plus 1.5 GB per concurrent session, 10 GB disk for images plus 2 to 4 GB per learner. Ports 80 and 443 open. Point `DOMAIN` at the host before the first start so Caddy can obtain a certificate.
- Upgrades: `TAG=vX.Y.Z docker compose pull && docker compose up -d`. Running sessions keep their image until they stop; the gateway shows a "restart to update" chip.
- Monitoring: `GET /healthz`, `/metrics` when enabled (sessions by state, creations, failures, event rates, proxy latency), structured logs on stdout. sessiond has `/healthz` and `/status` inside the network.
- Backups: the gateway data volume nightly (SQLite, small). Workspace volumes are learner data; `playctl export-user <uid>` produces a tarball of a workspace and its progress.
- Capacity: a 96-core, 440 GB host runs about 100 concurrent sessions at the defaults; `sessions.maxSessions` is the guard.

## 6. Remote box

A minimal Debian container with sshd, a `deploy` user and the session toolchains. TRAMP lessons use `/ssh:box:`; `box` is a Host alias in the learner's `~/.ssh/config` written by sessiond with a key pair generated per session. Per learner on servers, shared in local mode. Project P6's deploy task and P5's publish task target it.

## 7. Networking

| From | To | Port | Purpose |
|---|---|---|---|
| internet | Caddy | 443 | HTTPS and WSS |
| Caddy | gateway | 8080 | HTTP |
| gateway | sessiond | 7000 | proxy, health, RPC |
| gateway | socket proxy | 2375 | Engine API |
| sessiond | gateway | 8080 | fixtures, progress, stuck reports |
| session | remote box | 22 | TRAMP |
| session | internet | 443 | package registries only, per `sessions.egress` |

## 8. Storage

| Volume | Content | Backup |
|---|---|---|
| `gateway-data` | SQLite, cookie secret, curriculum cache | nightly |
| `pg-ws-<uid>` (server) or `./workspace` (local) | learner files, config, checkpoints, playground state | by policy |
| `caddy-data` | certificates | optional |

## 9. Security

### 9.1 Trust boundaries and threats

The learner is untrusted: they run arbitrary code inside their session (that is the point). The gateway host and the operator are trusted. Threats, in priority order: a learner escaping the container or reaching another learner's session; a learner reaching the Docker API; resource abuse (mining, fork bombs, disk filling); a learner's workspace content attacking the web app (served previews); account and session theft; supply-chain compromise of images.

### 9.2 Session container profile

Applied by the gateway in server mode (and recommended in `compose.local.yaml` where it does not get in the way):

```
user: learner (uid 1000), no sudo
read_only: true; tmpfs: /tmp (1g, noexec off: builds need it), /run (64m), /home/learner/.cache (2g)
volumes: pg-ws-<uid>:/work only
cap_drop: ALL; security_opt: no-new-privileges:true, seccomp: default, apparmor: docker-default
pids_limit: 1024; cpus: 2; memory: 3g; memory-swap: 3g; ulimits: nofile 65536, nproc 2048
network: sessions (internal) only; no published ports; no Docker socket
runtime: runc, or runsc (gVisor) for public deployments
```

Egress: the sessions network is internal (no default route). `sessions.egress: registries` adds a small egress proxy container (Squid with an allowlist: crates.io and static.crates.io, index.crates.io, opam.ocaml.org and GitHub release URLs for opam, pypi.org and files.pythonhosted.org, packages.typst.org, CTAN mirrors) that sessions reach through `HTTP(S)_PROXY`; `all` gives a NAT route; `none` keeps sessions offline (lessons still work, see emacs.md §9).

### 9.3 Gateway and web application

- Authentication and cookies as in backend.md §2.6: HMAC-signed cookie, `HttpOnly`, `Secure`, `SameSite=Lax`; constant-time invite comparison; invite codes hashed at rest; OIDC with PKCE and issuer verification.
- CSRF: `Origin` must match `publicUrl` on every state-changing request and WebSocket upgrade. No cross-origin resource sharing is enabled.
- Session isolation: `/s/{id}` is served only to the session's owner; per-user WebSocket limit; session tokens for `/internal` are random 256-bit values, hashed in the database, scoped to one session.
- Preview content: files are read through `os.Root` so paths cannot escape the workspace; HTML is served with a sandboxing CSP and `nosniff`; the preview iframe is sandboxed; nothing in a preview can read the app's cookies or call the API with the learner's credentials beyond what the learner could do anyway.
- Headers: HSTS (via Caddy), `X-Frame-Options: DENY` for the app, a strict CSP for the app bundle (self plus the fonts path, `wss:` for WebSockets, `blob:` for the PDF worker).
- Rate limits: login attempts per IP and per code; session creation per user; stuck reports per hour.
- Docker API: only through the socket proxy with the minimal endpoint set; the gateway never runs privileged containers, never mounts host paths into sessions, and only acts on containers carrying its own labels.

### 9.4 Data and privacy

Event logs stay inside the session (`/work/.playground/events.log`, rotated at 10 MB) unless the learner clicks "I'm stuck", which sends the last 200 events to the gateway. Logs never include file contents, cookie values or tokens. `features.telemetryUpload` is off by default and documented as sending stuck reports to a configured URL only. Learners can export and delete their data (`DELETE /api/v1/me` removes the user, sessions, progress and volumes after a confirmation).

### 9.5 Supply chain

Images are built in CI from pinned base images, pinned toolchain versions and `packages.lock` with checksums; an SBOM and provenance attestation are published with each image; `govulncheck` and `pnpm audit` run in CI; Dependabot or Renovate keeps pins current. Emacs packages are installed from archives over HTTPS with the lock's checksums verified.

### 9.6 Residual risks

Container escapes through kernel bugs remain possible on any shared host; gVisor reduces the surface. Learners can exhaust their own session (that is their problem and the limits bound the blast radius). The remote box is a second container per learner with the same profile and an sshd; its only key is the learner's.
