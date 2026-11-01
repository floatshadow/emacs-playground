# Deployment, configuration and security

*Planned* items carry a `TODO(<area>, deployment.md §N)` in the code.

## 1. Images

| Image (`ghcr.io/floatshadow/emacs-playground/…`) | Contents |
|---|---|
| `gateway` | distroless; gateway with the embedded web bundle (`-tags embedweb`), the curriculum bundle (`/srv/curriculum`, strict build), web fonts (`/srv/fonts`) |
| `session` | Emacs 31.1 (native compilation), tmux, TigerVNC, toolchains, pinned packages, sessiond; about 5.8 GB |
| `remote-box` | sshd, sudo, nginx, Emacs with the pinned packages, the session's Rust and Python with the prefetched crates and wheels |

Dockerfiles: `images/<name>/Dockerfile`, context the repository root. Pins: third-party.md; `images/session/versions.env` is reported by sessiond's `/status`.

CI: `ci.yml` (Go: gofmt, vet, build, race tests, protocol drift, `playctl curriculum validate`; web: lint, format, typecheck, tests, build, size) and `images.yml` (a matrix job per image, then `images/<image>/test/ci-smoke.sh`: session ERT suites, the remote box's tools, gateway in local mode serving the curriculum; a cold session build takes over an hour). Pull requests build without pushing and smoke-test only the gateway; no provenance or SBOM attestations; session and remote-box build caches live in the `buildcache` package.

Tags: the short commit sha and `latest` for pushes to main; `vX.Y.Z` for release tags. Forks set `IMAGE_BASE` in `.env`. *Planned:* the gateway refuses a session image of another major version; `playctl images build` uses `docker buildx bake` with a registry cache.

## 2. Compose files

| File | Adds |
|---|---|
| `compose.yaml` | gateway with `gateway-data` and `deploy/config.json`; networks `front`, internal `sessions` |
| `compose.local.yaml` | local mode on `127.0.0.1:8080`; session (`./workspace:/work`, id and token `local`); remote box (alias `box`) |
| `compose.server.yaml` | server mode; Caddy (80, 443); Docker socket proxy on the internal `docker` network; OIDC client secret |
| `compose.dev.yaml` | `make dev`: session on the host network, `127.0.0.1:7000`; remote box on `127.0.0.1:2222` |

`.env` holds only `COMPOSE_FILE`, `TAG`, `IMAGE_BASE`, `DOMAIN`. Never set `hostname:` on the gateway in server mode: it finds its own container by hostname (§7). Caddy answers `/internal/*` with 404; the server-mode gateway also refuses `/internal` requests with forwarding headers.

Server-mode session containers (backend.md §2.3) get the §9.2 profile and `PG_SESSION_ID`, `PG_SESSION_TOKEN`, `PG_PROXY_SECRET`, `PG_GATEWAY_URL`, plus `PG_VNC_GEOMETRY`, `PG_TERM_SIZE`, `PG_GUI_ENABLED` and `PG_AUTHOR_MODE` from the config and, with a box, `PG_BOX_HOST` and `PG_BOX_KEY` (§6).

## 3. Configuration file

File: `-config`, else `$PG_CONFIG`, else `/etc/playground/config.json`. Precedence: defaults < file < `PG_*` environment < flags (`-set key.path=value`). Environment names split camelCase (`sessions.limits.memory` → `PG_SESSIONS_LIMITS_MEMORY`); unsplit names and `config.Aliases` also work. Lists: comma-separated or JSON (`PG_SESSIONS_GROUP_LIMITS='[{"group":"cs101","maxActiveSessions":30}]'`). Unknown keys are errors; secrets are files (`*File` keys). All keys and defaults: `config.Defaults`, the generated `deploy/config.schema.json`, `deploy/config.example.json`; `gateway config` prints the effective configuration, secrets redacted.

Keys with non-obvious behaviour:

| Key | Default | Notes |
|---|---|---|
| `publicUrl` | `http://localhost:8080` | cookies, OIDC redirect, Origin checks; https in server mode unless loopback; locally `localhost` ≡ `127.0.0.1` |
| `auth.mode` | `none` local, `oidc` server | `none` is refused in server mode |
| `auth.oidc.issuer`, `clientId` | | required with `oidc`; discovery at `<issuer>/.well-known/openid-configuration`; https unless loopback or `allowInsecureIssuer` |
| `auth.oidc.clientSecretFile`, `scopes`, `displayName`, `providerLogout` | none, `openid email profile`, `SSO`, `true` | no secret: public client (PKCE); add scope `groups` if the provider needs it; "Sign in with <displayName>"; logout continues at the provider's `end_session_endpoint`, if any |
| `auth.oidc.usernameClaim`, `groupsClaim` | `preferred_username` (else `email`), `groups` | groups: list or string, ID token only |
| `auth.oidc.allowedEmailDomains`, `allowedGroups` | any | domains need `email_verified` (Entra ID omits it: use groups); one match suffices |
| `sessions.backend` | `static` local, `docker` server | |
| `sessions.isolation` | `network` | per-session networks (§7); `shared` uses `sessions.network` |
| `sessions.subnetPool` | `10.231.0.0/16` | per-session `/28`s; empty falls back to the daemon's pools (about 30 networks) |
| `sessions.gatewayContainer` | own hostname | joins every session network |
| `sessions.runtime` | `runc` | `runsc` for gVisor |
| `sessions.limits` | 2 CPUs, `3g`, 1024 pids, `4g` disk | every session; disk only with storage-driver quotas |
| `sessions.maxSessions` | `50` | active sessions overall (`429 session_limit`) |
| `sessions.groupLimits` | `[]` | ordered `{group, maxActiveSessions}` (0: unlimited); a learner counts against their first listed group (`429 group_quota_exceeded`) |
| `sessions.defaultGroupMaxActiveSessions` | `0` (none) | shared pool of learners in no listed group |
| `sessions.egress` | `registries` | §9.2 |
| `local.sessionId`, `sessionToken` | `local` | must match `compose.local.yaml` |
| `features.authorMode` | `false` | raw conditions and state, `rpc eval`, hot reload |

sessiond reads environment only (backend.md §3.13). `PG_PROXY_SECRET` is derived per session (backend.md §2.3) from `<dataDir>/session-proxy.secret`, a key separate from the cookie secret so rotating cookies does not lock out running sessions; locally it is unset.

## 4. Local mode

`make local && docker compose up`, then http://localhost:8080 (`BUILD=1` builds instead of pulling).

`./workspace` becomes a git repository and receives the image's Emacs configuration on first start (emacs.md §3); `make local` makes it world-writable for uid 1000. `make shell` attaches a native terminal to the browser's frame.

## 5. Server mode (experimental)

Learners sign in with OpenID Connect (backend.md §2.6). Register a client, confidential or public, with redirect URI `https://<DOMAIN>/api/v1/auth/oidc/callback` and post-logout URI `https://<DOMAIN>/login`; ID tokens carry `sub`, and `email_verified` or the groups claim for rules that use them. Run `make server DOMAIN=… OIDC_ISSUER=… OIDC_CLIENT_ID=…` (writes a minimal `deploy/config.json` unless it is a server config already, then checks it with `playctl config check`), put any client secret in `deploy/secrets/oidc_client_secret` (kept 644 for the gateway's uid 65532), then `docker compose up -d`. Allow-lists and quotas (all keys: `deploy/config.example.json`):

```json
{"auth": {"oidc": {"issuer": "https://idp.example.edu/realms/main", "clientId": "emacs-playground",
   "scopes": ["openid","email","profile","groups"], "allowedEmailDomains": ["example.edu"]}},
 "sessions": {"groupLimits": [{"group":"cs101","maxActiveSessions":30}], "defaultGroupMaxActiveSessions": 5}}
```

- Host: 4 GB RAM plus about 1.5 GB and a quarter core per concurrent session; 10 GB disk plus 2–4 GB per learner; ports 80 and 443; `DOMAIN` resolving before first start.
- Upgrade: `TAG=vX.Y.Z docker compose pull && docker compose up -d`; running sessions keep their image until they stop.
- Monitoring: `/healthz`, optional Prometheus, JSON logs.
- Backups: §8; *planned:* `playctl export-user`.
- `images/gateway/test/server-mode.sh <image>` runs the server stack with Dex: sign-in, allow-lists, group quotas, isolation, gateway replacement, logout.

## 6. Remote box

sshd, a `deploy` user (passwordless sudo, for `/ssh:box|sudo::`), Emacs with the pinned packages (lesson 13.7), offline Rust and Python like the session's (emacs.md §9), nginx serving `/srv/www`; TRAMP lessons use `/ssh:box:`, P5 deploys to `/srv/www`, P6 to `/srv/app`. Each session has an ed25519 key pair in `/work/.playground/ssh` and a `box` alias in `~/.ssh/config` (`HostName` `PG_BOX_HOST`, user `deploy`, `BatchMode`, no host-key checks: a re-created box has new host keys), so `ssh box` and TRAMP never prompt; `/status` reports `remoteBox` once written. Locally sessiond generates the pair, and the one box started with the stack reads `authorized_keys` from that directory (read-only) at every login. On servers each session gets its own box with a fresh pair (backend.md §2.3, public half as `AUTHORIZED_KEYS`), keeping nothing across re-creation.

## 7. Networking

| From → To | Port | Purpose |
|---|---|---|
| internet → Caddy → gateway | 443 → 8080 | HTTPS, WSS |
| gateway → sessiond | 7000 | proxy, RPC, status; proxy secret on all but `/healthz` |
| gateway → socket proxy | 2375 | Engine API (allows containers, images, volumes, networks, exec, info and `POST`) |
| sessiond → gateway | 8080 | `/internal`, bearer session token |
| session → remote box | 22 | TRAMP |
| session → internet | 443 | per `sessions.egress` |

With `isolation: network`, each session is alone with its box on `pg-net-<id>`, created before its container, `internal` unless egress is `all`, removed with it; the gateway joins as `gateway` (re-joining and pruning: backend.md §2.3). Sessions reach only the gateway and their box.

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
tmpfs /tmp (1g, exec: builds), /run (64m, noexec), /home/learner/.cache (2g; `~/.ssh` links to `~/.cache/ssh`)
only pg-ws-<user>:/work; no Docker socket, no published ports
cap_drop ALL; no-new-privileges; default seccomp and AppArmor
sessions.limits; memory-swap = memory; nofile 65536; restart none; stop timeout 20 s
```

No `nproc` ulimit: it counts per uid across the host, and every session is uid 1000; `pids_limit` bounds each container.

Egress: session networks have no route unless `egress: all` (NAT, and cargo and uv go online); lessons work offline. *Planned:* `registries` adds an allowlist proxy (crates.io, opam, PyPI, Typst packages, CTAN) via `HTTP(S)_PROXY`; until then it behaves like `none`.

### 9.3 Gateway and web application

- Login (backend.md §2.6): code flow with PKCE, state and nonce in a signed 10-minute cookie, verified ID tokens; allow-lists rechecked per request; logout revokes all the user's cookies. CSRF: `Origin` must match `publicUrl` on state-changing requests and WebSocket upgrades; no CORS.
- `/s/{id}` only for its owner, through the proxy of backend.md §2.5 (credentials stripped, `rpc eval` only in author mode, internal ops refused); `/internal` tokens are random 256-bit, stored hashed, per session.
- sessiond requires the proxy secret (backend.md §3.13); with §7, one learner cannot reach another's Emacs, previews or RPCs.
- Previews: read through `os.Root`; active content gets a sandboxing CSP, `nosniff`, a sandboxed iframe. Open (DESIGN.md §12): the CSP keeps `allow-same-origin`, so a learner's HTML opened directly can make credentialed requests to `/api` and `/s`; closing it needs a separate origin or dropping that flag.
- App headers (`internal/gateway/static.go`): strict CSP, `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`; HSTS via Caddy.
- Rate limits: `POST /api/v1/sessions` 10 per user a minute, made once per page load; stuck reports 10 per user an hour (browser and `/internal`; caps in api.md §2.5). Progress is accepted only for steps in the loaded bundle.
- Docker only through the socket proxy; no privileged containers or host mounts; only labelled objects.

### 9.4 Data and privacy

Event logs stay in the session (`/work/.playground/events.log`, rotated at 10 MB) except the last 200 sent with "I'm stuck"; they never hold file contents or secrets. *Planned:* `features.telemetryUpload` (off by default) also sends stuck reports to a configured URL; data export and `DELETE /api/v1/me`.

### 9.5 Supply chain and repository hygiene

CI builds from pinned bases, `versions.env` (checksummed) and `packages.lock` (SHA-256, every tarball vendored). *Planned:* `govulncheck`, `pnpm audit`, automated pin updates. Open: no LICENSE file; `deploy/config.json` is tracked although `make local` writes it.

### 9.6 Residual risks

gVisor narrows kernel escapes. A learner can exhaust only their own session. The server-mode box (`docker.HardenBox`) runs sshd as root with all capabilities dropped except `CHOWN DAC_OVERRIDE FOWNER SETUID SETGID SYS_CHROOT AUDIT_WRITE KILL NET_BIND_SERVICE` (sshd, sudo, nginx), 1 CPU, 2 GB, 1024 processes, alone with its session on `pg-net-<id>`.
