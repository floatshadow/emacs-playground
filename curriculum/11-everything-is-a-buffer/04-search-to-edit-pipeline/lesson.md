---
title: The search-to-edit pipeline
summary: Rename a concept across a project by chaining ripgrep, Embark, wgrep, diff-hl and Magit, five packages with no integration code between them.
minutes: 15
---

The crate `tally` calls its ranges *buckets*; the team has decided they are *bins*. That is a change across code, tests and docs. Emacs has no "rename across project" feature for it, and it does not need one: five packages, each doing one job on a buffer, chain into one.

:::vscode
This is Search: Replace in Files (Ctrl+Shift+H), then the Source Control view. The difference is what sits in the middle: the results become an ordinary buffer, so you edit them with replace, macros or multiple cursors, review them in the margin, and commit the hunks you want.
:::

## The pipeline

1. **Search:** `M-s r` (consult-ripgrep) lists every match in the project as you type.
2. **Export:** :try[C-.] `E` (embark-export) turns the matches into a grep buffer.
3. **Edit:** `C-x C-q` (wgrep) makes the matched lines editable; any editing command works. `C-c C-c` writes each changed line back into its file and saves it.
4. **Review:** diff-hl marks changed lines in the margin of every file in a git repository; `C-x v n` and `C-x v p` jump between changed hunks (lesson 5.5). Eglot and Flymake re-check the saved files, so a half-done rename shows up as an error.
5. **Commit:** `C-x g` opens Magit, where the changes wait to be staged and committed (lesson 5.1).

No package in this list calls another. Embark knows that consult's candidates are grep lines and makes a grep buffer; wgrep knows grep buffers; diff-hl and Flymake watch files being saved; Magit reads the repository. Each consumes what the previous one produced.

## Replace keeps the case

Replace commands match case-insensitively when the search text is all lower case, and then copy the case of each match into the replacement: `bucket` → `bin` turns `Bucket` into `Bin` and `BUCKET` into `BIN`, while `make_buckets` becomes `make_bins`. This is the variable `case-replace`, on by default. Ripgrep's `--smart-case` follows the same rule for the search.

::steps
