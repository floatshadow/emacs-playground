# Implementation notes

Developer workflow, longer commands, the toolchain, the slice-1 scope and the known limitations. How the system behaves is specified in the other documents of `docs/` (§1 maps the topics that used to be collected here); the code is authoritative where they differ. The rules for contributors (ownership, environment, the main commands) are in `AGENTS.md`.

## 1. Where the details live

| Topic | Document |
|---|---|
| Module path, package layout, interfaces | backend.md §1, §2.3 |
| Lesson ids (one slash, literal in URLs), error codes, origins | api.md §1 |
| Endpoints, RPC ops, events, step statuses, agent ops and replies | api.md §2 to §7 |
| TypeScript generation from Go (`tsgen`) | api.md §8, DESIGN.md ADR-019 |
| Condition and stuck-rule ids, condition kinds, layout patterns, demo strings | CURRICULUM.md B.3, api.md §6 |
| Predicates, virtual roots (`otherWindows`, `selectedWindow`, `session`), exit-table regexes | CURRICULUM.md B.4, frontend.md §6 |
| Configuration keys, precedence, `PG_*` names and aliases, Compose files | deployment.md §2, §3 |
| Proxy secret, per-session networks, hardening, stores, curriculum serving, progress | backend.md §2, deployment.md §7, §9 |
| Rate limits, stuck-report bounds | deployment.md §9, api.md §2.5 |
| sessiond environment, supervisor, input tap, step engine, checkpoints, scenario loader ("Go to this step", `setup.reset`) | backend.md §3 |
| playctl commands (`curriculum play` flags and output) | backend.md §4, CURRICULUM.md B.5 |
| Session image, tmux, terminal keys, GUI frame, saving, module unlocking | emacs.md §1 to §7 |
| Agent hooks, the learner's frame (the GUI frame while one exists), windows following the learner between the terminal and GUI frames, waiting loops (`waiting`) and Rescue, echo deduplication, `snapshot`, `open`, `reset-step`, `setup`, side windows, C-g during redisplay | emacs.md §10 |
| Demo idle end and abandoned demos, the cold-start resume, checkpoint restores on a temporary index, the prefix list | api.md §4.1, backend.md §3.2, §3.6, §3.9 |
| Go to this step (veil, quiet keys), finished and displaced lessons, offered locales, the top bar at narrow widths | frontend.md §7, §11, §12 |
| Extended keys in the browser, demo player input, nudge suppression, progress resets from another tab | frontend.md §2, §5, §6 |
| Authoring rules (modules only grow, demos from the checkpoint, builds, `C-g` after an edit) | CURRICULUM.md B.3 |
| Every dependency, its pin and why | third-party.md |

## 2. Development workflow

- **Config files:** `deploy/config.json` is the minimal local file (written by `make local`); `deploy/config.dev.json` is for `make dev` (listen `127.0.0.1:8080`, `publicUrl` `http://localhost:5173` so the Vite-proxied Origin passes, data and curriculum under `.dev/`, author mode on); `deploy/config.example.json` is the complete server example.
- **sessiond on the host** (no Emacs needed to print its configuration): `PG_WORKSPACE=$HOME/pg-work PG_RUN_DIR=$HOME/pg-run go run ./cmd/sessiond config`; `sessiond -fake` runs the whole daemon with bash in place of Emacs, tmux and Xvnc.
- **Emacs configuration changes:** the configuration is baked into the session image (`/opt/playground/emacs.d`; the agent loads from there) and copied to `./workspace/emacs.d` on first start only. After changing `emacs.d/`, rebuild the session image (about 15 s for a config-only change) and `make reset-workspace`. After editing the agent's docstrings, byte-compile it with `byte-compile-error-on-warn`.
- **Fast iteration on the running stack without rebuilding images:** build `CGO_ENABLED=0` binaries into a directory the Docker daemon can see, add a Compose overlay that bind-mounts them over `/gateway` and `/usr/local/bin/sessiond` (and `web/dist` with `PG_WEB_DIR`, a built curriculum over `/srv/curriculum`), then `docker compose -f compose.yaml -f compose.local.yaml -f <overlay> up -d --force-recreate`.
- **Web without the gateway:** `cd web && pnpm preview --port 8080 --host 127.0.0.1 --strictPort` serves a built bundle; `pnpm dev:fake` runs the fake session (`web/dev/README.md`) that `pnpm e2e:lesson` walks.
- Validate Compose with `docker compose -f compose.yaml -f compose.local.yaml config -q`.
- When a sandbox refuses `docker run`/`docker exec` with inline shell code, put the code in a script file.

