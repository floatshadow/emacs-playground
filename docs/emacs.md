# Emacs side: image, configuration, frames, agent

Everything in the session container except sessiond.

## 1. Session image

Debian trixie, multi-stage `images/session/Dockerfile`; each heavy stage (`emacs-build`, `tmux`, `rust`, `python`, `fonts`, `packages`, …) rebuilds only when its own pins in `images/session/versions.env` change. Emacs 31.1 is natively compiled ahead of time with tree-sitter grammars; tmux is built from its release tarball (Debian ships 3.5a); pinned packages are compiled into `/opt/playground/{elpa,eln-cache}`.

Configure flags: `--with-native-compilation=aot --with-tree-sitter --with-modules --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2 --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3 --without-pop` (Emacs 31 rejects `--with-json`). Lucid: the daemon survives losing the X connection; pgtk cannot serve terminal and X frames from one daemon. Fallback: Emacs 30.2.

Paths: user `learner` (uid 1000); workspace `/work`; pristine config `/opt/playground/emacs.d`; live config `/work/emacs.d` (`--init-directory`); server socket `/run/playground/emacs/playground`; agent socket `/run/playground/agent.sock`; versions in `/opt/playground/versions.env` (`GET /status`). The root filesystem may be read-only; `/tmp`, `/run` and `~/.cache` are tmpfs. Run-time native code goes to `~/.cache/emacs/eln-cache`, else `/work/.playground/eln-cache` when that is `noexec`.

Size: about 5.8 GB unpacked, 1.5 GB compressed (the remote box 2.6 GB); a cold build takes about 11 minutes on a 96-core builder, a config-only change about a minute.

## 2. Toolchains

One cached stage per toolchain, versions and sha256 in `versions.env`: Rust (rustup, rust-analyzer, clippy, rustfmt, cargo-nextest, codelldb in `/opt/codelldb`), Python (uv, ruff, basedpyright; debugpy in `/opt/debugpy`), OCaml (opam with one switch, `/opt/opam/default`: dune, utop and the prefetched libraries; the editor tools ocaml-lsp-server, ocamlformat, merlin, odoc and earlybird are built in a second switch and only their executables kept; OCaml 5.4 because ocamlfind has no 5.5 release), Typst and tinymist, TeX Live 2025 from its frozen repository (scheme-basic plus `prefetch/texlive.txt`, in `/opt/texlive`; per-user files under `~/.cache/texlive`) with texlab, Node (one binary) for bash-language-server and basedpyright (from npm: the PyPI wheel bundles a second Node), marksman, pandoc, delta, shellcheck, ImageMagick, poppler-utils, enchant with hunspell en_US and en_GB; fonts Iosevka Nerd Font, Noto CJK, Noto Color Emoji (Typst embeds Libertinus). The `natives` stage compiles vterm's module, jinx's module and pdf-tools' `epdfinfo` from the vendored tarballs into their package directories.

## 3. Configuration layout

`emacs.d/`: `early-init.el` (GC, frame defaults, native-code location), `init.el` (loads the modules listed in `modules.el`, in order; never edited by the course), `modules.el` (written by the playground: `(setq playground-modules '(base agent …))`), `modules/` (base, agent, editing, files, windows, completion, git, terminals, lsp, lang-rust, lang-ocaml, lang-python, writing, input, org, power, remote, ui; each file's header lists its keys by lesson), `lisp/playground-agent.el` (§10), `user-lisp/` (the learner's Lisp), `packages.el`, `packages.lock`, `vendor/`, `tools/` (§8), `test/` (§10.6).

