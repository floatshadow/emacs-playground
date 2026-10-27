# Emacs Playground

A browser-based, hands-on Emacs course around a real, fully configured Emacs running in Docker. Real projects in Rust, OCaml, Python, Markdown, Typst and LaTeX; a live key ticker that explains every chord; a panel that always knows how to get you out of a confusing state; checkpoints to retry any step.

Status: slice 1 integrated: local mode runs Parts 0 to 3 of the course (28 lessons) end to end in the browser, with the terminal and GUI frames, the live panels, checkpoints and the preview pane. Server mode (docker backend) is experimental; OIDC and Parts 4 to 14 are later work. `AGENTS.md` explains how to build, test and run every component.

Try it locally (Docker and Compose; the images are built from this repository):

```sh
BUILD=1 make local      # writes .env, creates ./workspace, builds gateway, session and remote-box images
docker compose up -d    # then open http://localhost:8080
```

Your files and your growing Emacs configuration live in `./workspace`; `make shell` attaches a native terminal to the same Emacs.

- `docs/DESIGN.md`: the overview and reading guide: goals, system at a glance, technology choices, decision records, roadmap.
- `docs/architecture.md`, `docs/deployment.md`, `docs/backend.md`, `docs/api.md`, `docs/emacs.md`, `docs/frontend.md`, `docs/CURRICULUM.md`, `docs/third-party.md`: one document per concern.
- `deploy/config.example.json`: the deployment configuration file; `deploy/Caddyfile`, `deploy/invites.example.txt`.
- `design/demo/`: a clickable design demo. Open `design/demo/index.html` in a browser, no server needed. The Emacs pane is a simulation of about sixty commands. Open the lesson in a given state with a hash, for example `lesson.html#play=C-x%20C-b`.
- `design/screenshots/`: rendered views of the demo.
