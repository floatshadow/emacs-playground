# Emacs side: image, configuration, frames, agent

Everything that lives inside the session container except sessiond: the Emacs build, toolchains, the configuration modules, the terminal and GUI frames, package pinning, and `playground-agent.el`.

## 1. Session image

Debian trixie, multi-stage `images/session/Dockerfile`:

| Stage | Produces | Cached independently |
|---|---|---|
| `emacs-build` | Emacs 31.1 from the release tarball, installed to `/usr/local` | yes, changes rarely |
| `toolchains` | rustup, opam, uv, typst, TeX Live, node (for a few LSP servers), ripgrep, fd, git, delta, pandoc, enchant, poppler, fonts | yes |
| `packages` | the Emacs packages in `packages.lock`, byte- and native-compiled | yes, changes with the lock |
| `config` | `/opt/playground/emacs.d`, tmux config, Xvnc helpers, prefetched project dependencies | small |
| `final` | sessiond binary, `tini`, user `learner`, entrypoint | small |

Emacs configure flags: `--with-native-compilation=aot --with-tree-sitter --with-modules --with-x --with-x-toolkit=lucid --with-cairo --with-harfbuzz --with-xinput2 --with-rsvg --with-webp --with-imagemagick --with-gnutls --with-sqlite3 --without-pop` (plus the default PNG, JPEG, GIF, TIFF and XPM support, with their dev libraries in the build stage). JSON support is built in since Emacs 30, and Emacs 31's configure rejects `--with-json`. Image support matters: `image-mode`, `image-dired`, SVG rendering and inline Org previews depend on librsvg, libwebp and ImageMagick being present at build time. Lucid rather than GTK: lighter, no GTK-under-VNC quirks, no daemon crash when the X connection drops (an old GTK issue). Not pgtk: that build cannot create terminal frames from a daemon that also serves X frames in the way we need, and we run under X anyway. Fallback: Emacs 30.2 with the same flags if a required package breaks on 31.

Users and paths: `learner` (uid 1000), home `/home/learner`, workspace `/work`, pristine config at `/opt/playground/emacs.d` (read-only), live config at `/work/emacs.d` symlinked from `~/.config/emacs`, Emacs server socket in `/run/playground/emacs/`, agent socket `/run/playground/agent.sock`. The root filesystem is read-only in server mode; `/tmp`, `/run` and `/home/learner/.cache` are tmpfs.

Size budget: Emacs 400 MB, toolchains 3.5 GB (TeX Live medium is the largest), packages 300 MB, prefetched dependencies 800 MB, fonts 100 MB; total about 5.5 GB.

## 2. Toolchains

| Language or tool | Install | Pieces |
|---|---|---|
| Rust | rustup, stable channel pinned to a release | rust-analyzer, clippy, rustfmt, cargo-nextest, codelldb (for dape) |
| OCaml | opam 2.x, switch `5.3` or current | dune, ocaml-lsp-server, ocamlformat, utop, merlin, ocamlearlybird, ppx_expect |
| Python | uv | basedpyright, ruff, debugpy, ipython, pytest |
| Typst | release binary | typst, tinymist |
| TeX | TeX Live scheme-medium | latexmk, texlab, synctex, biber, a curated package list |
| Markdown | | marksman, pandoc |
| Shell and misc | | bash-language-server, shellcheck, ripgrep, fd, git, delta, jq, tmux 3.5, TigerVNC `Xvnc`, xdotool, enchant with `en_US` and `zh_CN` dictionaries, poppler utils, fontconfig |
| Fonts | | Iosevka Nerd Font (also served to the browser), Libertinus (Typst samples), Noto CJK, Noto Color Emoji |

Versions are pinned in `images/session/versions.env` and surfaced in `GET /status`.

## 3. Configuration layout

