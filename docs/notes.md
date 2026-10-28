# Implementation notes

Developer workflow, test tools, toolchain, scope, known limitations and curriculum authoring pitfalls. Rules and main commands: `AGENTS.md`.

## 1. Where the details live

DESIGN.md maps topics to documents; third-party.md lists every dependency and pin.

## 2. Development workflow

- **Config files:** `deploy/config.json` (minimal local); `deploy/config.dev.json` (`make dev`: `publicUrl` is Vite's `:5173` so proxied Origins pass, state under `.dev/`, author mode); `deploy/config.example.json` (full server example).
- **sessiond without Emacs:** `sessiond config` prints its configuration; `sessiond -fake` substitutes bash for Emacs, tmux and Xvnc.
- **Emacs configuration** changes need a session image rebuild (about 15 s when only the config changed) and `make reset-workspace` (emacs.md §3).
- **Iterating without rebuilding images:** build `CGO_ENABLED=0` binaries where the daemon sees them and bind-mount them in a Compose overlay over `/gateway` and `/usr/local/bin/sessiond` (also `web/dist` with `PG_WEB_DIR`, a built curriculum over `/srv/curriculum`); `up -d --force-recreate`.
- **Web without the gateway:** `pnpm preview --port 8080 --host 127.0.0.1 --strictPort`; `pnpm dev:fake` runs the fake session (`web/dev/README.md`).

## 3. Longer commands and test tools

```sh
go run ./cmd/gateway config -config deploy/config.json   # effective config, secrets redacted
go run ./cmd/gateway -config deploy/config.dev.json -web web/dist -fonts .dev/fonts -set sessions.limits.memory=4g
go test -tags dockerintegration ./internal/docker/       # docker backend against the real Engine
images/gateway/test/server-mode.sh ghcr.io/floatshadow/emacs-playground/gateway:dev   # network isolation, proxy secret, gateway re-attach
```

**sessiond against the real image:**

```sh
docker run -d --name pg-it --network host --tmpfs /run:mode=1777 --tmpfs /tmp:mode=1777 --tmpfs /work:mode=1777,exec \
  -e PG_LISTEN=127.0.0.1:17200 -e PG_GATEWAY_URL=http://127.0.0.1:17180 -e PG_AUTHOR_MODE=true \
  ghcr.io/floatshadow/emacs-playground/session:dev
PG_SESSIOND_URL=http://127.0.0.1:17200 PG_TEST_FAKE_GATEWAY=127.0.0.1:17180 PG_TEST_GUI=1 \
  go test -tags integration ./internal/sessiond/ -run Integration -v
```

Without `PG_SESSIOND_URL` the test uses testcontainers (`PG_SESSION_IMAGE`) and skips the lesson and GUI parts.

**Browser scripts** (`web/e2e/`, environment `PG_CDP`, `PG_URL`, `PG_WORKSPACE`): `smoke.mjs` (`make e2e`; `PG_SMOKE_ONLY=3,4`, `PG_SMOKE_PLAY=0`), `review.mjs` (one check per UX contract; `PG_REVIEW_ONLY`), `lesson.mjs` (fake session), `shots.mjs`, `demo-focus.mjs`. Output goes to `artifacts/`; scripts reset the progress they touch. Page loads must stay under the gateway's ten session requests a minute per user. Exploratory scripts: `web/e2e/*.local.mjs` (git-ignored).

**Parallel QA.** A local stack has one session; browser agents sharing it corrupt each other's progress and Emacs state. Give each agent its own Compose project (`-p`, an overlay with its own port via `ports: !override`, `PG_PUBLIC_URL` on that port or POSTs are refused as cross-origin, and its own world-writable workspace with `.playground/ssh` created before `up`) and Chrome container (own CDP port), and point the scripts at it (`PG_URL`, `PG_CDP`, `PG_WORKSPACE`; `PG_PLAY_SESSION` for `make curriculum-play`). Drive with real keys; inspect Emacs with `emacsclient --eval` in the container. A change is accepted when `make check`, `make emacs-test`, `smoke.mjs` and `review.mjs` pass, and `playctl curriculum play all` then `play -watch all` pass on a fresh workspace.

## 4. Toolchain

Go 1.27.3 from go.dev into `~/.local/go` (`go1.27.3.linux-amd64.tar.gz`, sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40`, as listed at `https://go.dev/dl/?mode=json`); pnpm through corepack: `corepack enable --install-directory ~/.local/bin pnpm`, then `corepack install` in `web/` fetches the `packageManager` version (Node 25+: `npm install -g corepack` first). Commit the `minimumReleaseAgeExclude` entries pnpm adds to `web/pnpm-workspace.yaml` with the lockfile. Builds need GitHub, go.dev, npm, GNU/NonGNU ELPA, MELPA, Docker Hub and GHCR.

## 5. Scope

Session image with Emacs 31.1, every toolchain and all eighteen modules (emacs.md); all of sessiond (backend.md §3) with projects, laps and the remote box; gateway local mode, docker backend experimental, OIDC 501; playctl; every web screen; curriculum Parts 0 to 14 with P1–P8 ready. Everything else is a `TODO` with its doc section (DESIGN.md §10).

## 6. Known limitations

Open decisions and unscheduled work are in DESIGN.md §12; what a checkpoint restore does not put back is in backend.md §3.9.

- Lesson 3.5: "Go to this step" on `try`/`hide` lacks the learner's `*Help*` rule (the hint says to re-evaluate `popups.el`); Watch it on `rule` leaves the demo's rule active.
- Lesson 3.1's `close` demo is relative to the selected window.
- Fast-forwarded steps show as "Later"; Continue on an all-watched lesson stays on its last step.
- The terminal's WebGL skip at a device pixel ratio above 1 was verified only under Chrome's emulated scale factor, not on HiDPI hardware.

## 7. Curriculum authoring pitfalls

The rules are CURRICULUM.md B.3 (demo strings, authoring rules) and B.4 (exit rows). Pitfalls beyond them:

- **One action per step.** Several file opens across projects plus a new trick in one step is too much; split it.
- **Every sentence true on the learner's screen**, in the shipped theme and with any later module set (`C-x C-b`: a second window or ibuffer). Name or hide every mode-line token the learner sees.
- **Say how often and how to page.** `ESC ESC ESC` undoes one thing at a time ("twice" when two are needed); which-key's "(1 of 2)" pages with `C-h n`.
- **Name the copy.** The workspace holds several copies of a project (`/work/hello` from 2.3, `/work/projects/hello`): text, hints and conditions name the absolute path. Opening the wrong copy renames buffers through uniquify (`main.rs<projects/hello>`), which breaks demos that type buffer names.
- **Demos and hints start from the step's checkpoint**, never from where the previous demo left off: Watch it, Do it yourself, Skip and Go to this step restore or fast-forward first. Type absolute paths after the offered directory; never rely on the prompt's directory or point ("the prompt now starts in src/" is false after a reset).
- **Pause `~1000` before a `C-g` that follows an edit** (emacs.md §10.4).
- **More modules than listed:** also replay Part 3 first, then Parts 0-2 (emacs.md §4).
- **Commands that read their own keys** (`help-for-help`, `query-replace`, which-key paging) send their `key` event only when they finish: check the screen state (`{"windows.buf": " *Metahelp*"}`) for "the menu opened".
- **Session state:** a restore brings back `sessiond.GlobalVars` (add one when a lesson relies on more), not minor modes or `display-buffer-alist`. Demos never depend on history the learner made (2.2 finds its command with `M-r`; 1.6 sets the macro counter first).
- **Setups never ask** (`inhibit-interaction`): bind `kill-buffer-query-functions` to nil when killing a buffer with a process. A window that pops up (build output) is not the learner's split.
- **Write the keys out.** Keys in the lesson or step text silence habit cards; paraphrased ones do not. `:try` clicks count as the learner's own input.
- **Command names:** the agent reports `real-this-command`; keep older names (`kill-region` for `C-k`) as alternatives, and `M-y` is `consult-yank-pop` only from lesson 4.2.
- **Test with every module on:** with a language server, imenu positions can land on doc comments and corfu's popup opens inside snippet fields.
- **Demos never type a newline right after a symbol** in code buffers: corfu-auto with cape-dabbrev can take the RET as a candidate.
- **Magit** buffers survive restores, and Magit keeps each repository's open sections after its buffer is killed: setups clear `magit-repository-local-cache`, demos fold first (`M-2`, `M-4`). Opening a conflicted file puts point in its first conflict, so `C-c ^ n` goes to the second.
- **setup.run rebuilding `.git`** races with git run by the previous lesson's buffers (diff-hl, Magit): retry `rm`, `add` and `commit`. Box state comes from setup.run; checkpoints do not cover the box.
- **No TRAMP calls from timers or process callbacks:** one that runs inside another TRAMP operation on the same connection breaks it.
- **Lessons that edit `emacs.d/modules`** (13.1, 13.6) cannot be replayed on a stack that mounts the modules read-only.
- **Check:** `playctl curriculum play` and `play -watch`, `-repeat 2`, on a fresh workspace (`make curriculum-play`, `make curriculum-watch`); `play all` in course order too, since what learners set in Part 13 (a 2 s auto-save) stays on for the projects.
