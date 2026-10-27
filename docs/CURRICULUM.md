# Curriculum

Part A is the course: fifteen parts and eight projects, each lesson with its objective, keys, the VS Code habit it replaces, and its exercise. Part B is the authoring format that turns a lesson into something the system can run: the Markdown dialect, the scenario schema, checks, hints, tables and the validation workflow.

Conventions: lessons run in the terminal frame unless tagged `gui` (needs the GUI frame) or `remote` (uses the SSH box). "Modules" names the config modules unlocked when a part starts (emacs.md §3). Times are rough.

## A. The course

### Part 0 — Orientation (45 min; modules: base, agent)

- **0.1 What the screen is telling you** (10 min). VS Code: status bar, Problems. You can: name buffer, window, frame, mode line, echo area, minibuffer; read `-:**-`, `L12 C4`, `(Rust Eglot)`. Keys: none new. Exercise: the lesson asks five questions about the screen; answers are checked against the live state (which window is selected, is the buffer modified).
- **0.2 C-g and the three ways to get lost** (12 min). VS Code: Esc. You can: cancel a prompt, a pending prefix and a search; close an extra window; leave a help buffer. Keys: `C-g`, `ESC ESC ESC`, `C-x 1`, `q`. Exercise: open `C-x C-b`, `C-h t` and an isearch on purpose, escape each; the step completes only when every trap has been entered and left.
- **0.3 Asking Emacs** (12 min). VS Code: hover help, command palette. You can: find what a key does, read a command's documentation, see the current mode's bindings, jump to the source of any command. Keys: `C-h k`, `C-h f`, `C-h v`, `C-h m`, `C-h o`, `C-h b`, `C-h ?`, helpful buffers. Exercise: find out what `C-x C-s` runs, read its docstring, follow the link to its source.
- **0.4 Which-key and the key ticker** (8 min). VS Code: keyboard shortcut reference. You can: explore a prefix map without memorising it; read the ticker and the Where am I panel. Keys: any prefix, `M-x`. Exercise: discover three commands under `C-x r` using only the popup.

### Part 1 — Moving and editing (2 h; + editing)

- **1.1 Motion** (15 min). VS Code: arrows, Ctrl+arrows, Home, End, Ctrl+G. Keys: `C-f C-b C-n C-p`, `M-f M-b`, `C-a C-e`, `M-m`, `M-a M-e`, `C-M-f C-M-b` (sexp), `C-M-a C-M-e` (defun), `C-M-n C-M-p`, `M-< M->`, `C-v M-v`, `C-l`, `M-g g`, `C-M-l`. Exercise: navigate `main.rs` to five marked spots without arrow keys; the step counts arrow-key use and nudges after ten.
- **1.2 Kill, yank and the kill ring** (15 min). VS Code: cut, copy, paste, clipboard history. Keys: `C-SPC`, `C-w`, `M-w`, `C-y`, `M-y` (consult-yank-pop), `C-k`, `M-d`, `M-DEL`, `C-x h`, `C-x C-x`, `M-z`. Exercise: reorder three functions in a file using only kill and yank.
- **1.3 Undo that never loses work** (10 min). VS Code: Ctrl+Z, Ctrl+Y. Keys: `C-/`, `C-?` (undo-redo), `C-x u` opens vundo, undo in region. Exercise: recover a deleted paragraph after making five further edits.
- **1.4 Incremental search** (12 min). VS Code: Ctrl+F, F3. Keys: `C-s`, `C-r`, `C-s C-s`, `M-s .`, `M-s o` (occur), `C-M-s` (regexp), `M-c` toggles case, `M-s SPC`, `RET`, `C-g`, lazy match count. Exercise: find every `greet` call and visit them in order; then an occur listing.
- **1.5 Replace** (12 min). VS Code: Ctrl+H, regex mode. Keys: `M-%`, `C-M-%`, `y n ! ^ .`, `\&`, `\1`, visual-replace (`M-%` with preview), case preservation. Exercise: rename a variable using a capture group without touching a similarly named one.
- **1.6 Rectangles and keyboard macros** (15 min). VS Code: column select, repeat last edit. Keys: `C-x SPC`, `C-x r t`, `C-x r k`, `C-x r y`, `C-x r N`, `F3`, `F4`, `C-x C-k n`, `C-x C-k C-c` (counter), `C-x C-k e` (edit macro), `C-x C-k r` (apply to region lines). Exercise: turn a plain list into a numbered Markdown table with one macro.
- **1.7 Multiple cursors and iedit** (12 min). VS Code: Ctrl+D, rename symbol in file. Keys: `C-;` (iedit), `C-> C-<` (mc), `C-c m l` (edit lines), `C-=` (expreg grows the region). Exercise: rename a local variable inside one function only, with iedit's function scope.
- **1.8 Jump anywhere** (12 min). VS Code: Ctrl+G, outline, breadcrumbs, matching bracket. Keys: `M-j` (avy), `M-g i` (consult-imenu), `M-g o` (consult-outline), `C-x r m / C-x r b` (bookmarks), `M-g M-g`, `C-M-f` on a bracket, breadcrumb bar. Exercise: jump to the third `let` on screen with avy in two keystrokes; bookmark a function and return to it from another file.