```
emacs.d/
  early-init.el       GC tuning, no package.el at startup, frame defaults (no toolbar, no scrollbars)
  init.el             loads modules named in modules.el; never edited by the course
  modules.el          written by sessiond: (setq playground-modules '(base agent ...))
  modules/
    base.el           which-key, modus-themes, savehist, recentf, saveplace, auto-revert, auto-save-visited,
                      uniquify, repeat-mode, sane defaults, C-z and C-x C-z unbound, xterm-mouse, OSC 52, kkp
    agent.el          playground-agent minor mode, always on
    editing.el        avy, expreg, multiple-cursors, iedit, vundo, visual-replace, kmacro helpers, ws-butler
    completion.el     vertico, orderless, marginalia, consult, embark, embark-consult, corfu, cape, tempel
    files.el          dired defaults, dirvish, project.el config, consult-dir, bookmarks, ibuffer groups
    windows.el        windmove, ace-window, winner, popper, display-buffer-alist rules, tab-bar setup,
                      burly, olivetti, follow-mode helpers, resize keys, golden-ratio (off by default)
    git.el            magit, diff-hl, git-timemachine, forge (optional)
    terminals.el      eat (default), vterm (alternative, compiled at image time), eshell config, compile tweaks (ansi colours, auto-scroll), async-shell
    lsp.el            eglot config and server programs, flymake, eldoc-box, apheleia, dape, consult-eglot
    lang-rust.el      rust-ts-mode, cargo via compile, clippy flymake backend, rustfmt via apheleia
    lang-ocaml.el     tuareg, dune, utop, merlin-eldoc, ocamlformat
    lang-python.el    python-ts-mode, pet, ruff via apheleia, python shell
    writing.el        markdown-mode, typst-ts-mode, auctex, cdlatex, reftex, pdf-tools, citar, jinx, olivetti
    org.el            org, agenda files, capture templates, babel languages, ox exporters, org-roam, org-present
    power.el          wgrep, wdired, ediff layout, calc, eshell + tramp, ielm, re-builder, line tools
    remote.el         tramp defaults, connection sharing, tramp-container
    ui.el             nerd-icons (dired, completion, corfu, ibuffer), mode line, breadcrumb, pulsar, line numbers, ligature (GUI frame only)
    power.el also     edit-indirect for fenced code blocks (Part 11.6)
    input.el          C-x 8 helpers, TeX input method default, rime (optional)
  lisp/
    playground-agent.el
  user-lisp/          Emacs 31 feature: the learner's own files, auto-compiled and autoloaded
  packages.lock       name, version, archive, sha256 per package
```

Every module is a readable file with a header explaining what it adds and why, grouped by the lesson that introduces it. `init.el` loads modules in the order listed in `modules.el` and reports load errors to the agent instead of failing silently.

## 4. Module unlocking

Scenarios declare `modules`. sessiond writes `modules.el` and restarts the daemon with buffer restore (architecture.md §5.2). Part 0 runs `base agent`; the full set is reached in Part 12, where the learner reads and edits these files. Unlocking is monotonic by default; the course map shows what each part turns on.

## 5. Terminal frame

`tmux.conf` (socket `-L pg`):

```
set -g default-terminal "tmux-256color"
set -as terminal-features ",xterm*:RGB:extkeys:clipboard:hyperlinks"
set -s extended-keys always
set -s extended-keys-format csi-u
set -s escape-time 0
set -g focus-events on
set -g mouse on
set -g set-clipboard on
set -g allow-passthrough on
set -g status off
set -g prefix None
unbind -a
set -g window-size latest
set -g remain-on-exit off
set -g history-limit 0
```

The learner never interacts with tmux. It exists for reconnection and for bring-your-own-terminal attachment. All key bindings are removed; the prefix is unset.

Keys: xterm.js runs with the kitty keyboard protocol extension on; tmux passes CSI u through with `extended-keys always`; `kkp.el` in `base.el` enables the protocol in Emacs (`kkp-enable-in-terminal`). This is what makes `C-;`, `C-RET`, `C-S-s` and `C-M-%` distinguishable. Milestone 1 verifies the full path and falls back to legacy encoding (with those bindings avoided in lessons) if any link fails.

Clipboard: Emacs 31 sets the terminal selection through OSC 52 when `xterm-extra-capabilities` includes `setSelection`; tmux forwards it with `set-clipboard on`; xterm.js's clipboard addon writes it to the browser clipboard. Paste from the browser goes through bracketed paste.

Mouse: Emacs 31 enables `xterm-mouse-mode` by default; clicks, drags on window dividers and scrolling work in the terminal frame.

Font: the browser loads Iosevka Nerd Font so nerd-icons glyphs render; `TERM=tmux-256color`, `COLORTERM=truecolor`.

## 6. GUI frame

`Xvnc :1 -geometry 1600x900 -depth 24 -rfbport 5901 -localhost -SecurityTypes None -dpi 110 -AcceptSetDesktopSize=1`, no window manager; the geometry is the initial size, the browser pane's size follows through RandR, and the Emacs frame fills the display (sized by the agent, not by `fullscreen`, which needs a window manager). Fonts through fontconfig: Iosevka Nerd Font 12 pt as default, Noto CJK as fallback. `pdf-tools` is built at image time (`pdf-tools-install` non-interactive) so `pdf-view-mode` opens PDFs instantly; `image-mode`, SVG, inline Org images and LaTeX previews work. `posframe`-based popups are allowed in GUI frames only; the config checks `(display-graphic-p)`.

The GUI frame is started on demand and torn down when the learner switches back; its only consumer is the noVNC pane through sessiond.

## 7. Daemon and frames

