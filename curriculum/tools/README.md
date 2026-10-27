# Curriculum tools

Authoring tools for `curriculum/` (CURRICULUM.md Part B.5). They need only
Python 3 with PyYAML, and Docker for the replay harness. They stand in for
`playctl curriculum validate` and `playctl curriculum play` until those
exist, and stay useful as a cross-check afterwards.

| Tool | What it does |
|---|---|
| `validate.py` | Schema and content checks of Part B.2 to B.4: index, every scenario (all `protocol.Scenario` fields and nothing else), lesson.md directives and front matter, the five tables, fixture drift. Content rules from the newcomer review: no level-one hint that only repeats the step's keys; exit-row regexes that JavaScript reads like RE2 (compiled with node's `RegExp` when node is installed); every prompt in `EXIT_PROMPT_SAMPLES` matches exactly its own exit row. Exit status 1 on errors. `test_validate.py` tests these rules. |
| `pgcurriculum.py` | Python ports of `internal/protocol` (keys, demo strings, layout patterns, predicates, state paths), shared by the other tools. The Go code is the reference. |
| `sync_fixtures.py` | Copies the shared fixture trees in `fixture-src/` into each lesson that uses them (`--check` reports drift). |
| `play/play.py` | Replays every step's demo in a real Emacs 31.1 and checks the step's conditions after every command. |
| `play/probe.sh` | Evaluates one Lisp form in the same Emacs and configuration, for checking facts while writing demos. |

```sh
python3 curriculum/tools/validate.py                 # all lessons and tables
python3 -m unittest discover -s curriculum/tools -p 'test_*.py'   # the validator's own tests
python3 curriculum/tools/sync_fixtures.py            # after editing fixture-src/
docker build -t pg-curriculum-play curriculum/tools/play
python3 curriculum/tools/play/play.py --all          # or one or more lesson ids; -v, --keep
curriculum/tools/play/probe.sh form.el
```

Scratch output goes to `artifacts/play/` and `artifacts/probe/` (git-ignored).

## Fixtures

Each lesson ships its own `fixtures/` directory (fixture tarballs are built
per lesson), but lessons in a part share one small project:

| Tree in `fixture-src/` | Used by | Extracted to |
|---|---|---|
| `hello` | Part 0, lesson 2.3 | `/work/hello` |
| `editing` | Part 1 | `/work/editing` |
| `projects` (`hello`, `polyglot`, `notes`, `inbox`) | Part 2 except 2.3, Part 3 | `/work/projects` |

Edit the tree in `fixture-src/`, then run `sync_fixtures.py`.
`validate.py` fails when a lesson's copy has drifted.

## The replay harness

`play/Dockerfile` is a small image (`silex/emacs:31.1-ci` plus the
third-party packages Parts 0 to 3 use and the Rust tree-sitter grammar). It
is not the session image. `play.py` copies the lesson's fixtures into a
scratch `/work`, starts `emacs -nw` under a pseudo-terminal, loads
`assumed-config.el` (the modules) and `driver.el`, applies the scenario's
setup (open, layout, compile, eval), then feeds each step's demo to the
command loop through `unread-command-events`, as if typed. After every
command it records the command names, a state object shaped like the
agent's (api.md §5), the results of the step's elisp conditions and the
files its file conditions read. The conditions are evaluated with the
step engine's semantics:

- a step completes at the first command after which every condition holds;
- FAIL when that never happens, or when the step is complete before its
  demo starts;
- WARN when it completes before the demo's last command (the remaining
  keys run in the next step; harmless on a lesson's last step).

Shell conditions are not evaluated (none are used in Parts 0 to 3).

## What the lessons assume

`assumed-config.el` mirrors the behaviour-relevant settings of
`emacs.d/modules/{base,editing,files,windows}.el` as the Emacs agent wrote
them. If a module changes one of these, the lessons named here need a
look, and the replay tells you which steps break.

Modules:

- **base**: `help-window-select t`, so help takes the cursor and `q` closes
  it (0.2, 0.3); `use-short-answers t`, so confirmations take `y` (2.4,
  2.6, 2.7); `auto-save-visited-mode` on, which 0.1, 2.3, 2.4 and 2.5 turn
  off in `setup.eval` because they teach the modified flag (the agent turns
  it back on at the start of every setup with `reset`, so it never stays
  off after those lessons);
  `delete-selection-mode`; `repeat-mode` (3.2 `C-x o o o`, 3.8 `C-x } }`);
  savehist, recentf and save-place (2.2, 2.8); uniquify
  `post-forward-angle-brackets` (2.4 expects `main.rs<hello>`); `C-z`
  unbound.
