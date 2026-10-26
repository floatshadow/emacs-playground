# Emacs Playground

A browser-based, hands-on Emacs course around a real, fully configured Emacs running in Docker. Real projects in Rust, OCaml, Python, Markdown, Typst and LaTeX; a live key ticker that explains every chord; a panel that always knows how to get you out of a confusing state; checkpoints to retry any step.

Status: design complete, implementation starting.

- `docs/DESIGN.md`: the overview and reading guide: goals, system at a glance, technology choices, decision records, roadmap.
- `docs/architecture.md`, `docs/deployment.md`, `docs/backend.md`, `docs/api.md`, `docs/emacs.md`, `docs/frontend.md`, `docs/CURRICULUM.md`, `docs/third-party.md`: one document per concern.
- `deploy/config.example.json`: the deployment configuration file; `deploy/Caddyfile`, `deploy/invites.example.txt`.
- `design/demo/`: a clickable design demo. Open `design/demo/index.html` in a browser, no server needed. The Emacs pane is a simulation of about sixty commands. Open the lesson in a given state with a hash, for example `lesson.html#play=C-x%20C-b`.
- `design/screenshots/`: rendered views of the demo.