`emacs --fg-daemon=playground` with `server-socket-dir` in `/run/playground/emacs`. The terminal frame is `emacsclient -t -s playground` inside tmux; the GUI frame is `emacsclient -c -s playground` on `:1`. `C-x C-c` in either runs `save-buffers-kill-terminal`, which only closes that frame; sessiond respawns it and the browser explains. `C-x C-c` is additionally advised in `base.el` to skip the "save buffers?" prompt in the playground (auto-save-visited covers it) and to message what happened.

Buffer restore after a daemon restart: sessiond asks the agent for a `snapshot` periodically (every 30 s and on each checkpoint): the list of file-visiting buffers with point, the window layout, the current tab. After restart, `open {files, layout}` re-applies it. Unsaved edits are covered by `auto-save-visited-mode` (5 s).

## 8. Packages and pinning

Sources: GNU ELPA, NonGNU ELPA, MELPA (stable where available). `packages.lock` pins name, version, archive and tarball sha256. `playctl packages update` refreshes the lock against the archives; the image build installs exactly the lock and fails on mismatch. The learner may install more packages at any time (their `elpa/` lives in `/work/emacs.d/elpa`), which is what Part 12 teaches.

Package list by module (abridged): which-key (built in), modus-themes and ef-themes, kkp, clipetty (fallback), vertico, orderless, marginalia, consult, embark, embark-consult, corfu, cape, tempel, avy, expreg, multiple-cursors, iedit, vundo, visual-replace, ws-butler, dirvish, consult-dir, ace-window, popper, burly, olivetti, magit, diff-hl, git-timemachine, eat, vterm, eglot (built in), flymake (built in), eldoc-box, apheleia, dape, consult-eglot, rust-ts-mode (built in), tuareg, dune, utop, merlin-eldoc, pet, markdown-mode, edit-indirect, typst-ts-mode, auctex, cdlatex, pdf-tools, citar, jinx, org (built in), org-roam, org-present, ob-rust, wgrep, nerd-icons, nerd-icons-dired, nerd-icons-completion, nerd-icons-corfu, nerd-icons-ibuffer, breadcrumb, ligature, pulsar, helpful, rime (optional). third-party.md lists versions and licenses.

## 9. Prefetched project dependencies

For every curriculum project, the image runs the project's dependency fetch at build time (`cargo fetch` into a shared `CARGO_HOME`, `opam` switch with the lesson's libraries, `uv sync` caches, `typst` package downloads, TeX packages). Lessons therefore work with egress blocked. The learner adding new dependencies needs egress to the registries (deployment.md §3, `PG_EGRESS_MODE`).

## 10. playground-agent.el

A global minor mode, about 400 lines, no dependencies beyond built-ins. It reports; it does not change editing behaviour.

### 10.1 Hooks and emissions

| Hook | Emits | Notes |
|---|---|---|
| `post-command-hook` | `key` then `state` | `keys` from `(key-description (this-command-keys-vector))`, `cmd` from `this-command` (symbol name; lambdas reported as `<lambda>`); `state` coalesced to one per command |
| `window-configuration-change-hook`, `window-selection-change-functions`, `window-buffer-change-functions` | `state` | layout and window list |
| `minibuffer-setup-hook`, `minibuffer-exit-hook` | `minibuffer`, `state` | prompt from `(minibuffer-prompt)`, contents from `(minibuffer-contents-no-properties)` |
| `isearch-mode-hook`, `isearch-mode-end-hook` | `minibuffer {kind: isearch}` | isearch does not use the minibuffer; reported as a prompt-like state |
| `kmacro` start and end (`kmacro-start-macro`, `kmacro-end-macro` advice, `defining-kbd-macro` watch) | `state` | |
| `after-change-major-mode-hook` | `state` | |
| `server-after-make-frame-hook`, `delete-frame-functions` | `frame` | which frame kind opened or closed |
| `echo-area` watcher via `set-message-function` (Emacs 28+) | `echo` | filters out messages matching a noise list (`Mark set`, `Quit` are kept; `Wrote …` kept; eldoc noise dropped) |
| `tramp` and `eglot` connection hooks | `echo` | "connecting to rust-analyzer…" style progress |

### 10.2 State snapshot

```elisp
(defun playground--state ()
  (let* ((buf (window-buffer (selected-window))))
    (with-current-buffer buf
      `((t . "state")
        (buf . ,(buffer-name)) (file . ,(buffer-file-name)) (mode . ,(symbol-name major-mode))
        (modeName . ,(format-mode-line mode-name)) (minors . ,(playground--active-minors))
        (modified . ,(buffer-modified-p)) (readonly . ,buffer-read-only)
        (narrowed . ,(buffer-narrowed-p)) (overwrite . ,(and overwrite-mode t))
        (line . ,(line-number-at-pos)) (col . ,(current-column)) (point . ,(point))
        (markActive . ,(region-active-p))
        (windows . ,(playground--windows)) (layout . ,(playground--layout))
        (frame . ,(if (display-graphic-p) "gui" "terminal")) (frames . ,(length (frame-list)))
        (tabs . ,(playground--tabs))
        (minibuffer . ,(playground--minibuffer)) (isearch . ,(and isearch-mode t))
        (recursive . ,(recursion-depth)) (macro . ,(and defining-kbd-macro t))
        (prefixArg . ,(and current-prefix-arg (format "%S" current-prefix-arg)))
        (diagAtPoint . ,(playground--diag-at-point))))))
