# Emacs Playground

A browser-based, hands-on Emacs course around a real, fully configured Emacs running in Docker. Real projects in Rust, OCaml, Python, Markdown, Typst and LaTeX; a live key ticker that explains every chord; a panel that always knows how to get you out of a confusing state; checkpoints to retry any step.

Status: first slice implemented (Parts 0 to 3 of the course, Rust and Python toolchains, terminal and GUI frames, local mode). The rest of the course and the server mode hardening are tracked in `docs/DESIGN.md` §10.

## Run it on your machine

Requirements: Docker Engine 27 or later with Compose v2, about 6 GB of disk for the images, a modern browser. Images are prebuilt by GitHub Actions and published to the GitHub Container Registry, so nothing is compiled locally.

```
git clone https://github.com/floatshadow/emacs-playground
cd emacs-playground
make local          # writes .env and a minimal deploy/config.json, pulls the images
docker compose up   # first start takes about half a minute
```

Then open http://localhost:8080. Your files and your growing Emacs configuration live in `./workspace`, which survives restarts; `make reset-workspace` starts over. `make shell` attaches your own terminal to the same Emacs.

No `make`? The equivalent is:

```
printf 'COMPOSE_FILE=compose.yaml:compose.local.yaml\nTAG=latest\n' > .env
printf '{ "mode": "local", "auth": { "mode": "none" } }\n' > deploy/config.json
mkdir -p workspace && docker compose pull && docker compose up
```

To build the images yourself instead of pulling them: `BUILD=1 make local` (the Emacs image compiles Emacs from source; expect 10 to 60 minutes depending on the machine).

## Run it for a group

```
cp deploy/config.example.json deploy/config.json   # set publicUrl, auth, limits
cp deploy/invites.example.txt deploy/invites.txt   # or configure OIDC
echo DOMAIN=emacs.example.edu >> .env
make server
docker compose up -d
```

Caddy obtains the TLS certificate; the gateway starts one isolated session container per learner. Details, the configuration reference and the security model are in `docs/deployment.md`.

## Images

| Image | Built by | Contents |
|---|---|---|
| `ghcr.io/floatshadow/emacs-playground/session` | `.github/workflows/image-emacs.yml` | Emacs 31 built from source, toolchains, the course configuration, sessiond |
| `ghcr.io/floatshadow/emacs-playground/remote-box` | `.github/workflows/image-emacs.yml` | SSH target with the same toolchains, for the remote-editing lessons |
| `ghcr.io/floatshadow/emacs-playground/gateway` | `.github/workflows/image-web.yml` | the web app, the gateway, the curriculum bundle and fonts |

Tags: `latest` and `main` follow the main branch, `main-<sha>` pins a commit, `X.Y.Z` and `X.Y` are releases (`.github/workflows/release.yml` on a `vX.Y.Z` tag). Set `TAG=` in `.env` to pin one. Forks publish under their own owner; set `IMAGE_BASE=ghcr.io/<owner>/<repo>` in `.env` to pull from a fork.

## Repository map

- `docs/DESIGN.md`: the overview and reading guide: goals, system at a glance, technology choices, decision records, roadmap. One document per concern beside it: architecture, deployment, backend, API, Emacs, frontend, curriculum, third-party.
- `docs/feedback/`: usability reviews and fix plans.
- `design/demo/`: the clickable design demo (open `index.html`); `design/screenshots/` its rendered views.
- `deploy/`: the configuration example, the Caddyfile, the invites example.
- `AGENTS.md`: how to build, test and run every component when contributing.

License: GPL-3.0-or-later for the Emacs configuration and lessons; see `LICENSE` for the rest.
