# Emacs side: image, configuration, frames, agent

Everything that lives inside the session container except sessiond: the Emacs build, toolchains, the configuration modules, the terminal and GUI frames, package pinning, and `playground-agent.el`.

## 1. Session image

Debian trixie, multi-stage `images/session/Dockerfile` (build context: the repository root). Heavy stages are cached independently and rebuild only when their own pins in `images/session/versions.env` change:

| Stage | Produces |
|---|---|
| `pins` | `versions.env` split per stage |
| `sessiond` | static sessiond binary |
| `emacs-build` | Emacs 31.1 in `/usr/local`, native code for every Lisp file, tree-sitter grammars |
| `rust`, `python`, `fonts` | the pinned Rust toolchain in `/opt/rust`; uv, basedpyright and ruff in `/opt/uv`; Iosevka Nerd Font |
| `toolchains` | runtime base: Debian packages (tmux 3.5a, TigerVNC Xvnc, xdotool, ripgrep, fd, git, Noto CJK, gcc for native code) plus the stages above |
| `packages` | the packages of `packages.lock`, sha256-checked, byte- and native-compiled into `/opt/playground/{elpa,eln-cache}` |
| `config` | `/opt/playground/emacs.d` (pristine config, agent compiled), `/etc/tmux.conf`, helpers in `/opt/playground/bin`, `versions.env` |
| `final` | sessiond, user `learner`, environment, entrypoint |

