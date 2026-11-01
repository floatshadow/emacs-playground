# Emacs side: image, configuration, frames, agent

Everything in the session container except sessiond.

## 1. Session image

Debian trixie, multi-stage `images/session/Dockerfile`; each heavy stage (`emacs-build`, `tmux`, `rust`, `packages`, …) rebuilds only when its pins in `images/session/versions.env` change. Emacs 31.1 is built with tree-sitter grammars, tmux from its release tarball; pinned packages are natively compiled into `/opt/playground/{elpa,eln-cache}`.

Configure flags: `--with-native-compilation=aot --with-tree-sitter --with-modules --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2 --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3 --without-pop` (Emacs 31 rejects `--with-json`). Lucid, because the daemon survives losing the X connection and pgtk cannot serve terminal and X frames from one daemon. Fallback: Emacs 30.2.

Paths: user `learner` (uid 1000); workspace `/work`; pristine config `/opt/playground/emacs.d`; live config `/work/emacs.d` (`--init-directory`); server socket `/run/playground/emacs/playground`; agent socket `/run/playground/agent.sock`; versions in `/opt/playground/versions.env` (`GET /status`). The root filesystem may be read-only; `/tmp`, `/run` and `~/.cache` are tmpfs. Run-time native code goes to `~/.cache/emacs/eln-cache`, or `/work/.playground/eln-cache` when that is `noexec`, else nowhere (`early-init.el`); so nothing depends on it, the build natively compiles the Emacs Lisp files the AOT build leaves out (`native-gaps.el`) and the trampolines the modules need (`prebuild.el`, which also fails the build when a module does not load).

Size: about 5.8 GB unpacked, 1.5 GB compressed (remote box 2.6 GB); a cold build takes about 11 minutes on 96 cores.

## 2. Toolchains

One cached stage per toolchain (list: third-party.md §4), versions and sha256 in `versions.env`. The opam switch `/opt/opam/default` holds dune, utop and the prefetched libraries; the OCaml editor tools are built in a second switch and only their executables kept. TeX Live is scheme-basic plus `prefetch/texlive.txt`, with per-user files under `~/.cache/texlive`. The `natives` stage builds vterm's and jinx's modules and `epdfinfo` (third-party.md §3).

## 3. Configuration layout

`emacs.d/`: `early-init.el` (GC, frame defaults, native-code location), `init.el` (loads the modules listed in `modules.el`, in order; never edited by the course), `modules.el` (written by the playground: `(setq playground-modules '(base agent …))`), `modules/` (base, agent, editing, files, windows, completion, git, terminals, lsp, lang-rust, lang-ocaml, lang-python, writing, input, org, power, remote, ui; each file's header lists its keys by lesson), `lisp/playground-*.el` (the agent, §10), `user-lisp/` (the learner's Lisp: `my-commands.el`, plus what Part 13 adds), `templates` (tempel snippets, lesson 4.4), `packages.el`, `packages.lock`, `vendor/`, `tools/` (§8), `test/` (§10.6).