### Part 2 — Buffers and files (1.5 h; + files)

- **2.1 Buffers are not tabs** (8 min). VS Code: editor tabs. You can: explain buffer versus file; list buffers; recognise special buffers. Keys: `C-x C-b`, `C-x b`, `C-x k`. Exercise: say which of six listed buffers visit files (checked).
- **2.2 The minibuffer** (10 min). VS Code: the palette input box. Keys: `TAB`, `M-n M-p` history, `C-g`, `M-x`, `C-u` arguments, `M-r` search history. Exercise: run three commands by name, re-run one from history.
- **2.3 Opening files** (12 min). VS Code: Ctrl+P, Ctrl+O. Keys: `C-x C-f`, `TAB`, `//` and `~` resets in the path, `C-x C-r` recent, `C-x d`. Exercise: the demo lesson: open `src/main.rs`, change the greeting, save; open `src/` by mistake and leave Dired.
- **2.4 Switching, saving and killing buffers** (12 min). VS Code: Ctrl+Tab, Ctrl+S, Ctrl+W, Save As. Keys: `C-x b`, `C-x C-s`, `C-x s`, `C-x C-w`, `C-x k`, `revert-buffer`, auto-revert, uniquified names (`main.rs<hello>`). Exercise: open four files, cycle through them, save all, kill two, revert one.
- **2.5 Many buffers with ibuffer** (10 min). VS Code: none. Keys: `C-x C-b` (ibuffer), `/ f`, `/ m`, `* m`, `D`, `S`, groups by project. Exercise: kill every `*Help*` buffer in one operation; save all modified Rust buffers.
- **2.6 Dired and the dirvish side window** (15 min). VS Code: Explorer, file operations. Keys: `C-x d`, `RET`, `^`, `m u d x`, `C`, `R`, `+`, `D`, `!`, `&`, `(`, `TAB` subtree, `C-x C-j` side window, icons. Exercise: reorganise the fixtures folder: make `docs/`, move two files, rename one, delete a stray.
- **2.7 Projects** (12 min). VS Code: workspace, Ctrl+Shift+F. Keys: `C-x p f`, `C-x p b`, `C-x p g` and `M-s r` (consult-ripgrep), `C-x p p`, `C-x p k`, `C-x p c`. Exercise: find every `TODO` in the project and visit them; switch projects and back.
- **2.8 Recent files, bookmarks and registers** (8 min). VS Code: recent, Ctrl+Shift+T. Keys: `C-x C-r`, `C-x r m`, `C-x r b`, `C-x r l`, `C-x r SPC` / `C-x r j` (point registers), save-place. Exercise: bookmark three files and jump between them by name.

### Part 3 — Windows, layouts and workspaces (1.5 h; + windows)

