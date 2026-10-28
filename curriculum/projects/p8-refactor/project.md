---
title: Repository-wide refactor
summary: Rename a concept across Rust and Python with ripgrep and wgrep, rename files in wdired, rewrite forty call sites with a macro, walk the build errors, review in ediff and commit hunk by hunk.
minutes: 75
---

`/work/shelf` is a small Rust program with a Python report beside it, and its code says `usr` where it means `user`: a function, two files, imports. You will rename the concept everywhere, rewrite forty call sites of an old helper, let the build find what the renames broke, review the result against where you started and commit it as two clean commits. No refactoring engine: the same editing tools on more files.

:::vscode
Search across files plus Replace All, with its preview, is ripgrep exported into a buffer you edit like any other (wgrep), then `C-c C-c`; the preview is the buffer itself. F2 on a file in the Explorer is editing the Dired listing. Where VS Code would need multi-cursor across forty lines, a keyboard macro replays any edit. The diff editor against HEAD is `vc-ediff`, and staging selected ranges is staging hunks in Magit.
:::

## Find, then change everywhere

:try[M-s r] searches the project as you type; a `#` after the search text starts a second, local filter over the results. In the results `C-.` `E` exports them to a grep buffer; :try[C-x C-q] there makes the lines editable, and `C-c C-c` writes every changed line back to its file and saves it. The same `C-x C-q` in Dired renames files by editing their names.

## Repeat an edit

`F3` starts recording a keyboard macro, `F4` stops, and `F4` again replays it. Record one line in a way that works on every line (move by words and characters, `M-z` to zap up to a character, end with `C-n`), then `C-u 39 F4` replays it 39 times.

## Let the build find the rest

`C-x p c` runs `make`, which builds the Rust program and runs the Python test. :try[M-g M-n] visits each error in turn, Rust's `--> src/main.rs:2:1` and Python's traceback lines alike.

## Review and commit

`M-x vc-ediff` compares the file with its committed version, HEAD: here, the project as it was at the first task. In the control window `n` and `p` move between differences, `a` copies A's version into B, `w b` saves B, `q` quits. In Magit `M-4` expands every hunk, and `s` with the cursor inside a hunk stages just that hunk.

::steps
