# Emacs Playground

A hands-on Emacs course in the browser, around a real, fully configured Emacs running in Docker: real projects, a key ticker that explains every chord, a panel that gets you out of confusing states, and checkpoints to retry any step.

Status: the whole course (Parts 0 to 14 and projects P1 to P8), every toolchain, terminal and GUI frames, the per-session remote box, local mode; server mode is experimental. Roadmap: `docs/DESIGN.md` §10.

## Run it on your machine

Needs Docker Engine 27+ with Compose v2, about 6 GB of disk and a modern browser. Images are prebuilt on GHCR.

```sh
git clone https://github.com/floatshadow/emacs-playground
cd emacs-playground
make local          # writes .env and deploy/config.json, creates ./workspace, pulls the images
docker compose up   # first start takes about half a minute
```

Open http://localhost:8080. Your files and growing Emacs configuration live in `./workspace` (`make reset-workspace` starts over); `make shell` attaches your terminal to the same Emacs.

Without `make`:

```sh
printf 'COMPOSE_FILE=compose.yaml:compose.local.yaml\nTAG=latest\n' > .env
printf '{ "mode": "local", "auth": { "mode": "none" } }\n' > deploy/config.json
mkdir -p workspace/.playground/ssh && chmod 777 workspace workspace/.playground workspace/.playground/ssh
docker compose pull && docker compose up
```

`BUILD=1 make local` builds the images yourself (Emacs compiles from source: 10 to 60 minutes).

## Run it for a group

```sh
cp deploy/config.example.json deploy/config.json   # set publicUrl, auth, limits
cp deploy/invites.example.txt deploy/invites.txt   # or configure OIDC
make server DOMAIN=emacs.example.edu
docker compose up -d
```

Caddy obtains the TLS certificate; the gateway starts one isolated session container per learner. See `docs/deployment.md`.

## Images

`ghcr.io/floatshadow/emacs-playground/` + `session` (Emacs 31, toolchains, the course configuration, sessiond), `remote-box` (SSH target for the remote-editing lessons), `gateway` (web app, gateway, curriculum bundle, fonts). Tags: `latest` follows main, the short commit sha pins a commit, `vX.Y.Z` is a release. Pin with `TAG=` in `.env`; `IMAGE_BASE=ghcr.io/<owner>/<repo>` pulls a fork's images.

## Contributing

Start with `AGENTS.md` (build, test, ownership, invariants) and `docs/DESIGN.md` (design and reading guide). `design/demo/` is the clickable design demo; `deploy/` holds configuration examples and the Caddyfile.

License: GPL-3.0-or-later for the Emacs configuration and lessons; there is no top-level `LICENSE` file for the rest yet.