- **3.1 Windows and frames** (10 min). VS Code: editor groups, split editor. You can: show the same buffer twice; split and close; explain window versus frame. Keys: `C-x 2`, `C-x 3`, `C-x 0`, `C-x 1`, `C-x o`, `C-x 4 0`, `C-x 5 2`, `C-x 5 0`. Exercise: show `main.rs` in two windows, edit in one, watch the other; end with one window.
- **3.2 Moving between windows** (8 min). VS Code: Ctrl+1/2/3, focus group commands. Keys: `C-x o`, `S-<arrows>` (windmove), `M-o` (ace-window with labels), repeat-mode (`C-x o o o`). Exercise: with four windows, reach each in one chord (the step checks selected-window changes against the layout).
- **3.3 Resizing and balancing** (10 min). VS Code: drag borders, maximise group. Keys: `C-x ^`, `C-x {`, `C-x }`, `C-x -`, `C-x +`, `maximize-window`, `minimize-window`, `fit-window-to-buffer`, mouse drag on dividers (works in the terminal), Emacs 31 `rotate-windows`, `flip-window-layout`, `transpose-window-layout`. Exercise: make the compile window exactly eight lines, then balance, then rotate the layout.
- **3.4 Open things elsewhere** (10 min). VS Code: open to the side, open in new window. Keys: `C-x 4 f`, `C-x 4 b`, `C-x 4 d`, `C-x 4 c` (clone), `C-x 4 4` (next command's buffer in another window), `C-x 5 f`, `C-x 4 1`. Exercise: open `Cargo.toml` beside `main.rs` and a Dired listing below, without the mouse.
- **3.5 Taming pop-ups** (15 min). VS Code: the panel, sidebar behaviour, "preview" editors. You can: decide where `*compilation*`, `*Help*`, Magit and Eshell appear; toggle the panel like VS Code's terminal panel; use side windows and dedicated windows. Keys: `C-`` (popper toggle), `C-x w s` (window-toggle-side-windows), `display-buffer-alist` rules in `modules/windows.el` (read, not written yet). Exercise: make compile output always appear at the bottom at a third of the height and Help on the right; the check reads the resulting layout.
- **3.6 Undo and save layouts** (10 min). VS Code: none, or workspace state. Keys: `C-c <left>` (winner-undo), `C-x r w` / `C-x r j` (window configuration registers), `burly-bookmark-windows` (named layouts), `window-swap-states`, `desktop-save-mode` across restarts. Exercise: build a layout, destroy it, undo it, save it as "review", restore it after a Rescue.
- **3.7 Workspaces with tab-bar** (12 min). VS Code: multiple windows, multi-root workspaces, tab groups. Keys: `C-x t 2`, `C-x t RET`, `C-x t 0`, `C-x t o`, `C-x t r`, `C-x t b`, one tab per project (project switch opens a tab), `tab-line-mode` for those who want per-window tabs. Exercise: `hello` and `polyglot` in two tabs with different layouts; switch without losing either.
- **3.8 Focus and layout recipes** (10 min). VS Code: zen mode, zoom, centred layout. Keys: olivetti, `follow-mode` (two windows scroll as one long page), `C-x C-+`, `C-x C-M-+` (global zoom), `C-x z` repeat, `text-scale-adjust`. Recipes checked by layout: edit and compile; side-by-side implementation and test; writing with preview; wide code and narrow docs. Exercise: reproduce the three recipes from memory.

### Part 4 — Completion everywhere (1 h; + completion)

- **4.1 The minibuffer, upgraded** (10 min). VS Code: fuzzy palette. Keys: `M-x`, space-separated orderless terms, `!` negation, `=` literal, `M-RET` (accept input as typed), marginalia annotations. Exercise: find a command you do not know the name of by describing it in two words.
- **4.2 Consult** (12 min). VS Code: Go to symbol, search across files, Go to line with preview. Keys: `C-x b` (consult-buffer with narrowing `b SPC`, `f SPC`), `M-s l` (consult-line), `M-s r` (consult-ripgrep), `M-g i`, `M-g o`, `M-g m` (marks), `C-x r j` (registers), live preview. Exercise: find the line anywhere in the project where `p99` is defined, through ripgrep with preview.
- **4.3 Embark** (10 min). VS Code: right-click context menu. Keys: `C-.` (act), `C-;` (dwim), `embark-export`, `embark-collect`, acting on files, buffers, symbols, URLs. Exercise: export a ripgrep search to a buffer (prepares 11.4).
- **4.4 Corfu, cape and tempel** (10 min). VS Code: IntelliSense popup, snippets. Keys: `TAB`, `RET`, `C-n C-p`, `M-SPC` separator, `C-M-i`, `cape-file`, `cape-dabbrev`, `M-+` (tempel insert), snippet fields. Exercise: write a function using completion and one snippet only.

### Part 5 — Git with Magit (1.5 h; + git)

- **5.1 Status and staging hunks** (15 min). VS Code: Source Control view. Keys: `C-x g`, `TAB`, `s u S U`, `k` (discard), `c c`, `?`. Exercise: stage two hunks of one file, discard a third, commit.
- **5.2 Commits, log and blame** (15 min). VS Code: GitLens. Keys: `l l`, `l a`, `RET` on a commit, `c a` (amend), `c e`, `b` (blame), `C-x v =`. Exercise: find who changed a line and when, then amend the last commit message.
- **5.3 Branches, rebase and stash** (15 min). VS Code: branch picker. Keys: `b c`, `b b`, `r i` (interactive rebase), `z z`, `z p`, `m m`. Exercise: create a branch, make two commits, squash them, rebase onto main.
- **5.4 Merge conflicts with smerge** (15 min). VS Code: merge editor. Keys: `C-c ^ n`, `C-c ^ u`, `C-c ^ l`, `C-c ^ a`, `C-c ^ E` (ediff). Exercise: resolve a prepared three-file conflict.
- **5.5 diff-hl and git-timemachine** (10 min). VS Code: gutter indicators, file timeline. Keys: `C-x v n`, `C-x v p` (hunk motion), `C-x v r` (revert hunk), `git-timemachine` with `p n q`. Exercise: scroll back through a file's history and copy a lost paragraph from an old version.

### Part 6 — Terminals and running things (1 h; + terminals)

- **6.1 compile and next-error** (12 min). VS Code: Tasks, Problems panel. Keys: `C-c C-c`, `M-x compile`, `g`, `M-g M-n`, `M-g M-p`, `C-x``, `recompile`, compilation regexps for cargo, dune, pytest. Exercise: fix two compiler errors by jumping to them from the compile window.
- **6.2 eat: a terminal in a buffer** (12 min). VS Code: integrated terminal. Keys: `M-x eat`, `eat-project`, `C-c C-j` (semi-char), `C-c C-k` (char), `C-c C-e` (emacs mode), copy and search in terminal output, vterm as the heavier alternative. Exercise: run the test suite in eat, copy a failing test name into the source buffer.
- **6.3 Eshell** (15 min). VS Code: none. Keys: `M-x eshell`, Lisp in the shell (`(+ 1 2)`, `(find-file "x")`), `>#<buffer out>`, `M-r` history, aliases, `$` variables, globbing. Exercise: list the five largest files and open the biggest with one command.
- **6.4 Async shell commands and Dired integration** (10 min). VS Code: run in terminal. Keys: `M-&`, `M-!`, Dired `!` and `&`, `C-u M-|` (replace region with output, a teaser for Part 10). Exercise: convert three images from Dired with one marked command.

### Part 7 — Language servers and debugging (2 h; + lsp, lang-rust, lang-ocaml, lang-python)

- **7.1 Eglot** (15 min). VS Code: IntelliSense, F12, Shift+F12, F2. Keys: `M-x eglot` (auto in projects), `M-.`, `M-,`, `M-?`, eldoc and eldoc-box, `C-c l r` (rename), `C-c l a` (code actions), `consult-eglot-symbols`. Exercise: rename `greet` across the crate and jump to its three references.
- **7.2 Flymake and diagnostics** (12 min). VS Code: Problems, lightbulb. Keys: `M-g n`, `M-g p`, `C-h .`, `M-x flymake-show-buffer-diagnostics`, `flymake-show-project-diagnostics`, code actions. Exercise: clear every diagnostic in a prepared file with the fewest edits.
- **7.3 Format on save with apheleia** (8 min). VS Code: format on save. Keys: `M-x apheleia-format-buffer`, toggling per buffer, formatter table (rustfmt, ocamlformat, ruff). Exercise: save a badly formatted file in each language.
- **7.4 Rust** (15 min). VS Code: rust-analyzer. Keys: `C-c C-c` cargo build, `cargo test` with the compile regexps, clippy as a Flymake backend, inlay hints, `cargo-nextest`. Exercise: add a test, run it, fix a clippy warning.
- **7.5 OCaml** (15 min). VS Code: OCaml Platform. Keys: dune build in compile, merlin type under point (`C-c C-t`), `utop` in a buffer (`C-c C-e` sends a phrase), ocamlformat, expect tests. Exercise: implement a function to satisfy an expect test, using utop to explore.
- **7.6 Python** (15 min). VS Code: Python extension. Keys: pet picking the venv, `C-c C-p` REPL, `C-c C-c` send buffer, ruff fixes via code actions, pytest through compile. Exercise: fix a failing test with the REPL open.
- **7.7 Debugging with dape** (15 min). VS Code: Run and Debug. Keys: `M-x dape`, `dape-breakpoint-toggle`, `n s o c`, watch, REPL; adapters for codelldb, debugpy, ocamlearlybird. Exercise: find a wrong value in the Rust CLI by stepping.

### Part 8 — Writing (2 h; + writing, input)

- **8.1 Markdown with live preview** (12 min). VS Code: Markdown preview. Keys: `C-c C-s b`, `C-c C-l`, `M-RET`, `C-c C-c p`, table alignment, the preview pane updating on save. Exercise: write a README section with a table and a link; preview it.
- **8.2 Typst with tinymist** (15 min). VS Code: Tinymist. Keys: `typst-ts-mode`, eglot with tinymist, `C-c C-c` compile, watch mode routed to the preview pane, errors in Flymake. Exercise: add a figure and a table to `report.typ`; watch the PDF update.
- **8.3 LaTeX with AUCTeX** (15 min). VS Code: LaTeX Workshop. Keys: `C-c C-c` (latexmk), `C-c C-e` environments, `C-c C-s` sections, cdlatex `` ` `` and `'`, reftex `C-c )`, errors `C-c``. Exercise: write a section with an equation and a reference; build.
- **8.4 pdf-tools and SyncTeX** (10 min, `gui`). VS Code: PDF viewer. Keys: `pdf-view-mode` navigation, `C-c C-v` forward search, double-click backward search, annotations, `o` outline, `i` isearch inside the PDF. Exercise: jump from a sentence in the source to the PDF and back.
- **8.5 Images, SVG and inline previews** (10 min, `gui`). VS Code: image preview. Keys: `image-mode` (`+ - r`), `image-dired`, SVG rendering, inline images and LaTeX previews in Org (`C-c C-x C-v`), `C-c C-x C-l`. Exercise: inspect a JPEG and an SVG, then show both inline in an Org file.
- **8.6 Citations with citar** (10 min). VS Code: citation picker. Keys: `C-c C-x @` or citar insert, bibtex-mode, `citar-open`. Exercise: cite two entries in Typst and in LaTeX from one `.bib`.
- **8.7 Unicode and input methods** (12 min). VS Code: unicode input, IME. Keys: `C-x 8 RET` (insert-char by name), `C-x 8` shortcuts (`C-x 8 - -`), `C-\` with the TeX method (`\alpha` → α), `set-input-method`, `describe-char`, `C-u C-x =`, rime or pyim for Chinese when enabled. Exercise: type a line of mathematics with Greek letters and arrows, then a line of Chinese.
- **8.8 Focused writing** (8 min). VS Code: zen mode, spell check, word count. Keys: olivetti, jinx (`M-$`), `M-q`, `C-x f`, visual-line-mode, `count-words`. Exercise: fix every misspelling in a page using only `M-$`.

### Part 9 — Org-mode (2 h; + org)

- **9.1 Outlining and structure editing** (12 min). Keys: `TAB`, `S-TAB`, `M-<up>`, `M-<right>`, `C-c C-n`, `C-x n s`, `C-c C-x C-w`. Exercise: restructure a messy plan into three levels.
- **9.2 TODOs, scheduling, the agenda and capture** (15 min). VS Code: Todo Tree. Keys: `C-c C-t`, `S-<right>`, `C-c C-s`, `C-c C-d`, `C-c a`, `C-c c`, tags `C-c C-q`. Exercise: capture three tasks from other buffers, schedule them, read the week in the agenda.
- **9.3 Tables and spreadsheet formulas** (12 min). Keys: `|` and `TAB`, `C-c =` formulas, `C-u C-c *` recalc, `C-c }` coordinates, column formulas. Exercise: a latency table that computes the mean.
- **9.4 Babel: run code, pass results between languages** (15 min). VS Code: Jupyter. Keys: `C-c C-c` on a block, `#+begin_src rust`, `:results table`, `:var`, `C-c '` (edit in the language's mode with LSP), `:tangle`. Exercise: a shell block produces numbers, a Python block scores them, a Rust block formats the result.
- **9.5 Babel on a remote host** (10 min, `remote`). Keys: `:dir /ssh:box:/tmp`, `:session`. Exercise: run a shell block on the box and tabulate its output locally.
- **9.6 Export** (12 min). Keys: `C-c C-e h o`, `l p`, `l b` (Beamer), `m m`, `org-present`. Exercise: export the plan to HTML and PDF; present it.
- **9.7 Notes with org-roam** (10 min). VS Code: Foam. Keys: `C-c n f`, `C-c n i`, backlinks buffer, daily notes. Exercise: three linked notes and a daily note.
- **9.8 A literate init.org** (10 min). Keys: `org-babel-tangle`, `:tangle`, `#+PROPERTY`. Exercise: move one module into an Org file that tangles to it.

