# Implementation notes

Workflow, test tools, toolchain, scope, known limitations and authoring pitfalls; rules and main commands are in `AGENTS.md`.

## 1. Where the details live

DESIGN.md maps topics to documents; third-party.md lists every dependency and pin.

## 2. Development workflow

- **Config files:** `deploy/config.json` (minimal local); `deploy/config.dev.json` (`make dev`: `publicUrl` is Vite's `:5173` so proxied Origins pass, state under `.dev/`, author mode); `deploy/config.example.json` (server).
- **Emacs configuration:** rebuild the session image (about 15 s) and `make reset-workspace`.
- **Without rebuilding images:** bind-mount `CGO_ENABLED=0` binaries over `/gateway` and `/usr/local/bin/sessiond` in a Compose overlay (also `web/dist` with `PG_WEB_DIR`, a built curriculum over `/srv/curriculum`); `up -d --force-recreate`.
- **Without the stack:** `sessiond -fake` (backend.md §3.1); `pnpm preview --port 8080 --host 127.0.0.1 --strictPort`; `pnpm dev:fake` (`web/dev/README.md`).

## 3. Longer commands and test tools

```sh
go run ./cmd/gateway -config deploy/config.dev.json -web web/dist -fonts .dev/fonts -set sessions.limits.memory=4g
go test -tags dockerintegration ./internal/docker/       # docker backend against the real Engine
images/gateway/test/server-mode.sh ghcr.io/floatshadow/emacs-playground/gateway:dev   # server stack with Dex: sign-in, quotas, isolation, re-attach
```

**sessiond against the real image:**

```sh
docker run -d --name pg-it --network host --tmpfs /run:mode=1777 --tmpfs /tmp:mode=1777 --tmpfs /work:mode=1777,exec \
  -e PG_LISTEN=127.0.0.1:17200 -e PG_GATEWAY_URL=http://127.0.0.1:17180 -e PG_AUTHOR_MODE=true \
  ghcr.io/floatshadow/emacs-playground/session:dev
PG_SESSIOND_URL=http://127.0.0.1:17200 PG_TEST_FAKE_GATEWAY=127.0.0.1:17180 PG_TEST_GUI=1 \
  go test -tags integration ./internal/sessiond/ -run Integration -v
```

Without `PG_SESSIOND_URL`: testcontainers (`PG_SESSION_IMAGE`), no lesson or GUI parts.

**Behavioural harness** (sessiond). `sessiond -fake` runs the fake editor (`internal/sessiond/agent/fakeeditor`): a deterministic model of buffers, windows, tabs, help and build buffers and the minibuffer behind the real agent socket, which answers every agent op from its model and reports key, state and minibuffer events; its tmux turns rpc keys, demos, skips and fast-forwards into editor keys, and tests inject faults (failing ops, a hook run when an op arrives, a dropped connection, a silent Emacs). Its feature budget is what a test needs. It is checked against the real agent by the contract fixtures in `emacs.d/test/fixtures/agent` (setup files, steps, calls and replies, key and minibuffer events, the final state read once every step ran; compared by shape plus the fields marked exact, the final state against the replay's; one case keeps every state message for the state shapes sessiond decodes), recorded by `make agent-fixtures` in the session image and replayed by `make emacs-test` and by `go test ./internal/sessiond/agent/fakeeditor`. `internal/sessiond/lifecycle_test.go` drives every lesson operation through the HTTP API against a fake gateway that applies reports as the real one does (generations, rising statuses, laps once per id) and can go down, reset a lesson or lose its store, and compares the ordered trace of events and gateway reports with `internal/sessiond/testdata/traces/*.trace`; `UPDATE_TRACES=1` rewrites them, and a changed trace is reviewed like code. The progress outbox's retry, coalescing, generation and restart rules have their own tests on `testing/synctest` time (`internal/sessiond/progress`). `TestKilledDuringRestore` re-runs the test binary as a fake-mode sessiond process, kills its process group with SIGKILL while `restore-editor` is held (the files restored, nothing else), and starts another on the same workspace, which must resume the step, Reset it and let the learner complete it.

**Browser scripts** (`web/e2e/`; `PG_CDP`, `PG_URL`, `PG_WORKSPACE`): `smoke.mjs` (`make e2e`; `PG_SMOKE_ONLY=3,4`, `PG_SMOKE_PLAY=0`), `review.mjs` (one check per UX contract; `PG_REVIEW_ONLY`), `lesson.mjs` (fake session), `shots.mjs`, `demo-focus.mjs`. They write to `artifacts/`, reset the progress they touch and stay under ten page loads a minute (deployment.md §9.3); `*.local.mjs` is git-ignored.

**Parallel QA.** Browser agents sharing a session corrupt each other's state; each gets its own Compose project (`-p`; an overlay with its own port via `ports: !override`; `PG_PUBLIC_URL` on that port, or POSTs are refused as cross-origin; its own world-writable workspace with `.playground/ssh` made before `up`) and Chrome container, and the scripts point at it (`PG_URL`, `PG_CDP`, `PG_WORKSPACE`, `PG_PLAY_SESSION`). Drive with real keys; inspect with `emacsclient --eval`. Accepted when `make check`, `make emacs-test`, `smoke.mjs` and `review.mjs` pass, and `play all` then `play -watch all` pass on a fresh workspace.

## 4. Toolchain

Go 1.27.3 from go.dev into `~/.local/go` (`go1.27.3.linux-amd64.tar.gz`, sha256 `65efbc80fdc1fa2cd2631cad5b481777a68e8725f34e89f50fc6ac714e379b40`, as listed at `https://go.dev/dl/?mode=json`); pnpm through corepack: `corepack enable --install-directory ~/.local/bin pnpm`, then `corepack install` in `web/` (Node 25+: `npm install -g corepack` first). Commit the `minimumReleaseAgeExclude` entries pnpm adds to `web/pnpm-workspace.yaml` with the lockfile. Builds need GitHub, go.dev, npm, GNU/NonGNU ELPA, MELPA, Docker Hub and GHCR.

## 5. Scope

Done: the session image with all eighteen modules; sessiond with projects, laps and the remote box; gateway local mode (server mode with OIDC experimental); playctl; every web screen; Parts 0 to 14 with P1–P8. Everything else is a `TODO` with its doc section (DESIGN.md §10).

## 6. Known limitations

Open decisions: DESIGN.md §12; what a restore misses: backend.md §3.9.

- Lesson 3.5: "Go to this step" on `try`/`hide` lacks the learner's `*Help*` rule (the hint says to re-evaluate `popups.el`); Watch it on `rule` leaves the demo's rule active.
- Lesson 3.1's `close` demo is relative to the selected window.
- Fast-forwarded steps show as "Later"; Continue on an all-watched lesson stays on its last step.
- The WebGL skip above device pixel ratio 1 is verified only under Chrome's emulated scale factor.
- An activation does not write `snapshot.json`, so a recovery can reopen the previous lesson's windows under the new lesson's step: a recover op queued behind another lesson's load, or a daemon killed within `snapshot.AutosaveInterval` of a lesson switch (before a restore wrote its spec) whose last spec has any file under the new root (`showsLesson`). Reset puts the step back.
- Lesson 11.6's `fence` misses its Rust-buffer condition on the first `C-c '` after a fresh workspace start; replays from 11.1 pass.
- `progress.Outbox.Flush` serves only the step tests and repeats part of `Run`'s delivery.
- The progress interleaving tests (`internal/sessiond/session/recovery_test.go`) order the competing drop by a sleep; the reconnect test sends its hello at the start of the restore only; `hosts.test.ts` checks the key guard's predicate, not a dispatched key.
- backend.md §3.9 describes an agent leaving during `restore-editor` both as `ErrEditor` and as a deferred editor state; the code answers `ErrEditor` and keeps the deferred state.
- While the snapshot shows a lesson, steps after the active one show no tick (`selectors.ts` `stepStatuses`): they are evaluated again when reached.
- The splitter sets `aria-valuenow` only after a drag or key, in pixels, without `aria-valuemin`/`aria-valuemax`.
- Keys reach Emacs only while the terminal has focus (Ctrl+V after a click in the lesson prose stays with the page); in Firefox, Alt+V can open the menu bar and `ESC v` is the `M-v` fallback.
- The `integration`-tagged sessiond test is outside `make check`, needs real Emacs and tmux, and is only compiled (`go vet -tags integration ./internal/sessiond/`).

## 7. Curriculum authoring pitfalls

Rules: CURRICULUM.md B.3 and B.4. Beyond them:

- **One action per step.**
- **Every sentence true on the learner's screen**, in the shipped theme and with any later module set; name or hide every mode-line token the learner sees.
- **Say how often and how to page**: `ESC ESC ESC` undoes one thing at a time; which-key's "(1 of 2)" pages with `C-h n`.
- **Name the copy** by absolute path (`/work/hello` or `/work/projects/hello`): a wrong copy uniquifies buffer names and breaks demos that type them.
- **Demos and hints start from the step's checkpoint**, not the previous demo's end: type absolute paths, never rely on the prompt's directory or point.
- **Commands that read their own keys** (`help-for-help`, `query-replace`, which-key paging) send `key` only when they finish: check the screen state (`{"windows.buf": " *Metahelp*"}`).
- **Session state:** add to `snapshot.GlobalVars` what a lesson relies on (backend.md §3.9). Demos never depend on the learner's history (2.2 finds its command with `M-r`; 1.6 sets the macro counter first).
- **Setups never ask:** bind `kill-buffer-query-functions` to nil to kill a buffer with a process.
- **Write the keys out:** keys in the lesson or step text silence habit cards, paraphrased ones do not. `:try` clicks are learner input.
- **Command names:** keep older names (`kill-region` for `C-k`) as alternatives; `M-y` is `consult-yank-pop` only from lesson 4.2.
- **Test with every module on:** with a language server, imenu can land on doc comments and corfu's popup opens in snippet fields.
- **No newline right after a symbol** in code buffers: corfu-auto with cape-dabbrev can take the RET, and an open popup takes `M-<` and `M->`, so a demo searches forward instead of jumping back after typing a symbol. Prose buffers open the popup only on request.
- **Magit** buffers survive restores and remember open sections: setups clear `magit-repository-local-cache`, demos fold first (`M-2`, `M-4`). A conflicted file opens at its first conflict, so `C-c ^ n` goes to the second.
- **setup.run rebuilding `.git`** races with git run by the previous lesson's buffers (diff-hl, Magit): retry `rm`, `add` and `commit`. Box state comes from setup.run; checkpoints do not cover the box.
- **No TRAMP calls from timers or process callbacks:** nested in another operation on the connection, they break it.
- **user-lisp seeds** load from `after-init-hook` in `with-demoted-errors`, so a broken file cannot stop the daemon.
- **Idempotent demos:** adding to a seeded file replaces a line, so a revisit's Watch it or Skip adds nothing. A "menu closed" condition needs a suffix pressed: `waiting` is null before it opens.
- **Lessons that edit `emacs.d/modules`** (13.1, 13.7) cannot replay on a stack that mounts the modules read-only.
- **Check:** `play`, `play -watch`, `-repeat 2` on a fresh workspace (`make curriculum-play`, `curriculum-watch`); `play all` in course order, since Part 13's settings (a 2 s auto-save) stay on for the projects; Part 3 then Parts 0-2 for more modules than listed; pause `~1000` before a `C-g` that follows an edit (emacs.md §10.4).