A module that fails to load is reported (a warning and `moduleErrors` in the agent's `hello`) and the others still load. `PG_MODULES="base agent …"` overrides `modules.el` (tests, `prebuild.el`). `agent.el` prefers the compiled agent in the image (`/opt/playground/emacs.d/lisp`, or `PG_AGENT_LISP`) so it always speaks sessiond's protocol (`playground-agent-protocol` equals `protocol.Version`; sessiond logs a mismatch). The pristine config is copied to `/work/emacs.d` on first start only (AGENTS.md §5).

## 4. Module unlocking

Scenarios declare `modules`; a scenario load adds its modules and never removes any (`rpc modules` sets exactly). sessiond writes `modules.el` and restarts the daemon with buffer restore (backend.md §3.3, §3.12); checkpoint restores never change it. Part 0 runs `base agent`; Part 13 reaches the full set. A lesson must pass with more modules than it lists (from Part 2 `C-x C-b` is ibuffer; the windows module closes a project's tab after `C-x p k`); replays meant to match an in-order learner start from a fresh workspace.

## 5. Terminal frame

tmux (`-L pg`, session `emacs`) holds the terminal frame so it survives browser reconnects and `make shell` can attach. `images/session/tmux.conf`:

- `default-terminal tmux-256color`; outer `xterm*` features RGB, extkeys, clipboard, hyperlinks; `extended-keys always`, `extended-keys-format csi-u`, `escape-time 10` (keeps a split escape sequence together), `focus-events on`, `set-clipboard on`, `allow-passthrough on`.
- No status line, prefix, bindings or history; every mouse event goes to Emacs.
- `C-S-<letter>` re-sent as CSI u (tmux 3.8 drops the Shift); `send-keys` bypasses this.
- `fill-character " "`: blanks, not grey `~`, around a window smaller than its client.
- `remain-on-exit on` and a global `pane-died` respawn hook, overridden per session so only sessiond respawns (backend.md §3.3).

Keys: the browser's CSI u chords (frontend.md §5) pass through tmux to `kkp`, whose decoders `base.el` installs directly in tmux frames (kkp waits for a protocol answer tmux never gives). It also maps tmux's Home `ESC [ 1 ~` and End `ESC [ 4 ~` to `<home>`/`<end>` and unmodified Delete `ESC [ 3 ~` to `<deletechar>` (`<delete>` becomes DEL through `local-function-key-map`); modified keys keep kkp's names. A keyboard macro records raw bytes, so when recording ends decoded sequences become their key (F3 is `<f3>` in `C-x C-k RET`, not `ESC O R`).

Faces: selected mode line violet `#7a58c4`, others `#232639` (lessons say "the bright mode line"); the region teal `#00808f` (`bg-graph-cyan-1`: distinct from the search, hl-line, paren and mode-line backgrounds, at least 3:1 on the background; the browser terminal's selection matches, frontend.md §5); modus-vivendi-tinted palette overrides set before the theme loads. The cursor keeps the theme's colour. Housekeeping minors (ws-butler, apheleia, jinx, ocaml-eglot) have no lighter. `extended-command-suggest-shorter` is off (its tip masked M-x answers).

Clipboard: OSC 52 through tmux; paste arrives bracketed. `xterm-mouse-mode` is on (not automatic behind tmux).

## 6. GUI frame

Xvnc on `:1` (backend.md §3.3), no window manager, started on demand; posframe-style popups only in graphical frames, `pop-up-frames` off.

The browser asks Xvnc for a screen the size of its pane (RandR). Without a window manager `fullscreen` applies only at creation, so `base.el` fits every GUI frame to the monitor when it is made or the screen changes (`playground-fit-frame-to-display`, pixelwise); it does nothing under a window manager or outside the playground daemon.

## 7. Daemon and frames

`emacs --fg-daemon=playground`; frames are `emacsclient -t` in tmux and `emacsclient -c` on `:1`. `C-x C-c` closes only its frame, without asking about unsaved files; sessiond respawns the terminal frame. A frame that opens again shows the most recent file buffer, not `*scratch*`.

Saving: `auto-save-visited-mode` saves file buffers 5 s after the last edit without formatting (apheleia skips those saves; `C-x C-s` formats even when one already wrote the buffer), never over a file changed on disk since visited (`auto-save-visited-predicate`; else a timer asks "Save anyway?" after a lesson reset). Unmodified buffers auto-revert. No backup, auto-save or lock files. sessiond saves modified buffers before the daemon stops (backend.md §3.3).

After a daemon restart sessiond reopens `snapshot.json` with `open`: the window spec saved every 30 s, before a restart and by every restore (backend.md §3.9).

Module behaviour lessons rely on: on the last screen `C-v` moves to the end instead of signalling, `M-v` likewise on the first (`scroll-error-top-bottom`, lesson 1.1's paging step); eat and `M-x shell` run `/bin/bash`; `C-x p t` and `C-x p p` are bound before project.el loads; prose buffers wrap at words and open corfu's popup only on request; an empty `*Org-Babel Error Output*` window closes after a block runs cleanly; while a compile runs, an unmatched last line is read again with the next one, so a cargo error split across output chunks is still a location.

## 8. Packages and pinning

Sources, preferred first: GNU ELPA, NonGNU ELPA, MELPA Stable, MELPA. `packages.el` lists top-level packages per module; `tools/lock.el` resolves them into `packages.lock` (version, archive, file, sha256), skipping what Emacs ships. Regenerating resolves every package anew (new versions); removing a package instead prunes by hand its lines, the dependencies nothing else locked or listed requires (their `-pkg.el`), and their tarballs, leaving every other line as it is. Every locked tarball is vendored in `vendor/` (ELPA drops superseded releases, MELPA keeps no old snapshots); the image installs exactly the lock with no archive configured, failing on any hash mismatch. Pinned packages are read-only in `/opt/playground/elpa`; the learner's own go to `/work/emacs.d/elpa` (Part 13).

## 9. Prefetched project dependencies

Sessions have no egress (deployment.md §9.2), so cargo and uv run offline (`CARGO_NET_OFFLINE`, `UV_OFFLINE`; off for `egress: all`) against prefetched dependencies listed in `images/session/prefetch/`: `cargo/` (crates, a registry cache copied to `$CARGO_HOME` under `~/.cache` at start), `python.in`/`python.txt` (hashed wheels, uv's `UV_FIND_LINKS`), `opam.txt` (libraries in the switch), `typst.txt` (packages under `TYPST_PACKAGE_PATH`), `texlive.txt`. Fixtures should omit `Cargo.lock` and `uv.lock` or resolve to the prefetched versions. The remote box gets the same crates and wheels.

## 10. playground-agent.el

A global minor mode (built-in libraries only) that reports and answers sessiond's calls and never changes editing behaviour; hook code is wrapped so an agent bug cannot break a command. `playground-agent.el` holds the options, the op table (`playground-agent-ops`) and the mode, and loads one file per feature: `playground-transport` (socket, queue, pings, dispatch, replies, `at-top-level`), `playground-frames` (the learner's frame and window, frame hooks, `frame`, `leave-display`, `resume-loop`), `playground-observe` (command, minibuffer, isearch and echo reports, the state object, `eval`, `check`), `playground-rescue` (Rescue, `reset-debug`), `playground-editor-state` (window specs, `open`, `capture-editor`, `restore-editor`, `modified-files`, `flush-buffers`), `playground-setup` (`setup`, `set-modules`, `visit`, `describe`) and `playground-compat` (what it knows of transient, which-key, avy, menus, eldoc, eglot, Dired and ibuffer). `(require 'playground-agent)` loads them all. The image byte- and native-compiles every file with `byte-compile-error-on-warn`: an unescaped docstring quote can become a silent runtime error in a hook.

### 10.1 Hooks and emissions

- Command hooks → `key`, then `state`. `cmd` is `real-this-command` (commands rewrite `this-command`: C-k is `kill-line`). M-x (advised) reports the command run with `mx: true`. Internal events (focus, frame switch, mouse movement) and `which-key-C-h-dispatch` send nothing and keep the learner's frame. An unbound key is reported once as `undefined`. `keys` are those saved by `pre-command-hook` (Embark's `C-.` reads its action key itself); a prompt the running command opens and closes itself (Embark's action) reports nothing more.
- Messages and command errors → `echo`, minus noise (`playground-agent-echo-noise`: isearch's prompt, eldoc) and repeats with no key read since.
- Minibuffer and isearch start and end → `minibuffer` (`kind: isearch` for isearch) and `state`.
- Window, buffer, mode and macro changes → one coalesced `state` per command.
- Frame creation and deletion → `frame`; `closed` only when the last frame of its terminal goes. A new client frame gets the learner's windows; when the last graphical frame goes, its windows return to the terminal frame.
- Loops that read their own keys (`popup-menu`, avy, which-key paging; `playground-agent-waiting-loops`) set the state's `waiting` with the keys that leave them; so does a visible transient menu (`kind: transient`, `what` names the innermost prefix, leave `C-g`; `transient-active-prefix`, nil while a suffix's prompt suspends it, when the minibuffer is what waits; resent from `transient-post-exit-hook`, as after a close from outside). Suffixes report their own command names (`other-window`); keys the menu refuses report the command transient substitutes (`transient-undefined`, `transient-inapt`, `transient-noop`).
- `compilation-start-hook` stamps each run's start (for `restore-editor`).

### 10.2 State snapshot

`playground--state` builds api.md §5's state for the learner's window; windows get stable small ids, `layout` is `("vsplit"|"hsplit" child…)` over them; `minors` keeps only `playground-agent-interesting-minor-modes`. Identical consecutive states are suppressed. The minibuffer prompt is the displayed text.

### 10.3 Ops and commands

Ops run in the learner's window of the learner's frame (the frame of the last command, unless that is a terminal frame while a graphical frame exists: then the newest graphical frame), never whatever a timer selected, so `selected-window` in a condition means the learner's window.

- `check {forms}`: each form under `condition-case` and a 1 s timeout → `t`, `false` or `"unknown"`.
- `eval {form}`: value as JSON (symbol-keyed alists become objects).
- `rescue`: closes an open transient menu, visible, nested or suspended (`playground--close-menu`, the one caller of `transient--emergency-exit`), leaves a waiting loop by queueing its leave keys (a throw would leave a tty menu drawn), then minibuffers, recursive edits, isearch, macro recording, overwrite mode, narrowing, multiple cursors, iedit, extra windows (keeping a file window), `special-mode` buffers, the selection and pending input; returns its actions as sentences. Never saves, kills file buffers or reverts.
- `capture-editor {workspace, vars, maxText, maxUndo, maxTotal, maxGlobals}` → `{spec, buffers, globals}`: what a checkpoint keeps of Emacs (backend.md §3.9). `spec` is the `WindowSpec` (api.md §7): `windowState` is `window-state-get` with each buffer entry's file added, stripped of text properties (uniquify can leave buffer objects in names); files, not buffer names, identify buffers across a step. Dired, ibuffer and buffer-list windows record how to remake them (directory or filters, marks, line); `tabs`/`tabIndex` and `specialBuffers` (help buffers with their `help-xref-stack-item`) complete it. `buffers` maps each workspace file buffer with unsaved text or an undo history to `[modified, text|null, undo|null]` (readable undo entries only), within the bounds. `globals` prints the listed variables readably (strings without properties and at most 32 KiB, lists cut to 100, macro counters as integers, a macro only when it is keys), or null. Zero bounds and no `vars` capture the spec alone (`snapshot.json`).
- `open {spec}`: visits files, selects the spec's tab, then `window-state-put` with buffer entries renamed to the buffers now visiting those files and list buffers remade; else applies `layout`. Without a client frame the layout waits for the next one (`layoutDeferred`).
- `restore-editor {workspace, spec, buffers, globals}` → `{reverted, killed, buffers, globals, degraded}` (every checkpoint restore): first the step's start: leaves any open key menu, prompt or pending prefix key, as `setup`; reverts changed or modified file buffers; kills buffers of vanished files and unmodified workspace file buffers the spec does not list (conditions are false again, names un-uniquify); remakes help buffers; kills compilation runs started after the spec unless a spec window shows them (a demo's build must not satisfy the learner's step); restores the spec's tabs; `open`s it; runs `playground-reset-step-hook` (13.5 reloads a file there; `setup` empties it before `eval`). Then each buffer's unsaved text (`replace-buffer-contents`) and undo history with undo recording off and point from the spec, then the variables (and `kill-ring-yank-pointer`). Each buffer and part is restored on its own; the ones that fail (a remote buffer whose revert would ask) are named in `degraded`.
- `modified-files {workspace}` → `{paths}`; `flush-buffers` → `{saved}`: saves modified file buffers before Emacs stops, never over a file changed on disk, skipping remote and read-only ones and any that would ask.
- `setup {open, layout, eval, compile, workspace, reset, root}`: closes any key menu and leaves any prompt or pending prefix key; reverts buffers whose files changed on disk, kills those whose files are gone. With `reset`: re-enables `auto-save-visited-mode`, closes other tabs, clears unmodified leftover buffers, forgets save-place positions under `root`, opens files at line 1. Then opens, lays out (a layout string, `single`, or a workspace `.eld` window state, which sessiond does not send yet: backend.md §3.12), evaluates `eval` in the learner's window and runs `compile` in the first file's project root.
- `set-modules {enabled}`: writes `modules.el` atomically; sessiond restarts. `visit {path}` → `{buffer}`: `find-file` in the learner's window (rpc `open`).
- `describe {keys}` or `{cmd}`: binding and first docstring line (a transient prefix's is its docstring). `frame {display?}`: the GUI frame (§6). `leave-display` → `{closed, detail}`: deletes the X frames and closes the X connections before Xvnc stops; `resume-loop` → `{}`: a `top-level` from a timer restarts the command loop's wait on the terminal frame; `reset-debug` → `{}`: clears the `debug-on-quit` SIGUSR2 leaves.

Side windows: layouts, window states, `single` setups and Rescue select a normal window before deleting the others and make a sole window still marked as a side window ordinary; splitting such a window recurses to `max-lisp-eval-depth`.

Known limits: `restore-editor` leaves buffer-local minor modes a demo turned on; what else a restore omits is in backend.md §3.9.

### 10.4 Socket client

A local socket with a line filter (lines over 1 MB dropped), reconnect backoff 1 to 16 s and a 2 s `ping`. `process-send-string` blocks on a full socket, so after 5 s without a pong output is queued (1000 entries; oldest events dropped, never replies); a peer silent for 10 s is dropped.

`call`s run from a timer at top level under `with-timeout` and `inhibit-interaction`: an op that would prompt fails instead of blocking every later call. Ops may throw to the command loop (setup, rescue) and reply from a follow-up timer. Timers run with quitting inhibited, so three C-g in a row (or sessiond's SIGUSR2) stop a runaway eval.

C-g during redisplay: a C-g read while redisplay runs Lisp (typically `jit-lock-context-time` refontification 0.5 s after an edit) quits that Lisp and never runs `keyboard-quit`, so the region stays active. Demos therefore pause about a second before a C-g that follows an edit.

### 10.5 Performance

`post-command-hook` under 0.3 ms median; no polling timers but the ping.

### 10.6 Testing

`make emacs-test` runs `test/agent-test.el` (against a fake sessiond socket server in Elisp; it also replays the contract fixtures in `test/fixtures/agent/` that `make agent-fixtures` records with `test/record-fixtures.el`, backend.md §6) and `test/config-test.el` (early-init's native-code location), which loads `test/modules-test.el` (base and editing: tmux and macro keys, mode-line and region contrast, paging, M-y as `yank-pop` before Part 4, GUI frame fit), which runs `test/all-modules-test.el` in a child Emacs (every module loads with its packages; the keys of Parts 4 to 13, native parts, grammars, tools, language servers, dape adapters, Org's Rust blocks and error window, prose completion, Typst citations, compile's error regexps, early-finished compiles, cargo messages split across output chunks, a transient menu under repeat-mode). sessiond's integration test (backend.md §6) covers the agent end to end. `test/fake-sessiond.py` stands in for sessiond's socket to watch what the agent sends and type calls by hand.

## 11. Standalone use

The image works as a plain Emacs (`docker run -it --rm -v "$PWD:/work" …/session emacs -nw`); `PLAYGROUND=0` turns off the agent and the frame-only `C-x C-c`. The config runs on any Emacs 31 with the toolchains (lesson 13.7 starts it on the remote box).