### Part 10 — Text is the interface (1.5 h; + power)

- **10.1 Pipe a region through any tool** (10 min). Keys: `M-|` with `sort`, `jq`, `pandoc`; `C-u M-|` replaces the region; `M-!` and `C-u M-!` insert output. Exercise: sort a list, pretty-print JSON, convert a Markdown table to Org, all in place.
- **10.2 Registers and macros, advanced** (10 min). Keys: text and number registers, `C-x r s`, `C-x r i`, `C-x r n`, `C-x r +`, macro counters and `kmacro-edit-macro`, `apply-macro-to-region-lines`. Exercise: number forty lines with a prefix in one macro.
- **10.3 ediff** (12 min). VS Code: diff editor. Keys: `ediff-buffers`, `ediff-files`, `ediff-directories`, `n p a b`, `|`, `wa`, ediff-merge with ancestor. Exercise: merge two versions of a function, keeping parts of each.
- **10.4 Calc** (12 min). Keys: `C-x * c`, RPN basics, units (`u c`), symbolic algebra (`a s`), `C-x * e` embedded mode in a text buffer, `calc-grab-region`. Exercise: convert the latency table's units and compute a sum inside the document.
- **10.5 Eshell meets TRAMP** (10 min). Keys: `cd /ssh:box:/var/log`, remote `grep`, `cat`, `>` into a local buffer. Exercise: collect three remote log lines into a local Org table.
- **10.6 Change Emacs while it runs** (12 min). Keys: `M-:`, `C-x C-e`, `ielm`, `C-h f` then the source link, re-evaluating a `defun`, `re-builder`, `M-x list-timers`, `M-x proced`. Exercise: change a command's behaviour by redefining it live, then undo by re-loading the file.
- **10.7 Line tools and dynamic abbreviations** (8 min). VS Code: Sort Lines extension. Keys: `sort-lines`, `reverse-region`, `align-regexp`, `keep-lines`, `flush-lines`, `delete-duplicate-lines`, `M-/`, `C-M-/`. Exercise: clean a messy import list.
- **10.8 occur and occur-edit** (8 min). Keys: `M-s o`, `e` (edit), `C-c C-c` (apply), `multi-occur-in-matching-buffers`. Exercise: change a word in every matching line of five buffers at once.

