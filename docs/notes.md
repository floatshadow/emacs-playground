# Implementation notes

Workflow, test tools, toolchain, scope, known limitations and authoring pitfalls; rules and main commands are in `AGENTS.md`.

## 1. Where the details live

DESIGN.md maps topics to documents; third-party.md lists every dependency and pin.

## 2. Development workflow

- **Config files:** `deploy/config.json` (minimal local); `deploy/config.dev.json` (`make dev`: `publicUrl` is Vite's `:5173` so proxied Origins pass, state under `.dev/`, author mode); `deploy/config.example.json` (server).
- **Emacs configuration** changes: rebuild the session image (about 15 s for config only) and `make reset-workspace`.
- **Without rebuilding images:** bind-mount `CGO_ENABLED=0` binaries over `/gateway` and `/usr/local/bin/sessiond` in a Compose overlay (also `web/dist` with `PG_WEB_DIR`, a built curriculum over `/srv/curriculum`); `up -d --force-recreate`.
- **Without the stack:** `sessiond -fake` (backend.md §3.1); `pnpm preview --port 8080 --host 127.0.0.1 --strictPort`; `pnpm dev:fake` (`web/dev/README.md`).

## 3. Longer commands and test tools

```sh
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

Without `PG_SESSIOND_URL` it uses testcontainers (`PG_SESSION_IMAGE`), skipping the lesson and GUI parts.

**Browser scripts** (`web/e2e/`; `PG_CDP`, `PG_URL`, `PG_WORKSPACE`): `smoke.mjs` (`make e2e`; `PG_SMOKE_ONLY=3,4`, `PG_SMOKE_PLAY=0`), `review.mjs` (one check per UX contract; `PG_REVIEW_ONLY`), `lesson.mjs` (fake session), `shots.mjs`, `demo-focus.mjs`. They write to `artifacts/`, reset the progress they touch and stay under ten page loads a minute (deployment.md §9.3); `*.local.mjs` are git-ignored.

**Parallel QA.** Browser agents sharing the one local session corrupt each other's state, so each gets its own Compose project (`-p`; an overlay with its own port via `ports: !override`; `PG_PUBLIC_URL` on that port, or POSTs are refused as cross-origin; its own world-writable workspace with `.playground/ssh` made before `up`) and Chrome container, and the scripts point at it (`PG_URL`, `PG_CDP`, `PG_WORKSPACE`, `PG_PLAY_SESSION`). Drive with real keys; inspect Emacs with `emacsclient --eval`. A change is accepted when `make check`, `make emacs-test`, `smoke.mjs` and `review.mjs` pass, and `play all` then `play -watch all` pass on a fresh workspace.

## 4. Toolchain

Go 1.27.3 from go.dev into `~/.local/go` (`go1.27.3.linux-amd64.tar.gz`, sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40`, as listed at `https://go.dev/dl/?mode=json`); pnpm through corepack: `corepack enable --install-directory ~/.local/bin pnpm`, then `corepack install` in `web/` (Node 25+: `npm install -g corepack` first). Commit the `minimumReleaseAgeExclude` entries pnpm adds to `web/pnpm-workspace.yaml` with the lockfile. Builds need GitHub, go.dev, npm, GNU/NonGNU ELPA, MELPA, Docker Hub and GHCR.

## 5. Scope

Done: the session image with all eighteen modules; sessiond with projects, laps and the remote box; gateway local mode (docker backend experimental, OIDC 501); playctl; every web screen; Parts 0 to 14 with P1–P8. Everything else is a `TODO` with its doc section (DESIGN.md §10).

## 6. Known limitations

Open decisions: DESIGN.md §12; what a restore misses: backend.md §3.9.

- Lesson 3.5: "Go to this step" on `try`/`hide` lacks the learner's `*Help*` rule (the hint says to re-evaluate `popups.el`); Watch it on `rule` leaves the demo's rule active.
- Lesson 3.1's `close` demo is relative to the selected window.
- Fast-forwarded steps show as "Later"; Continue on an all-watched lesson stays on its last step.
- The WebGL skip above device pixel ratio 1 is verified only under Chrome's emulated scale factor.

## 7. Curriculum authoring pitfalls

Rules: CURRICULUM.md B.3 and B.4. Beyond them:

- **One action per step.**
- **Every sentence true on the learner's screen**, in the shipped theme and with any later module set; name or hide every mode-line token the learner sees.
- **Say how often and how to page**: `ESC ESC ESC` undoes one thing at a time; which-key's "(1 of 2)" pages with `C-h n`.
- **Name the copy** by absolute path (`/work/hello` or `/work/projects/hello`): a wrong copy renames buffers through uniquify and breaks demos that type buffer names.
- **Demos and hints start from the step's checkpoint**, not where the previous demo left off: type absolute paths, never rely on the prompt's directory or point.
- **Commands that read their own keys** (`help-for-help`, `query-replace`, which-key paging) send `key` only when they finish: check the screen state (`{"windows.buf": " *Metahelp*"}`).
- **Session state:** add to `sessiond.GlobalVars` when a lesson relies on more (backend.md §3.9). Demos never depend on the learner's history (2.2 finds its command with `M-r`; 1.6 sets the macro counter first).
- **Setups never ask:** bind `kill-buffer-query-functions` to nil when killing a buffer with a process.
- **Write the keys out:** keys in the lesson or step text silence habit cards, paraphrased ones do not. `:try` clicks are learner input.
- **Command names:** keep older names (`kill-region` for `C-k`) as alternatives; `M-y` is `consult-yank-pop` only from lesson 4.2.
- **Test with every module on:** with a language server, imenu positions can land on doc comments and corfu's popup opens inside snippet fields.
- **No newline right after a symbol** in code buffers: corfu-auto with cape-dabbrev can take the RET. Prose buffers open the popup only on request.
- **Magit** buffers survive restores and Magit remembers open sections: setups clear `magit-repository-local-cache`, demos fold first (`M-2`, `M-4`). Opening a conflicted file puts point in its first conflict, so `C-c ^ n` goes to the second.
- **setup.run rebuilding `.git`** races with git run by the previous lesson's buffers (diff-hl, Magit): retry `rm`, `add` and `commit`. Box state comes from setup.run; checkpoints do not cover the box.
- **No TRAMP calls from timers or process callbacks:** nested in another operation on the same connection, they break it.
- **Lessons that edit `emacs.d/modules`** (13.1, 13.6) cannot replay on a stack that mounts the modules read-only.
- **Check:** `play` and `play -watch`, `-repeat 2`, on a fresh workspace (`make curriculum-play`, `make curriculum-watch`); `play all` in course order, since Part 13's settings (a 2 s auto-save) stay on for the projects; replay Part 3 then Parts 0-2 for more modules than listed; pause `~1000` before a `C-g` that follows an edit (emacs.md §10.4).