## 3. Longer commands and test tools

```sh
go run ./cmd/gateway config -config deploy/config.json                 # effective config, secrets redacted
go run ./cmd/gateway -config deploy/config.json -set sessions.limits.memory=4g -listen :9000
go run ./cmd/gateway -config deploy/config.dev.json -web web/dist -fonts .dev/fonts   # also PG_WEB_DIR, PG_FONTS_DIR
go run ./cmd/playctl protocol typescript -o web/src/protocol.gen.ts [-check]
go run ./cmd/playctl config check deploy/config.json
go run ./cmd/playctl curriculum build curriculum -o artifacts/curriculum [-lenient]
go run ./cmd/playctl fonts -o .dev/fonts
go test -tags dockerintegration ./internal/docker/                     # docker backend against the real Engine
images/gateway/test/server-mode.sh ghcr.io/floatshadow/emacs-playground/gateway:dev   # server mode, from the repo root
```

`server-mode.sh` checks network isolation (a session is unreachable from the shared network and answers 401 without the secret on its own network) and the re-attach after the gateway container is replaced.

**sessiond against the real image** (host network):

```sh
docker run -d --name pg-it --network host --tmpfs /run:mode=1777 --tmpfs /tmp:mode=1777 --tmpfs /work:mode=1777,exec \
  -e PG_LISTEN=127.0.0.1:17200 -e PG_GATEWAY_URL=http://127.0.0.1:17180 -e PG_AUTHOR_MODE=true \
  ghcr.io/floatshadow/emacs-playground/session:dev
PG_SESSIOND_URL=http://127.0.0.1:17200 PG_TEST_FAKE_GATEWAY=127.0.0.1:17180 PG_TEST_GUI=1 \
  go test -tags integration ./internal/sessiond/ -run Integration -v
docker rm -f pg-it
```

Without `PG_SESSIOND_URL` the test starts `PG_SESSION_IMAGE` through testcontainers but skips the lesson and GUI parts (a bridge-networked container cannot reach the in-test gateway).

**Emacs tests by hand** (what `make emacs-test` runs):

```sh
docker run --rm --entrypoint sh ghcr.io/floatshadow/emacs-playground/session:dev -c 'cd /opt/playground/emacs.d && emacs --batch -L lisp -l playground-agent -l test/agent-test.el -f ert-run-tests-batch-and-exit'
docker run --rm --entrypoint sh ghcr.io/floatshadow/emacs-playground/session:dev -c 'cd /opt/playground/emacs.d && emacs --batch -l early-init.el -l test/config-test.el -f ert-run-tests-batch-and-exit'
```

**Browser scripts** (`web/e2e/`, playwright-core over CDP; environment `PG_CDP`, default `http://127.0.0.1:9222`, and `PG_URL`, default `http://127.0.0.1:8080`):