### Part 11 — Everything is a buffer: how packages compose (1.5 h; modules unchanged)

The thesis: packages that were never designed together still work together, because each of them produces or consumes a buffer with a known major mode, and the editing commands, hooks and "thing at point" conventions apply to all of them. Each lesson shows one composition pattern with packages from earlier parts.

- **11.1 The one abstraction** (10 min). You can: explain why a Dired listing, a Magit status, a compile log and a shell are all searchable, copyable and macro-able; name the three glue mechanisms (major modes over text, hooks, thing-at-point). Keys: `C-s` inside Magit, Dired and `*compilation*`; `M-w` on a Dired line; a macro recorded in Dired. Exercise: copy three file names out of a Dired listing and three hashes out of a Magit log with one macro each.
- **11.2 C-x C-q: edit the read-only view** (12 min). You can: rename files by editing the listing (wdired), edit grep results in place (wgrep), edit occur results, edit inside an archive (`archive-mode`, `tar-mode`). One key, four packages that share nothing but the convention. Exercise: rename six files with a regexp replace in wdired; fix a typo in a file inside a tarball without extracting it.
- **11.3 One list of locations** (10 min). You can: use `next-error` after compile, grep, occur, Flymake and xref alike; use `embark-export` to turn any list into one of those buffers. Keys: `M-g M-n`, `M-g M-p`, `C-x``, `embark-export`. Exercise: a session where compile errors, a grep and Flymake diagnostics are all walked with the same two keys.
- **11.4 The search-to-edit pipeline** (15 min). consult-ripgrep → embark-export → wgrep → `C-x C-s` for all → diff-hl marks → Magit commit. Five packages, no integration code between them. Exercise: rename a concept across the project with the pipeline; the step checks the files, the clean Flymake state and the commit.
- **11.5 Shell output is a buffer** (10 min). Eshell and compile output as material: `occur` on it, `M-|` it through `sort`, save it with `C-x C-s`, re-run a macro over it, `C-c C-o` to jump to a path in it. Exercise: turn a test run's output into a Markdown checklist of failing tests.
- **11.6 Narrow and indirect buffers** (10 min). `edit-indirect` for a fenced Rust block in Markdown, `org-edit-special` for Org, `clone-indirect-buffer-other-window`, narrowing; LSP, Flymake and formatting work inside the block because it is a real buffer in `rust-ts-mode`. Exercise: fix a type error inside a Markdown code fence with Eglot's help.
- **11.7 Act on the thing at point** (10 min). `embark-act`, `ffap`, `xref`, `browse-url`, `dired-jump`: a path in compile output opens; a URL in a commit message browses; a crate name in `Cargo.toml` opens its docs; a line in a grep buffer opens in Dired with `embark`. Exercise: six actions on six kinds of things without typing a path.
- **11.8 Hooks and advice as glue** (12 min). Ten lines that connect three packages: `after-save-hook` runs `typst compile`; `compilation-finish-functions` posts a message and reopens the preview; `display-buffer-alist` puts every pop-up where you want; `.dir-locals.el` sets a formatter per project. Exercise: write the ten lines in `user-lisp/glue.el` and watch them act (checked by events).
- **11.9 Capstone: a pipeline with no new package** (15 min). ripgrep → export → wgrep rename → compile → next-error → Magit stage by hunk → commit, with a layout recipe from 3.8 and a macro from 1.6. Exercise: the whole pipeline on the polyglot repo; checks on files, build, commit and layout.

### Part 12 — Remote editing with TRAMP (1 h; + remote; all `remote`)

- **12.1 TRAMP basics** (12 min). VS Code: Remote-SSH. Keys: `C-x C-f /ssh:box:/home/deploy/`, remote Dired, `C-x C-s`, connection reuse, `tramp-cleanup-all-connections`. Exercise: edit a config file on the box and verify it over `ssh` in eat.
- **12.2 Remote compile, Eshell and LSP** (15 min). VS Code: remote terminal and extensions. Keys: `compile` in a remote buffer, `eshell` with a remote default directory, Eglot over TRAMP with a remote `rust-analyzer`. Exercise: build the project on the box from a local Emacs and jump to a remote error.
- **12.3 Magit over TRAMP and sudo** (12 min). VS Code: remote source control. Keys: `C-x g` in a remote directory, `/sudo:box:/etc/...` multi-hop, `/ssh:box|sudo::/etc/nginx/nginx.conf`. Exercise: commit a change on the box; edit a root-owned file.
- **12.4 Containers** (10 min). VS Code: Dev Containers. Keys: `/docker:name:/`, `/podman:`, `/kubernetes:` mention. Exercise: edit a file inside a running container (the remote box's sidecar).

### Part 13 — Making it yours (2 h; all modules)

- **13.1 How init.el is organised** (12 min). VS Code: settings.json, settings UI. You can: read `init.el`, `modules.el` and one module; use `customize` and know when not to; find where a setting lives with `C-h v` and `C-h o`. Exercise: change three settings three ways (customize, `setq` in `user-lisp`, a module edit) and reload.
- **13.2 Packages: install, update, pin, remove** (15 min). VS Code: Extensions. Keys: `M-x list-packages`, `package-install`, `package-upgrade-all`, `package-vc-install` from a git URL, `use-package` with `:ensure :pin :bind :hook :custom :config`, `packages.lock`; elpaca and straight explained and compared. Exercise: install a package from MELPA and one from GitHub, configure both with use-package, remove one.
- **13.3 Keybindings** (15 min). VS Code: keybindings.json. Keys: `keymap-global-set`, `keymap-set` in a mode map, `define-prefix-command`, your own `C-c` prefix map, `C-h k` to verify, `which-key` group names, `repeat-map`. Exercise: build a personal prefix map with five commands and a repeat map.
- **13.4 Your first command, hooks and advice** (15 min). Keys: `defun` with `(interactive)`, `M-:` to test, a hook, `advice-add :around`, `C-h f` on your own function. Exercise: a command that inserts a dated header, bound to your prefix, with a hook that runs it in new Markdown files.
- **13.5 Themes, fonts, icons and the mode line** (12 min). VS Code: colour theme, file icon theme, font settings. Keys: `modus-themes-toggle`, `ef-themes`, `set-face-attribute`, fontsets for CJK, ligatures in the GUI frame, `text-scale`, nerd-icons in every list, `doom-modeline` versus the built-in mode line, `breadcrumb-mode`. Exercise: a light and dark pair, a bigger font for writing, icons on and off.
- **13.6 Take it home** (12 min). VS Code: Settings Sync. You can: clone the config from the playground to your machine, install the toolchains it expects, keep the ticker as an optional package. Exercise: `git clone` the config into a fresh container (the remote box) and start Emacs there without errors.

### Part 14 — Projects (10 h)

| | Project | Tasks (each with a checkpoint) | Languages and tools | Mirrors in VS Code |
|---|---|---|---|---|
| P1 | Hello, cargo | 1 scaffold with `cargo new` from Eshell; 2 layout: code above, compile below; 3 write with rust-analyzer; 4 fix a borrow error from the compile window; 5 add a test and run `cargo test`; 6 format on save; 7 commit with Magit by hunk | Rust, compile, Magit | rust-analyzer, Tasks, Source Control |
| P2 | A dune library | 1 `dune init`; 2 utop beside the source; 3 implement to satisfy expect tests; 4 merlin types and destructuring; 5 ocamlformat; 6 document with odoc; 7 commit | OCaml, dune, utop, merlin | OCaml Platform |
| P3 | Data script | 1 `uv init` and venv via pet; 2 write with basedpyright; 3 REPL-driven exploration; 4 ruff on save; 5 pytest in compile; 6 a dape breakpoint; 7 commit | Python, pet, ruff, dape | Python extension, Run and Debug |
| P4 | Paper with citations | 1 Typst skeleton with preview; 2 figures from `image-dired`; 3 citations with citar; 4 LaTeX appendix with AUCTeX; 5 pdf-tools and SyncTeX in the GUI frame (`gui`); 6 export to a tagged release | Typst, LaTeX, citar, pdf-tools | Tinymist, LaTeX Workshop |
| P5 | Docs site | 1 Markdown pages with preview; 2 static build in compile; 3 rename a term across pages with wgrep; 4 reorganise files in wdired; 5 spell check with jinx; 6 Magit commit per page; 7 deploy to the box with TRAMP | Markdown, wgrep, wdired, jinx, Magit, TRAMP | Markdown preview, multi-file replace |
| P6 | Polyglot service | 1 multi-root project and a tab per language; 2 fix the Rust build; 3 score with the Python tool from Eshell; 4 OCaml proof with dune; 5 Typst report with live preview; 6 one compile command per root; 7 deploy a release build to the box over TRAMP; 8 layout recipe saved with burly | Rust, Python, OCaml, Typst, TRAMP | multi-root workspace, Remote-SSH |
| P7 | Literate experiment notebook | 1 Org skeleton; 2 shell block runs the Rust benchmark; 3 Python block scores it; 4 OCaml block checks an invariant; 5 results into a table with formulas; 6 export to PDF and HTML; 7 run one block on the box | Org Babel, Rust, Python, OCaml | Jupyter, loosely |
| P8 | Repository-wide refactor | 1 find every use with consult-ripgrep; 2 export and wgrep rename; 3 rename files in wdired; 4 a macro over forty call sites; 5 compile and walk errors; 6 review with ediff against the checkpoint; 7 commit in Magit by hunk | wgrep, wdired, macros, ediff, Magit | multi-file replace, refactor preview |

Each project ends with a short "again, without hints" lap: the same tasks with hints disabled and a timer, to confirm the skills stuck.

## B. Authoring format

### B.1 Layout

```
curriculum/
  index.yaml                        parts, order, titles, outcomes, modules per part
  00-orientation/
    01-reading-the-screen/
      lesson.md                     prose (en); lesson.zh-CN.md for other locales
      scenario.yaml                 the runnable part
      fixtures/                     files copied into the workspace
      assets/                       images used by lesson.md
  …
  projects/p6-polyglot/
    project.md  scenario.yaml  fixtures/  remote.yaml
  tables/
    exits.yaml  habits.yaml  gloss.yaml  cheats.yaml  glossary.yaml