```

`playground--windows` lists each live window of the selected frame with id, buffer, mode, selected, dedicated and side (`window-parameter w 'window-side`). `playground--layout` reduces `window-state-get` to a nested `("hsplit"|"vsplit" child…)` list of window ids. `playground--diag-at-point` reads Flymake diagnostics at point for the echo fallback. Serialisation uses `json-serialize` with alists.

### 10.3 Commands

- `playground-rescue`: in order: `(while (> (minibuffer-depth) 0) (abort-recursive-edit))`; `(isearch-exit)` if searching; `(kmacro-end-macro)` if defining; `(overwrite-mode -1)`; `(widen)`; `(exit-recursive-edit)` loop; delete other windows keeping a window that visits a file if any; bury buffers whose mode derives from `special-mode` except `*scratch*`; `(deactivate-mark)`; `(keyboard-quit)` equivalents via `(discard-input)`. Returns the list of actions taken as strings. Never saves, kills file buffers, or reverts.
- `playground-check FORMS`: evaluates each form with `condition-case`, returns `t`, `nil`, or `"unknown"` on error, with a 1 s `with-timeout` per form.
- `playground-reset-step SPEC`: for each file-visiting buffer: revert if the file exists, kill otherwise (`kill-buffer` without prompts for unmodified; modified buffers are reverted since the checkpoint is authoritative); then `playground-open` with the spec's files and layout.
- `playground-open {files, layout}`: `find-file-noselect` each file, apply the layout with `window-state-put` from a serialised state when present, else a simple split description.
- `playground-snapshot`: the inverse of `playground-open`, used for checkpoints and restart restore.
- `playground-set-modules LIST`: writes `modules.el`; the restart is sessiond's job.
- `playground-describe KEYS`: `(key-binding (kbd KEYS))` and the first docstring line, used by the ticker for commands it does not have a gloss for.
- `playground-gui-frame`: `(make-frame-on-display ":1" …)` as an alternative to `emacsclient -c`.

### 10.4 Socket client

`make-network-process :family 'local :service "/run/playground/agent.sock" :coding 'utf-8 :nowait t` with a filter that accumulates lines and dispatches `call` messages, and a sentinel that schedules reconnects with backoff (1 to 16 s) through `run-with-timer`. Outgoing events are queued when disconnected, capped at 1000. `call` handlers run on the main thread between commands, from a timer (so they may throw to the command loop: setup, rescue); long `eval`s are bounded by `with-timeout`. Timers, like process filters, run with quitting inhibited, so three C-g in a row (or sessiond's SIGUSR2) are what stop a runaway eval. A C-g that Emacs reads while redisplay runs Lisp is not a key either: it quits that Lisp, redisplay catches the quit ("Error during redisplay: (jit-lock-function N) signaled (quit)" in *Messages*) and `keyboard-quit` never runs, so the region stays active. Emacs reads input at that point in its C code, before the Lisp is entered, so the agent cannot recover the key. The refontification 0.5 s after an edit (`jit-lock-context-time`) is the usual moment: a demo that presses C-g about half a second after an edit loses it about one run in ten (lesson 1.3's redo step waits a second first, and its hint tells the learner what a lost C-g looks like). A `ping` timer runs every 2 s.

### 10.5 Performance

`post-command-hook` cost is measured in CI and must stay under 0.3 ms median (building the alist and `json-serialize`). `state` is emitted at most once per command and once per layout change; identical consecutive states are suppressed by comparing a hash. No polling timers other than the ping.

### 10.6 Testing

ERT tests run with `emacs --batch -l lisp/playground-agent.el -l test/agent-test.el` against a fake socket server written in Elisp (`make-network-process :server t`), covering: event shapes, rescue from each trap state, reset with a spec, check timeouts, reconnection. The Go agent integration test (backend.md §6) runs the same scripted scenarios through sessiond.

## 11. Standalone use

The image is a complete Emacs development environment without the playground:

```
docker run -it --rm -v "$PWD:/work" ghcr.io/floatshadow/emacs-playground/session emacs -nw
```

`PLAYGROUND=0` disables the agent module; everything else works. The config in `emacs.d/` also runs on a normal machine with Emacs 31 once the toolchains it expects are installed; Part 12 of the course walks through that.