- `smoke.mjs` (`make e2e`, `pnpm e2e:smoke`): eleven scenarios against the compose stack: services healthy, course map, lesson 2.3 with the ticker, Where am I, Watch it as a demonstration followed by the learner's own keys, Reset this step through its dialog, the GUI frame, the Markdown preview of lesson 3.8, `C-x C-b` and Rescue in lesson 0.2, `playctl curriculum play` for Parts 0-3 and `play -watch` for Part 0. Besides the DOM it checks the `/events` frames the page receives and the workspace files on disk (`PG_WORKSPACE`, default `./workspace`). Scenario 6 (`C-x C-b`) sets Part 0's modules (`base agent`) and runs after scenario 9, because from Part 2 on `C-x C-b` is ibuffer in the same window. It resets the progress of lessons 0.2, 2.3 and 3.8 first, so it can be re-run, and ends with Start over. Output: `artifacts/verify/<n>.png` and `results.json`. `PG_SMOKE_ONLY=3,4` runs a subset; `PG_SMOKE_PLAY=0` skips the replays.
- `review.mjs`: one acceptance check per UX contract, each in a fresh browser context; about ten minutes for all. Checks are named by the ids of the review they came from (`docs/history/2026-10-27-fix-plan.md`), so `PG_REVIEW_ONLY=B1,B4` runs a subset. It writes `artifacts/review/<id>.png` and `results.json`, paces page loads to at most eight a minute (the gateway allows ten session requests a minute) and resets all progress at the end.
- `lesson.mjs` (`pnpm e2e:lesson`): the same kind of walk against the fake session; `shots.mjs`: screenshots of every screen; `demo-focus.mjs`: the demo player keeps playing when the terminal loses focus.

## 4. Toolchain

Fresh machine (Go into `~/.local/go`, pnpm through a corepack shim in `~/.local/bin`):

```sh
# Go 1.26.x (take the sha256 from https://go.dev/dl/?mode=json)
mkdir -p ~/.cache/go-dl && cd ~/.cache/go-dl
curl -sSLO https://go.dev/dl/go1.26.10.linux-amd64.tar.gz
echo "ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5  go1.26.10.linux-amd64.tar.gz" | sha256sum -c -
tar -C ~/.local -xzf go1.26.10.linux-amd64.tar.gz

corepack enable --install-directory ~/.local/bin pnpm
corepack install -g pnpm@11.28.8
```

pnpm 11 enforces a minimum release age for new versions; when it adds entries to `web/pnpm-workspace.yaml` (`minimumReleaseAgeExclude`), commit them with the lockfile. Builds need network access to GitHub, go.dev, npm, GNU and NonGNU ELPA, MELPA, Docker Hub and GHCR. Pinned versions and the reasons for them (TypeScript 5.9, ESLint 9, the xterm.js betas re-pinned together) are in third-party.md.

## 5. Slice 1 scope

What the first slice builds; everything else is a `TODO` with its doc section (roadmap: DESIGN.md §10).

- Session image: Emacs 31.1 from source, Rust and Python toolchains, ripgrep, fd, git, tmux 3.5, TigerVNC, xdotool, Iosevka Nerd Font, Noto CJK; modules base, agent, editing, completion, files, windows, ui, terminals (eat), lsp, lang-rust, lang-python, git (the others are stubs); under 4 GB.
- sessiond: all of backend.md §3, including the VNC bridge and the GUI frame.
- gateway: local mode complete; the server-mode docker backend (official client, unit-tested with a fake) experimental; OIDC returns 501.
- playctl: curriculum validate, build, play; config schema and check; protocol typescript.
- web: every screen and pane in frontend.md, including the GUI and preview panes.
- curriculum: Parts 0 to 3 complete (lesson.md, scenario.yaml with self-solving demos, fixtures), the five tables, and `curriculum/index.yaml` listing all 15 parts (later parts `planned`).

## 6. Known limitations

- Checkpoint restores leave the learner's git index alone, so a future Magit lesson that relies on staged changes needs its restore to put the staging area back (`TODO(sessiond, backend.md §3.9)`).
- zh-CN UI strings cover about a fifth of `en.json`, so the locale is not offered; several advice strings (ticker labels, pattern nudges, habit titles) are still English literals in `web/src/advice`.
- Plain Ctrl+/ reaches Emacs as `C-_` (tmux encodes it as that byte), so the ticker shows `C-_` for it.

- `reset-step` does not switch off buffer-local minor modes a demonstration turned on; lessons that toggle a mode accept either direction or reset it in their setup.
- "Go to this step" on lesson 3.5's `try` and `hide` starts without the learner's `*Help*` display rule (a checkpoint does not record `display-buffer-alist`), so the learner evaluates `popups.el` again, as the step's hint says. Watch it on 3.5 `rule` leaves the demonstration's rule active.