```

`playctl curriculum build` turns this into `curriculum.json` (index plus every scenario), per-lesson bundles and zstd fixture tarballs (deployment.md §1).

### B.2 lesson.md

CommonMark plus GFM tables, plus directives (`remark-directive`):

| Syntax | Renders as |
|---|---|
| `` `C-x C-f` `` | a keycap; any backticked text that parses as a key sequence |
| `:kbd[C-x C-f]` | a keycap, explicit |
| `:try[C-x C-f]` | a keycap with a "try it" button that sends the keys to Emacs |
| `:jargon[buffer]` | the word with a tooltip from `tables/glossary.yaml` |
| `:::vscode … :::` | the "Coming from VS Code" box |
| `:::aside{title="…"} … :::` | a side note |
| `:::gui-only … :::` | shown only when the GUI frame is active |
| `::steps` | where the step list renders (default: end of the lesson) |

Front matter: `title`, `summary`, `minutes`. Images are relative paths into `assets/`.

### B.3 scenario.yaml

```yaml
id: 02-buffers-and-files/03-opening-files
title: Opening files
minutes: 12
mode: terminal                 # terminal | gui | any
tags: []                       # remote, gui, project
modules: [base, agent, editing, files]
vscode: "Ctrl+P, Ctrl+O"
preview:                       # optional
  tabs: [docs/report.pdf, README.md]
  follow: { docs/report.typ: docs/report.pdf }