Emacs configure flags: `--with-native-compilation=aot --with-tree-sitter --with-modules --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2 --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3 --without-pop`, plus the default image formats. JSON support is built in (Emacs 31's configure rejects `--with-json`). Image libraries matter: `image-mode`, `image-dired`, SVG and inline Org previews need them at build time. Lucid rather than GTK: lighter, no GTK-under-VNC quirks, and the daemon survives the X connection going away. Not pgtk: that build cannot serve terminal and X frames from one daemon the way the playground needs. Fallback: Emacs 30.2 with the same flags if a required package breaks on 31.

Users and paths: `learner` (uid 1000), home `/home/learner`, workspace `/work`, pristine config `/opt/playground/emacs.d` (read-only), live config `/work/emacs.d` (symlinked from `~/.config/emacs`; the daemon runs with `--init-directory=/work/emacs.d`), `XDG_RUNTIME_DIR=/run/playground` so the server socket is `/run/playground/emacs/playground`, agent socket `/run/playground/agent.sock`, toolchain versions in `/opt/playground/versions.env` (reported by `GET /status`). The root filesystem may be read-only; `/tmp`, `/run` and `/home/learner/.cache` are tmpfs. Native code compiled at run time (only the learner's own Lisp) goes to `~/.cache/emacs/eln-cache`, else `/work/.playground/eln-cache` when the cache is mounted `noexec`, else nowhere (`early-init.el`).

Size: the slice-1 image is about 3.4 GB unpacked; a cold build takes about 9 minutes, a config-only change about 15 seconds. With every toolchain of §2 the budget is about 5.5 GB (TeX Live is the largest part).

## 2. Toolchains

| Language or tool | Install | Pieces |
|---|---|---|
| Rust | rustup, pinned stable release | rust-analyzer, clippy, rustfmt, rust-src; cargo-nextest, codelldb (for dape) |
| Python | uv | basedpyright, ruff; debugpy, ipython, pytest |
| OCaml | opam 2.x | dune, ocaml-lsp-server, ocamlformat, utop, merlin, ocamlearlybird, ppx_expect |
| Typst | release binary | typst, tinymist |
| TeX | TeX Live scheme-medium | latexmk, texlab, synctex, biber, a curated package list |
| Markdown | | marksman, pandoc |
| Shell and misc | | bash-language-server, shellcheck, ripgrep, fd, git, delta, jq, tmux 3.5, TigerVNC `Xvnc`, xdotool, enchant (`en_US`, `zh_CN`), poppler utils, fontconfig |
| Fonts | | Iosevka Nerd Font (also served to the browser), Noto CJK, Libertinus, Noto Color Emoji |

Slice 1 installs Rust (toolchain, rust-analyzer, clippy, rustfmt, rust-src), Python (uv, basedpyright, ruff), the shell basics, tmux, Xvnc, xdotool, Iosevka Nerd Font and Noto CJK; the rest is `TODO(emacs, emacs.md §2)` in the Dockerfile. Versions are pinned in `versions.env`.

## 3. Configuration layout

```
emacs.d/
  early-init.el       GC tuning, no package.el at startup, frame defaults, where run-time native code goes
  init.el             makes packages available, reads modules.el, loads the modules; never edited by the course
  modules.el          written by the playground: (setq playground-modules '(base agent ...))
  modules/
    base.el           modus theme and mode-line colours, savehist, recentf, saveplace, auto-revert,
                      auto-save-visited, uniquify, sane defaults, C-x C-c closes the frame only,
                      xterm-mouse, OSC 52, kkp in tmux frames, GUI fonts and frame fit
    agent.el          playground-agent-mode, always on in the playground
    editing.el        avy, expreg, multiple-cursors, iedit, vundo, visual-replace, kmacro helpers, ws-butler
    completion.el     vertico, orderless, marginalia, consult, embark, corfu, cape, tempel
    files.el          dired, dirvish, project.el, consult-dir, bookmarks, ibuffer groups
    windows.el        windmove, ace-window, winner, popper, display-buffer-alist rules, tab-bar, burly, olivetti
    ui.el             nerd-icons, mode line, breadcrumb, pulsar, line numbers, ligatures (GUI frame only)
    terminals.el      eat (default), eshell, compile tweaks, async-shell
    lsp.el            eglot, flymake, eldoc-box, apheleia, dape, consult-eglot
    lang-rust.el      rust-ts-mode, cargo via compile, clippy, rustfmt
    lang-python.el    python-ts-mode, ruff, the Python shell
    git.el            magit, diff-hl, git-timemachine
    lang-ocaml.el, writing.el, org.el, power.el, remote.el, input.el
                      stubs in slice 1 (tuareg; markdown, typst, auctex, pdf-tools, jinx; org; wgrep, wdired,
                      ediff, edit-indirect; tramp; input methods)
  lisp/playground-agent.el
  user-lisp/          the learner's own files (Emacs 31 compiles and autoloads them)
  packages.el         top-level packages per module; tools/lock.el resolves them into packages.lock
  packages.lock       name, version, archive, file, sha256 per package, dependencies first
  vendor/             MELPA tarballs the lock pins (MELPA does not keep old snapshots)
  tools/              lock.el (regenerate the lock), install.el (the image's installer)
  test/               ERT tests (§10.6)
```

Every module is a readable file with a header saying what it adds, why, and which lesson introduces it. `init.el` loads the modules in the order of `modules.el`; a module that fails is reported (a warning, and the agent's `hello` carries `moduleErrors`) and the others still load. The agent is part of the playground, not of the learner's configuration: `agent.el` prefers the compiled copy in the image (`/opt/playground/emacs.d/lisp`) so it always speaks the same protocol as sessiond.

The pristine config is copied to `/work/emacs.d` on the first start only. After changing `emacs.d/modules`, rebuild the session image and reset the workspace (`make reset-workspace`).

## 4. Module unlocking

Scenarios declare `modules`. Unlocking is monotonic: a scenario load adds its modules to the enabled set and never removes any (`rpc modules` can still set any set). sessiond writes `modules.el` and restarts the daemon with buffer restore (backend.md §3.3, §3.12); checkpoint restores keep `modules.el` as it is. Part 0 runs `base agent`; the full set is reached in Part 12, where the learner reads and edits these files.

Consequence for authors: a lesson must pass with more modules than it lists, because a learner who has gone further, or replays an earlier lesson, has more on. For example lesson 0.2's buffer-list step accepts both the plain buffer list and ibuffer (the files module makes `C-x C-b` ibuffer), and the windows module closes a project's own tab after `C-x p k`. Replays meant to match a learner going through the course in order start from a fresh workspace.

## 5. Terminal frame

tmux (socket `-L pg`, session `emacs`) holds the terminal frame so it survives browser reconnects and so `make shell` can attach to the same Emacs. The learner never interacts with tmux. `images/session/tmux.conf` (installed as `/etc/tmux.conf`):

- `default-terminal tmux-256color`; `terminal-features` for `xterm*`: RGB, extkeys, clipboard, hyperlinks. The outer terminal (xterm.js) is `xterm-256color` with `COLORTERM=truecolor`.
- `extended-keys always`, `extended-keys-format csi-u`, `escape-time 10` (a lone ESC waits 10 ms, so an escape sequence split across two reads stays one sequence), `focus-events on`.
- `set-clipboard on`, `allow-passthrough on`.
- No tmux user interface: `status off`, no prefix, every binding removed, `history-limit 0`, `window-size latest`, `mouse on` with every mouse event sent to Emacs.
- `C-S-<letter>` re-sent as CSI u (`ESC [ <code> ; 6 u`): tmux 3.5a turns Control+Shift+letter into plain Control+letter. These bindings see only keys typed through an attached client; `tmux send-keys` bypasses them.
- `remain-on-exit on` and a global `pane-died` hook that respawns the pane. sessiond overrides the hook per session, so in the playground only sessiond respawns it (backend.md §3.3).

Keys: tmux never forwards Emacs's kitty-keyboard push to the outer terminal, so xterm.js stays in the legacy encoding. The browser's terminal pane sends CSI u itself for exactly the chords the legacy encoding cannot carry (C-;, C-=, C-?, C->, C-1, C-S-a, C-RET, S-RET, C-DEL; `web/src/components/screen/extkeys.ts`); tmux parses them and passes them to Emacs as CSI u. In Emacs, `kkp` decodes them. kkp switches itself on only after the terminal answers the kitty protocol query, which tmux never does, so `base.el` installs kkp's decoders directly in tmux frames (`playground--kkp-tmux` on `tty-setup-hook`) for every `ESC [ <digit>` and `ESC [ <letter>` sequence.

Those decoders also see the keys tmux spells its own way: Home `ESC [ 1 ~`, End `ESC [ 4 ~`, Delete `ESC [ 3 ~`. `base.el` adds 1 and 4 to kkp's table of `~` keys (`<home>`, `<end>`; kkp only knows rxvt's 7 and 8) and reads unmodified Delete as `<deletechar>` (`delete-forward-char`), because `<delete>` would become DEL through `local-function-key-map`. Modified keys keep kkp's names: `C-<delete>` is `kill-word`, `C-<end>` is `end-of-buffer`.

Mode lines: the selected window's mode line is violet (`#7a58c4`, white text), the others dim (`#232639`, grey text); lessons point at "the bright mode line" to show which window is selected. These are `modus-vivendi-tinted-palette-overrides`, set before the theme loads. Housekeeping minor modes stay out of the mode line (ws-butler's lighter is hidden) and out of the state's `minors`.

Clipboard: Emacs sets the selection through OSC 52 (`xterm-tmux-extra-capabilities` includes `setSelection`); tmux forwards it with `set-clipboard on`; the browser writes it to the system clipboard when the page has focus and ignores refusals. Paste from the browser goes through bracketed paste.

Mouse: `base.el` turns on `xterm-mouse-mode` (behind tmux Emacs does not by itself); clicks, dragging window dividers and the wheel work.

Font: the browser loads Iosevka Nerd Font so nerd-icons glyphs render.

## 6. GUI frame

Xvnc on `:1` (command line in backend.md §3.3), no window manager. The GUI frame is started on demand and torn down when the learner switches back; its only consumer is the noVNC pane through sessiond. Fonts through fontconfig: Iosevka Nerd Font as default, Noto CJK as fallback for Han, Kana and Hangul (set when the first graphical frame appears). Popups such as posframe are used in GUI frames only (the config checks `display-graphic-p`); `pop-up-frames` is off.

The frame covers the X screen exactly and follows it when the screen changes size: the browser asks Xvnc for a screen as large as its pane (noVNC `resizeSession`, RandR in X), so the frame is drawn at the size it is shown instead of being scaled. Without a window manager `fullscreen` only takes effect when a frame is made, so `base.el` fits every GUI frame of the playground daemon itself (`playground-fit-frame-to-display`, from `after-make-frame-functions` and `display-monitors-changed-functions`): it moves the frame to the monitor's origin, gives its text area the monitor's size minus what the menu bar, internal border and fringes take (`frame-resize-pixelwise`), and clears `fullscreen`, which would fight an explicit size. Nothing happens when a window manager runs (`_NET_SUPPORTING_WM_CHECK` on the root window) or outside the playground daemon.

## 7. Daemon and frames

`emacs --fg-daemon=playground`. The terminal frame is `emacsclient -t -s playground` inside tmux; the GUI frame is `emacsclient -c -s playground` on `:1`. In the playground, `C-x C-c` (`save-buffers-kill-terminal`, advised in `base.el`) closes only that frame without asking about unsaved files, and says so; sessiond respawns the terminal frame and the browser explains. A frame that opens again shows the most recent file buffer, not `*scratch*`.

Saving: `auto-save-visited-mode` saves file buffers 5 s after the last edit, silently, and never over a file that changed on disk since it was visited (`auto-save-visited-predicate`): after a lesson reset that would ask "Save anyway?" from a timer. `global-auto-revert-mode` makes unmodified buffers follow their files. No backup, auto-save or lock files are written. Before the daemon stops (restart, module change, shutdown) sessiond saves the remaining modified file buffers whatever the mode (backend.md §3.3).

Buffer restore after a daemon restart: sessiond takes a `snapshot` (api.md §7) every 30 s and before each restart and checkpoint; after the restart, `open` re-applies it, deferring the layout to the first client frame when none exists yet.

## 8. Packages and pinning

Sources: GNU ELPA, NonGNU ELPA, MELPA Stable and MELPA, in that order of preference. `packages.el` lists the top-level packages per module (with `:pin` and `:prefer-snapshot` exceptions); `tools/lock.el` resolves dependencies, skips what Emacs already ships recent enough, and writes `packages.lock` with name, version, archive, file and sha256. MELPA snapshot tarballs the lock pins are vendored in `vendor/`. The image build installs exactly the lock, in order, with no archive configured, and fails on a missing file or sha256 mismatch. The pinned set lives read-only in `/opt/playground/elpa`; packages the learner installs go to `/work/emacs.d/elpa`, which is what Part 12 teaches.

Package list by module (abridged; third-party.md has versions and licenses): kkp, clipetty (Emacs < 29 only), modus-themes, ef-themes, vertico, orderless, marginalia, consult, embark, embark-consult, corfu, cape, tempel, avy, expreg, multiple-cursors, iedit, vundo, visual-replace, ws-butler, dirvish, consult-dir, ace-window, popper, burly, olivetti, magit, diff-hl, git-timemachine, eat, eldoc-box, apheleia, dape, consult-eglot, pet, markdown-mode, nerd-icons and its dired, completion, corfu and ibuffer companions, breadcrumb, ligature, pulsar, helpful. Built in: eglot, flymake, project, transient, rust-ts-mode, which-key, org. Planned with their modules: tuareg, dune, utop, merlin-eldoc, edit-indirect, typst-ts-mode, auctex, cdlatex, pdf-tools, citar, jinx, org-roam, org-present, wgrep, rime.

## 9. Prefetched project dependencies

For every curriculum project, the image runs the project's dependency fetch at build time (`cargo fetch` into a shared `CARGO_HOME`, the opam switch with the lesson's libraries, `uv sync` caches, typst packages, TeX packages), so lessons work with egress blocked; adding new dependencies needs egress to the registries (deployment.md §3). Not in slice 1: `TODO(emacs, emacs.md §9)`.