- **editing**: `M-y` consult-yank-pop, `C-?` and `C-M-_` undo-redo (1.3),
  `C-x u` vundo, `M-%` visual-replace with query on (1.5), `C-;` iedit,
  `C-c m l` mc/edit-lines, `C-=` expreg-expand (1.7), `M-j`
  avy-goto-char-timer with the default home-row labels (1.8 presses `d`),
  `M-g i` consult-imenu whose items read *Fn greet_all* (1.8).
- **files**: `C-x C-b` ibuffer grouped by project with `ibuffer-expert`
  (2.5: `D` does not confirm), `C-x C-r` recentf-open (2.8), dirvish over
  Dired with `q` running dirvish-quit (2.3, 2.6), `Cargo.toml` in
  `project-vc-extra-root-markers` (2.7, Part 3 `C-x p c`), `f` in
  `project-switch-commands` (2.7, 3.7).
- **windows**: windmove on Shift (3.2), ace-window labels `a s d f` (3.2
  presses `f`), the bottom-third rule for compilation buffers (3.5, 3.8),
  winner on `C-c <left>` (3.6), burly (3.6), `C-x p p` running
  `playground-project-tab` (3.7 expects a tab named after the project),
  olivetti (3.8).
- **Emacs 31**: the `C-x w` layout commands (3.3).

Agent and step engine:

- Elisp conditions never rely on the current buffer: they name the buffer
  (`find-buffer-visiting`, `(window-buffer (selected-window))`), so it does
  not matter where the agent evaluates them.
- Event conditions list alternatives where Emacs reports different names:
  `kill-line` sets `this-command` to `kill-region`, `yank-pop` to `yank`,
  `undo-redo` to `undo`; the harness counts both `real-this-command` and
  `this-command`. Prefix commands (`C-u`, digits) restore `this-command`,
  so no condition waits for `universal-argument`, and no condition waits
  for `execute-extended-command`: after `M-x`, the event is the command
  that ran.
- With `delete-selection-mode`, `C-g` in the minibuffer runs
  `minibuffer-keyboard-quit`; 0.2 accepts it, and stuck rules counting
  quits (`quits_in_window`) should count it too.
- `playground-rescue` is interactive: 3.6's demo runs it with `M-x` where
  the learner presses the Rescue button.
- A demo may run twice from the same checkpoint: Watch it restores the
  step first when the learner has run commands, Do it yourself restores
  it again, and `playctl curriculum play -watch` does both. A restore puts
  back files (files created since the checkpoint are removed) and windows,
  but not a buffer's minor modes. 0.4's last step accepts either toggle
  direction of `display-line-numbers-mode`; 3.8's follow and writing steps
  rely on their setup turning follow-mode and olivetti-mode off, so a
  second pass after a restore toggles them off again.
- Demos that open files by path type the absolute path after the offered
  directory (2.4, 3.4), so they work from any buffer; 2.3, 2.6 and 2.7
  keep relative names because the step teaches them from its starting
  buffer.
- `setup.layout` patterns may nest (3.2 builds a 2×2 grid); names with a
  `/` are workspace-relative files. Demos that depend on which window is
  selected after setup select it in `setup.eval` (3.2, 3.3).
- Lessons that type buffer names first close unmodified file buffers from
  earlier lessons outside their own tree (`setup.eval` in Parts 2 and 3),
  so `main.rs` means this lesson's file.
- 3.1 opens and deletes a second terminal frame with `C-x 5 2` and
  `C-x 5 0`; deleting it must not count as the terminal frame closing.
- 3.5 reads `windows[].side` and 3.7 reads `tabs.current` (the tab's
  explicit name after `C-x t r` or `playground-project-tab`).

Browser (frontend.md §6): `tables/exits.yaml` uses a virtual `session` root
next to the state fields (`session.prefix`, `session.emacs`,
`session.mode`, `session.echo`); its header defines them.

Demos send `C-=`, `S-<left>`, `S-<up>`, `C-c <left>`, `C-x <left>`,
`C-M-_`, `<f3>` and `<f4>`. In the real session they go through tmux, which
must pass them on (extended keys, kkp in base).