remote: { box: true }          # optional; provisions and expects the SSH box
setup:
  workspace: fixtures/hello-rust       # relative to the lesson directory
  root: hello                          # extracted to /work/hello
  reset: false                         # true restores root to the fixtures (overwrites edits, removes extra files, forgets saved cursor positions) and turns base's auto-save-visited-mode back on before eval
  open: [hello/README.md]
  layout: single                       # single | "(vsplit FILE BUFFER)" | "(hsplit …)" | a window-state file
  eval: "(goto-char (point-min))"
  compile: null                        # a command to run once so *compilation* exists
steps:
  - id: open
    goal: Open src/main.rs
    detail: "Hold Control, press x, then Control f. Type `ma`, press `TAB`, then `RET`."
    demo: 'C-x C-f "ma" TAB ~400 RET'  # key names; quoted text is typed; ~N pauses N ms
    conditions:
      - text: the Find file prompt was used
        event: { command: find-file, min: 1 }
        hint: "C-x then C-f. The prompt is the bottom line."
      - text: main.rs is the current buffer
        elisp: '(equal (buffer-file-name) "/work/hello/src/main.rs")'
        hint: "Type ma, press TAB, then RET."
    stuck:
      - when: { minibuffer_seconds: 30 }
        say: "Still in the prompt? RET accepts, C-g cancels."
      - when: { step_seconds: 300 }
        say: "Reset this step from the right-hand panel if the files look wrong."
    checkpoint: true
  - id: save
    goal: Save it
    conditions:
      - text: saved
        all:
          - { event: { command: save-buffer } }
          - { file: { path: hello/src/main.rs, not_contains: "Hello, world" } }
        hint: "C-x C-s. The mode line shows ** until you do."
  - id: layout
    goal: Code above, compile below
    conditions:
      - text: the layout matches
        layout: "(vsplit main.rs *compilation*)"
        hint: "C-x 2 splits; C-x o moves; C-x b *compilation* RET shows the compile buffer there."