## 10. playground-agent.el

A global minor mode with no dependencies beyond built-in libraries (`emacs --batch -l lisp/playground-agent.el` loads it alone). It reports what happens and answers sessiond's calls; it does not change editing behaviour. Everything it runs from hooks is wrapped so that an agent bug can never break a command.

### 10.1 Hooks and emissions

| Hook | Emits | Notes |
|---|---|---|
| `pre-command-hook`, `post-command-hook` | `key`, then `state` | `keys` are the keys saved before the command ran (commands that read keys, like C-h k, change `this-command-keys`; a command that signals has lost them by `post-command-hook`). `cmd` is `real-this-command`, not `this-command`: commands rewrite the latter to steer the next one (`kill-line` leaves `kill-region`, `yank-pop` leaves `yank`, `yank` on an empty kill ring leaves `t`, TAB in a prompt leaves `completion-at-point`). Prefix-argument commands are recognised before they run. M-x (`execute-extended-command`, advised) reports the command run by name with `mx: true` and the reader's keys. Internal events (focus, frame switches, mouse movement) send nothing and do not make their frame the learner's. `which-key-C-h-dispatch` (C-h after a prefix) sends nothing either: the prefix is still pending after it |
| `set-message-function`, `command-error-function` | `echo` | noise filtered (`playground-agent-echo-noise`; "Mark set", "Quit" and "Wrote …" are kept, isearch's prompt (with or without its `isearch-lazy-count` "1/4 ") and eldoc are dropped). A message equal to the last one sent with no key read since (`num-input-keys`) is the same message shown again (in another frame's echo area, or put back by `map-y-or-n-p` after `auto-save-visited-mode` saved from its timer) and is not sent. Echo messages never make `key` events: Emacs 31 runs `undefined` for an unbound sequence between the command hooks, and `post-command-hook` reports it with `cmd` `undefined` |
| `minibuffer-setup-hook`, `minibuffer-exit-hook` | `minibuffer`, `state` | the command that opened a prompt is reported when the prompt opens |
| `isearch-mode-hook`, `isearch-mode-end-hook` | `minibuffer {kind: isearch}` | isearch does not use the minibuffer |
| window configuration, selection and buffer change hooks, `after-change-major-mode-hook`, `kbd-macro-termination-hook`, kmacro start and end advice | `state` | coalesced to one per command or change |
| `server-after-make-frame-hook`, `delete-frame-functions` | `frame` | `closed` only when the last frame of its terminal goes away. A new client frame gets the windows of the frame the learner was in (`window-state-get` without pixel sizes, so a GUI state fits a terminal frame), and when the last graphical frame goes away its windows go back to the terminal frame: switching between the terminal and the GUI keeps the lesson's windows (tabs stay with their frame) |
| advice on `popup-menu`, `avy-read`, `avy--read-candidates`, `which-key-C-h-dispatch` (`playground-agent-waiting-loops`) | `state` | these loops read keys of their own; while one runs the state's `waiting` names it and the keys that leave it |
| `compilation-start-hook` | | stamps each compilation run with its start time (for `reset-step`) |
| `eglot--connect` advice | `echo` | "Starting the language server … for …" |