A module that fails to load is reported (a warning and `moduleErrors` in the agent's `hello`) and the others still load. `agent.el` prefers the compiled agent in the image (`/opt/playground/emacs.d/lisp`) so it always speaks sessiond's protocol.

The pristine config is copied to `/work/emacs.d` on first start only: after changing `emacs.d/modules`, rebuild the image and `make reset-workspace`.

## 4. Module unlocking

Scenarios declare `modules`. Unlocking is monotonic: a scenario load adds its modules and never removes any (`rpc modules` can still set any set). sessiond writes `modules.el` and restarts the daemon with buffer restore (backend.md §3.3, §3.12); checkpoint restores never change `modules.el`. Part 0 runs `base agent`; Part 13 reaches the full set.

Authors: a lesson must pass with more modules than it lists, since a learner who has gone further or replays has more on (from Part 2 `C-x C-b` is ibuffer; the windows module closes a project's tab after `C-x p k`). Replays meant to match an in-order learner start from a fresh workspace.

## 5. Terminal frame

tmux (`-L pg`, session `emacs`) holds the terminal frame so it survives browser reconnects and `make shell` can attach. `images/session/tmux.conf`:

- `default-terminal tmux-256color`; outer `xterm*` features RGB, extkeys, clipboard, hyperlinks.
- `extended-keys always`, `extended-keys-format csi-u`, `escape-time 10` (keeps a split escape sequence together), `focus-events on`, `set-clipboard on`, `allow-passthrough on`.
- No tmux UI: no status line, prefix or bindings, no history; every mouse event goes to Emacs.
- `C-S-<letter>` re-sent as CSI u (tmux, 3.8 included, drops the Shift); `send-keys` bypasses this.
- `fill-character " "`: blanks, not 3.8's grey `~`, around a window smaller than its client.
- `remain-on-exit on` and a global `pane-died` respawn hook, overridden per session so only sessiond respawns (backend.md §3.3).

Keys: the browser's CSI u chords (frontend.md §5) pass through tmux to `kkp`. kkp waits for a protocol answer tmux never gives, so `base.el` installs its decoders directly in tmux frames (`playground--kkp-tmux`). It also maps tmux's Home `ESC [ 1 ~` and End `ESC [ 4 ~` to `<home>`/`<end>` (kkp knows only rxvt's 7 and 8) and unmodified Delete `ESC [ 3 ~` to `<deletechar>`, since `<delete>` becomes DEL through `local-function-key-map`; modified keys keep kkp's names (`C-<delete>` is `kill-word`). A keyboard macro records raw bytes, so when recording ends, sequences `input-decode-map` turns into one key become that key (F3 is `<f3>` in `C-x C-k RET`, not `ESC O` and `R`).

Mode lines: selected violet `#7a58c4`, others `#232639` (lessons say "the bright mode line"), as palette overrides set before the theme loads. Housekeeping minors have no lighter (ws-butler, apheleia, jinx, ocaml-eglot); ws-butler is not in the state's `minors` either. `extended-command-suggest-shorter` is off (its tip masked M-x answers); key-binding suggestions stay.

Clipboard: OSC 52 through tmux; paste arrives bracketed. `xterm-mouse-mode` is on (not automatic behind tmux).

## 6. GUI frame

Xvnc on `:1` (backend.md §3.3), no window manager, started on demand. Fonts: Iosevka Nerd Font, Noto CJK fallback. Posframe-style popups only in graphical frames; `pop-up-frames` off.

The browser asks Xvnc for a screen the size of its pane (RandR). Without a window manager `fullscreen` applies only at creation, so the GUI frame is made without it and `base.el` fits every GUI frame to the monitor whenever it is made or the screen changes (`playground-fit-frame-to-display`, pixelwise, minus menu bar and fringes); it does nothing under a window manager or outside the playground daemon.

## 7. Daemon and frames

`emacs --fg-daemon=playground`; terminal frame `emacsclient -t -s playground` in tmux, GUI frame `emacsclient -c -s playground` on `:1`. `C-x C-c` closes only its frame, without asking about unsaved files; sessiond respawns the terminal frame. A frame that opens again shows the most recent file buffer, not `*scratch*`.

Saving: `auto-save-visited-mode` saves file buffers 5 s after the last edit, without format on save (apheleia skips those saves; `C-x C-s` formats even when one has already written the buffer), but never over a file changed on disk since visited (`auto-save-visited-predicate`; otherwise a timer would ask "Save anyway?" after a lesson reset). Unmodified buffers auto-revert. No backup, auto-save or lock files. sessiond saves modified buffers before the daemon stops (backend.md §3.3).

After a daemon restart sessiond re-applies its latest `snapshot` (taken every 30 s and before restarts and checkpoints) with `open`.

## 8. Packages and pinning

Sources, preferred first: GNU ELPA, NonGNU ELPA, MELPA Stable, MELPA. `packages.el` lists top-level packages per module; `tools/lock.el` resolves them into `packages.lock` (version, archive, file, sha256), skipping what Emacs ships. Every locked tarball is vendored in `vendor/`: GNU and NonGNU ELPA drop superseded releases and MELPA keeps no old snapshots, so a build never depends on an archive still serving a pinned version. The image installs exactly the lock with no archive configured, failing on any hash mismatch. Pinned packages are read-only in `/opt/playground/elpa`; the learner's own go to `/work/emacs.d/elpa` (Part 13).

## 9. Prefetched project dependencies

Sessions have no egress (deployment.md §9.2), so the image runs cargo and uv offline (`CARGO_NET_OFFLINE`, `UV_OFFLINE`; the gateway sets them to false/0 for `egress: all`) against prefetched dependencies, listed in `images/session/prefetch/` where authors extend them: `cargo/` (crates, a registry cache copied to `$CARGO_HOME` under `~/.cache` at start), `python.in`/`python.txt` (hashed wheels, uv's `UV_FIND_LINKS`), `opam.txt` (libraries in the switch), `typst.txt` (packages under `TYPST_PACKAGE_PATH`), `texlive.txt`. Fixtures should omit `Cargo.lock` and `uv.lock` or resolve to the prefetched versions. The remote box gets the same crates and wheels.

## 10. playground-agent.el

A global minor mode using built-in libraries only. It reports and answers sessiond's calls; it never changes editing behaviour. Hook code is wrapped so an agent bug cannot break a command. Compile it with `byte-compile-error-on-warn` after editing: an unescaped quote in a docstring can become a silent runtime error in a hook.

### 10.1 Hooks and emissions

- Command hooks → `key`, then `state`. `cmd` is `real-this-command`, since commands rewrite `this-command` (C-k is `kill-line`, M-y `yank-pop`, TAB in a prompt `minibuffer-complete`). M-x (advised) reports the command run with `mx: true`. Internal events (focus, frame switch, mouse movement) and `which-key-C-h-dispatch` send nothing and do not change the learner's frame. An unbound key is reported once, with `cmd` `undefined`. `keys` are those saved by `pre-command-hook` (Embark's `C-.` reads its action key itself); a prompt the running command opens and closes itself (Embark's action) reports nothing more.
- Messages and command errors → `echo`, minus noise (`playground-agent-echo-noise`: isearch's prompt, eldoc) and minus a repeat with no key read since (`num-input-keys`).
- Minibuffer and isearch start and end → `minibuffer` (`kind: isearch` for isearch) and `state`.
- Window, buffer, major-mode and keyboard-macro changes → one coalesced `state` per command.
- Frame creation and deletion → `frame`; `closed` only when the last frame of its terminal goes. A new client frame gets the learner's windows (`window-state-get` without pixel sizes); when the last graphical frame goes, its windows return to the terminal frame.
- Loops that read their own keys (`popup-menu`, avy, which-key paging; `playground-agent-waiting-loops`) set the state's `waiting` with the keys that leave them.
- `compilation-start-hook` stamps each run's start (for `reset-step`).

### 10.2 State snapshot

`playground--state` builds api.md §5's state for the learner's window (`json-serialize`). Windows get stable small ids; `layout` is `("vsplit"|"hsplit" child…)` over them; `minors` keeps only `playground-agent-interesting-minor-modes`. Identical consecutive states are suppressed. The minibuffer prompt is the displayed text (invisible spans dropped, string `display` properties shown).

### 10.3 Ops and commands

Ops run in the learner's window of the learner's frame: the newest graphical frame if any, else the frame of the last command (or the client frame made last), never whatever a timer has selected (possibly the daemon's invisible frame). So `selected-window` in a condition means the learner's window.

- `check {forms}`: each form under `condition-case` and a 1 s timeout → `t`, `false` or `"unknown"`.
- `eval {form}`: value as JSON (symbol-keyed alists become objects).
- `rescue`: leaves a waiting loop by queueing its leave keys (a throw would leave a tty menu drawn), then minibuffers and recursive edits, isearch, macro recording, overwrite mode, narrowing, multiple cursors, iedit, extra windows (keeping a normal file window), `special-mode` buffers, the selection and pending input; returns its actions as sentences. Never saves, kills file buffers or reverts.
- `snapshot`: the `WindowSpec` (api.md §7). `windowState` is `window-state-get` with each buffer entry's file added, stripped of text properties (uniquify can leave an unprintable buffer object in a name). Files, not buffer names, identify buffers across a step (a second `README.md` renames the first). Dired, ibuffer and buffer-list windows record how to remake them (directory or filters, marks, line); `tabs`/`tabIndex` and `specialBuffers` (help buffers with their `help-xref-stack-item`) complete it.
- `open {spec}`: visits files, selects the spec's tab, then `window-state-put` with buffer entries renamed to the buffers now visiting those files and list buffers remade; else applies `layout`. Without a client frame the layout waits for the next one (`layoutDeferred`).
- `reset-step {spec}` (every checkpoint restore): reverts changed or modified file buffers; kills buffers of vanished files and unmodified workspace file buffers the spec does not list (so conditions are false again and names un-uniquify); remakes help buffers; kills compilation runs started after the spec unless a spec window shows them (a demo's build must not satisfy the learner's step); makes the tabs the spec's; then `open`s the spec.
- `setup {open, layout, eval, compile, workspace, reset, root}`: leaves any prompt; reverts buffers whose files changed on disk and kills those whose files are gone. With `reset`: re-enables `auto-save-visited-mode`, closes other tabs, clears unmodified leftover buffers, forgets save-place positions under `root`, starts `open` files at line 1. Then opens, lays out, evaluates `eval` in the learner's window, and runs `compile` in the first file's project root.
- `set-modules {enabled}`: writes `modules.el` atomically; sessiond restarts.
- `describe {keys}` or `{cmd}`: binding and first docstring line (`cmd ""` for prefixes and unbound keys). `frame {display?}`: the GUI frame (§6).

Side windows: layouts, window states, `single` setups and Rescue select a normal window before deleting the others (`playground--leave-side-window`) and make a sole window still marked as a side window ordinary (`playground--unside-sole-window`); splitting such a window recurses to `max-lisp-eval-depth`.

Known limits: `reset-step` leaves buffer-local minor modes a demo turned on; what else a restore omits is in backend.md §3.9.

### 10.4 Socket client

A local socket with a line filter (lines over 1 MB dropped), reconnect backoff 1 to 16 s and a 2 s `ping`. `process-send-string` blocks on a full socket, so after 5 s without a pong output is queued (1000 entries; oldest events dropped, never replies); a peer silent for 10 s is dropped.

`call`s run from a timer at top level under `with-timeout` and `inhibit-interaction`: an op that would prompt fails instead of blocking every later call. Setups that might ask (killing a buffer with a live process) must avoid the question. Ops may throw to the command loop (setup, rescue) and reply from a follow-up timer. Timers run with quitting inhibited, so three C-g in a row (or sessiond's SIGUSR2) stop a runaway eval.

C-g during redisplay: a C-g read while redisplay runs Lisp (typically `jit-lock-context-time` refontification 0.5 s after an edit) quits that Lisp, is caught by redisplay ("Error during redisplay: (jit-lock-function N) signaled (quit)") and never runs `keyboard-quit`, so the region stays active. Nothing can recover it, so demos pause about a second before a C-g that follows an edit.

### 10.5 Performance

`post-command-hook` under 0.3 ms median; no polling timers but the ping.

### 10.6 Testing

`make emacs-test` runs `test/agent-test.el` (against a fake socket server) and `test/config-test.el` with `test/modules-test.el` (init.el with base and editing: tmux keys, mode-line contrast, M-y staying `yank-pop` until the completion module of lesson 4.2, GUI frame fit), which runs `test/all-modules-test.el` in a child Emacs (every module: no load errors or missing packages, the keys of Parts 4 to 13, vterm's and jinx's modules, `epdfinfo`, grammars, tools on PATH, language servers, dape adapters, Org's Rust blocks, compile's error regexps and cargo's two-line messages arriving in two chunks). sessiond's integration test (backend.md §6) covers the agent end to end.

## 11. Standalone use

The image works as a plain Emacs environment (`docker run -it --rm -v "$PWD:/work" …/session emacs -nw`); `PLAYGROUND=0` turns off the agent and the frame-only `C-x C-c`. The config also runs on any Emacs 31 with the toolchains (lesson 13.6 starts it on the remote box, which has Emacs and the pinned packages).