```

Condition kinds and their fields:

| Kind | Fields | True when |
|---|---|---|
| `event` | `command`, `min` (default 1), `since` (`step` default or `lesson`) | the command ran at least `min` times |
| `elisp` | a form | the form returns non-nil inside Emacs (2 s timeout; errors count as unknown) |
| `file` | `path`, one of `exists`, `contains`, `not_contains`, `regex`, `json_path` + `equals` | the predicate holds on the workspace file |
| `shell` | `run`, `cwd` (default the root), `timeout` (default 5) | exit status 0 |
| `layout` | an s-expression of `vsplit`, `hsplit`, buffer names, `*` wildcard | the window tree matches (buffer names compared by `uniquify` base name) |
| `state` | any state field and a value, e.g. `{ state: { narrowed: true } }` | the latest state has that value |
| `all`, `any`, `not` | lists or a condition | combinators |

Hints belong to conditions; the hint shown is the first unmet condition's. `stuck` rules use `minibuffer_seconds`, `idle_seconds`, `step_seconds`, `quits_in_window` and `unbound_repeats`. `demo` strings must complete the step when replayed; `playctl curriculum play` enforces this.

### B.4 Tables

```yaml
# tables/exits.yaml — layer 1, evaluated in order against the state object
- id: minibuffer-prompt
  when: { minibuffer.kind: prompt }
  why: "A prompt is open: {minibuffer.prompt}"
  how: "RET accepts, C-g cancels. TAB completes names."
- id: isearch
  when: { isearch: true }
  why: "You are searching (I-search)."
  how: "RET stops at this match. C-g jumps back to where you started."
- id: other-window-special
  when: { otherWindows.mode in [help-mode, Buffer-menu-mode, compilation-mode, grep-mode, occur-mode, dired-mode, magit-status-mode, magit-diff-mode, eshell-mode] }
  why: "A second window shows {otherWindows.buf}."
  how: "Press q inside it, or C-x 1 here to keep only this window."
- id: narrowed
  when: { narrowed: true }
  why: "Narrowed: you see only part of this buffer (Narrow in the mode line)."
  how: "C-x n w shows everything again."

# tables/habits.yaml — layer 2
- key: C-z
  vscode: undo
  say: "C-z suspends Emacs to the shell. Undo is C-/ (also C-x u)."

# tables/gloss.yaml — ticker glosses
find-file: Open a file (asks for a path)

# tables/cheats.yaml — per major mode
rust-ts-mode:
  - [M-., go to definition]
  - [C-c C-c, compile]
```

`when` predicates support equality, `in`, `not`, `>`, and dotted paths into the state object; `otherWindows` is the list of non-selected windows. Tables are localised per file (`exits.zh-CN.yaml`).

### B.5 Validation and workflow

1. Write `lesson.md` and `scenario.yaml`; put fixtures in place.
2. `playctl curriculum validate`: schema checks, key names parse, fixture paths exist, every `demo` tokenises, every lesson referenced by `index.yaml` exists, tables reference known state fields.
3. `playctl curriculum play <lesson>`: in a headless session, loads the scenario, replays each step's `demo`, and asserts each step completes. Runs in CI for every lesson; a lesson whose demo cannot solve its own steps does not ship.
4. `playctl dev` with author mode on: the lesson screen shows condition results live, the raw state, and the exits rows that matched, so authors see what the learner will see.
5. Localisation: add `lesson.<locale>.md`; the scenario is shared, so step `text`, `goal`, `detail`, `hint` and `say` accept per-locale maps (`hint: { en: …, zh-CN: … }`).