### 10.2 State snapshot

`playground--state` builds the state object of api.md §5 for the learner's window (§10.3) and serialises it with `json-serialize` (the builders use `:false`, `:null` and vectors explicitly). `windows` lists the live windows of the learner's frame with a stable small id per window; `layout` reduces the window tree to `("vsplit"|"hsplit" child…)` over those ids; `minors` keeps only `playground-agent-interesting-minor-modes`; `diagAtPoint` reads Flymake diagnostics at point. A state identical to the last one sent is suppressed.

### 10.3 Ops and commands

Ops run with the learner's window selected in the learner's frame: the frame of the last command (or the client frame made last), not whatever frame Emacs has selected when a timer runs, which after a client frame closes can be the daemon's invisible frame. While a graphical frame exists the newest one is the learner's frame even when the last command came from the terminal: graphical frames exist only in sessiond's GUI mode, where the browser shows them. Checks, states, snapshots, setups, `open` and `reset-step` all use it.

- `check {forms}` (`playground-check`): each form under `condition-case` and a 1 s `with-timeout`: `t`, `false`, or `"unknown"` on error or timeout.
- `eval {form}`: the value as JSON; alists with symbol keys become objects, unserialisable values their printed form.
- `rescue` (`playground-rescue`): first leaves a waiting loop (a terminal menu, avy, which-key's pages) by putting its leave keys in `unread-command-events` and waiting for it to end (a throw would leave a tty menu drawn over a frame that no longer redisplays); then leaves minibuffers and recursive edits (a throw to top level; the rest runs from a timer and replies from there), stops isearch and macro recording, and in the visible buffers turns off overwrite mode, widens, leaves multiple cursors and iedit; deletes the other windows keeping a normal window that visits a file if any, and makes a sole former side window ordinary again; buries `special-mode` buffers except `*scratch*`; drops the selection; discards pending input. Returns the actions as sentences and shows them in the echo area. Never saves, kills file buffers or reverts.
- `snapshot` (`playground-snapshot`) returns the `WindowSpec` of api.md §7 for the learner's frame:
  - `windowState` is `(window-state-get root t)` with `(file . NAME)` added to each window's buffer entry and the file as a fourth element of each `prev-buffers` entry (`window-state-put` ignores both), printed with text properties removed (uniquify can leave a buffer object in a name's properties, which would print unreadably). Buffer names do not identify buffers across a step: a second `README.md` renames the first `README.md<hello>`.
  - A window on a list buffer records how to make it again: Dired `(dired . DIR)` with the file on the window's line and the marks; ibuffer `(ibuffer . t)` with filters, marks and the buffer on the line; the buffer list `(buffer-menu . t)`.
  - `tabs` are the tab names in order (`""` for a tab named after its buffer), `tabIndex` the current one; `specialBuffers` the help buffers and the help command that made each (`help-xref-stack-item`), printed readably.
