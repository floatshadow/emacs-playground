# Emacs Playground

A browser-based, hands-on Emacs course around a real, fully configured Emacs running in Docker. Real projects in Rust, OCaml, Python, Markdown, Typst and LaTeX; a live key ticker that explains every chord; a panel that always knows how to get you out of a confusing state; checkpoints to retry any step.

Status: first slice (Parts 0 to 3 of the course, Rust and Python toolchains, terminal and GUI frames, local mode). The rest of the course and server-mode hardening are on the roadmap in `docs/DESIGN.md` §10.

## Run it on your machine

Requirements: Docker Engine 27 or later with Compose v2, about 6 GB of disk for the images, a modern browser. The images are prebuilt and published to the GitHub Container Registry, so nothing is compiled locally.

```sh
git clone https://github.com/floatshadow/emacs-playground
cd emacs-playground
make local          # writes .env and a minimal deploy/config.json, pulls the images
docker compose up   # the first start takes about half a minute
```

Then open http://localhost:8080. Your files and your growing Emacs configuration live in `./workspace`, which survives restarts; `make reset-workspace` starts over. `make shell` attaches your own terminal to the same Emacs.

Without `make`:

```sh
printf 'COMPOSE_FILE=compose.yaml:compose.local.yaml\nTAG=latest\n' > .env
printf '{ "mode": "local", "auth": { "mode": "none" } }\n' > deploy/config.json
mkdir -p workspace/.playground/ssh && chmod 777 workspace workspace/.playground workspace/.playground/ssh
docker compose pull && docker compose up
```

To build the images yourself: `BUILD=1 make local` (the session image compiles Emacs from source; 10 to 60 minutes depending on the machine).

## Run it for a group

```sh
cp deploy/config.example.json deploy/config.json   # set publicUrl, auth, limits
cp deploy/invites.example.txt deploy/invites.txt   # or configure OIDC
make server DOMAIN=emacs.example.edu
docker compose up -d
```

Caddy obtains the TLS certificate; the gateway starts one isolated session container per learner. Configuration reference, operations and the security model: `docs/deployment.md`.

## Images

| Image | Contents |
|---|---|
| `ghcr.io/floatshadow/emacs-playground/session` | Emacs 31 built from source, toolchains, the course configuration, sessiond |
| `ghcr.io/floatshadow/emacs-playground/remote-box` | SSH target with the same toolchains, for the remote-editing lessons |
| `ghcr.io/floatshadow/emacs-playground/gateway` | the web app, the gateway, the curriculum bundle and fonts |

Built by `.github/workflows/image-emacs.yml` (session, remote-box) and `image-web.yml` (gateway). Tags: `latest` and `main` follow the main branch, `main-<sha>` pins a commit, `X.Y.Z` and `X.Y` are releases (`release.yml` on a `vX.Y.Z` tag). Set `TAG=` in `.env` to pin one; set `IMAGE_BASE=ghcr.io/<owner>/<repo>` to pull a fork's images.

## Contributing

- `AGENTS.md`: how to build, test and run every component, who owns what, and the invariants to respect.
- `docs/DESIGN.md`: the design overview and reading guide to the other documents in `docs/`; `docs/notes.md` holds developer workflow notes, longer commands and known limitations; `docs/history/` keeps completed reviews and fix plans.
- `design/demo/`: the clickable design demo (open `index.html`); `design/screenshots/` its rendered views.
- `deploy/`: configuration examples, the Caddyfile, the invites example.

License: GPL-3.0-or-later for the Emacs configuration and lessons. The repository does not yet have a top-level `LICENSE` file covering the rest.