- `open {spec}` (`playground-open`): visits the files, restores points, switches to the spec's tab, then puts the windows back with `window-state-put`, renaming each buffer entry to whatever buffer visits that file now (an entry without a file whose buffer is gone is matched by base name), remaking Dired listings (reverted), ibuffer and the buffer list with point on the recorded line; else applies the `layout` pattern. Without a client frame (right after a daemon restart) the layout is kept for the next client frame (`layoutDeferred`).
- `reset-step {spec}` (`playground-reset-step`), run by every checkpoint restore: reverts file buffers whose file changed or that are modified (the checkpoint is authoritative), kills buffers of files that are gone and Dired buffers of missing directories, kills unmodified workspace file buffers the spec does not list (so the step's conditions are false again and uniquify gives the step's own buffers back their names), makes the help buffers those of the spec, kills compilation buffers whose run started after the spec was taken unless a spec window shows them (a demo's build must not satisfy "the build finished" for the learner), makes the tabs those of the spec (named tabs it lacks are closed, listed ones given to a spare unnamed tab beyond the spec's count, as in a GUI frame made after the checkpoint, else reopened from `tab-bar-closed-tabs` or made; the current tab chosen by position when the list matches, else by name, never created by name), then `open`s the spec, the selected window last.
- `setup {open, layout, eval, compile, workspace, reset, root}`: leaves any open prompt first (from a timer, like rescue); reverts buffers whose files changed on disk and kills those whose files are gone (sessiond has just extracted the fixtures, and `find-file-noselect` would otherwise ask). With `reset` it also turns `auto-save-visited-mode` back on when the base module is loaded (some lessons turn it off in their `eval`), closes other tabs and clears the remaining tab's explicit name (3.7 names it), clears the desk (unmodified buffers of other files under the workspace, Dired and buffer lists with stale marks, empty stray buffers made by `C-x b`; modified buffers are kept), forgets save-place positions under `root`, and starts the `open` files at their first line, in their buffers and windows. Then it opens the files, applies the layout (a pattern, a `.eld` window state, or one window on the first file), evaluates `eval` in the learner's window and runs `compile` in the project root of the first file.
- `set-modules {enabled}` (`playground-set-modules`): writes `modules.el` atomically; the restart is sessiond's job.
- `describe {keys}` or `{cmd}` (`playground-describe`): the binding of the keys (`key-binding` in the learner's window) and the first docstring line; prefixes and unbound keys give `cmd ""`.
- `frame {display?}` (`playground-gui-frame`): `make-frame-on-display` as an alternative to `emacsclient -c`, without a `fullscreen` parameter (no window manager; `base.el` fits the frame).
- The minibuffer prompt in the state and in `minibuffer` events is the displayed text: invisible spans are left out and string `display` properties shown (visual-replace keeps three scopes in its prompt and shows one).

Layouts, window states, single-window setups and Rescue never leave a side window as the frame's only window: they select a normal window before deleting the others (`playground--leave-side-window`), and a sole window still marked as a side window is made ordinary again (`playground--unside-sole-window`; splitting such a window recurses until `max-lisp-eval-depth`).

### 10.4 Socket client

`make-network-process :family 'local :nowait t` to `/run/playground/agent.sock` with a filter that splits lines (a line over 1 MB is dropped) and a sentinel that reconnects with backoff (1 to 16 s). `ping` every 2 s. Writes use `process-send-string`, which waits when the socket buffer is full; so while sessiond has not answered a ping for 5 s, events and replies are queued instead of sent, and a peer silent for 10 s is dropped. The queue holds 1000 entries; the oldest events are dropped first, replies never.

`call` messages run from a timer at top level, under `with-timeout` and `inhibit-interaction` (an op that would prompt fails instead of blocking the agent and every later call); ops may throw to the command loop (setup, rescue) and then reply from a follow-up timer. Timers run with quitting inhibited, so three C-g in a row (or sessiond's SIGUSR2) stop a runaway eval.

A C-g that Emacs reads while redisplay runs Lisp is not a key: it quits that Lisp (typically the refontification `jit-lock-context-time`, 0.5 s after an edit), redisplay catches the quit ("Error during redisplay: (jit-lock-function N) signaled (quit)" in `*Messages*`) and `keyboard-quit` never runs, so the region stays active. The quit is raised before any Lisp is entered, so the agent cannot recover the key. Demos that press C-g right after an edit pause about a second first.

### 10.5 Performance

`post-command-hook` cost must stay under 0.3 ms median (building the alist and `json-serialize`). `state` is emitted at most once per command and once per layout change; identical consecutive states are suppressed. No polling timers other than the ping.

### 10.6 Testing

Run in the session image (`make emacs-test`):

- `test/agent-test.el`: `emacs --batch -L lisp -l playground-agent -l test/agent-test.el` against a fake socket server in Elisp: event shapes, rescue from each trap state, reset with a spec, check timeouts, reconnection.
- `test/config-test.el`: early-init.el's helpers; in the image it also loads `test/modules-test.el`, which loads init.el with the base and editing modules and checks what lessons rely on: Home, End and Delete as tmux sends them, the M-x messages, the mode-line colours (WCAG contrast from the Modus palette), hidden lighters, Part 1's M-y (`yank-pop` until the completion module binds `consult-yank-pop` in lesson 4.2), and the GUI frame fit.

Compile the agent with `byte-compile-error-on-warn` after editing it: a docstring with an unescaped quote can turn into a runtime error in a hook that is otherwise silent. sessiond's integration test (backend.md §6) exercises the agent through sessiond.

## 11. Standalone use

The image is a complete Emacs development environment without the playground:

```
docker run -it --rm -v "$PWD:/work" ghcr.io/floatshadow/emacs-playground/session emacs -nw
```

`PLAYGROUND=0` turns the playground behaviour off (the agent and the frame-only `C-x C-c`); everything else works. The config in `emacs.d/` also runs on a normal machine with Emacs 31 once the toolchains it expects are installed; Part 12 of the course walks through that.
