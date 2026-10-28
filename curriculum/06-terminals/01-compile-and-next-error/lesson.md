---
title: compile and next-error
summary: Run builds and tests in a compilation buffer, jump from each error to its line with M-g M-n, and rebuild with g.
minutes: 12
---

Emacs runs builds itself. The output lands in a :jargon[compilation buffer], and every line that names a file and a line number becomes a link. One key walks you through those links in order, opening each file at the right spot, wherever you are: that is :jargon[next-error].

:::vscode
Run Build Task (Ctrl+Shift+B) is `C-c C-c` in a Rust buffer; Tasks: Run Task with your own command is `M-x compile`. The Problems panel is the *compilation* window itself, and F8 (next problem) is `M-g M-n`. Unlike the panel, the list is the tool's own output, so it works the same for cargo, dune, pytest, make or a shell script.
:::

## Building

In a Rust buffer, :try[C-c C-c] runs `cargo build` in the crate's root without asking. `M-x compile` asks for any command first, offering the last one; `C-a C-k` clears the offer. The output opens at the bottom of the frame, a third of it high (the rule you read in lesson 3.5), and scrolls to the first error. The mode line of *compilation* shows `exit [101]` when the build failed and `exit [0]` when it worked.

## Walking the errors

:try[M-g M-n] goes to the next error: it opens the file, puts the cursor on the line and column the compiler named, and highlights the error in the build output. :try[M-g M-p] goes back to the previous one. ``C-x ` `` is the same as `M-g M-n`, an older binding many people still use. In the *compilation* window itself, `RET` on an error visits it, `n` and `p` move between errors without leaving, and `g` runs the same command again.

`compile` saves your modified files before it starts, so you never build a stale file.

## Not only cargo

Emacs recognises the error formats of many tools out of the box: rustc and cargo (`--> src/count.rs:9:13`), OCaml and dune (`File "bin/main.ml", line 3, characters 4-9`), gcc, Python tracebacks. The playground adds pytest's `test_scores.py:9: AssertionError`. So `M-g M-n` works the same after `uv run pytest`, which this lesson ends with.

:::aside{title="When a format is not recognised"}
The patterns live in `compilation-error-regexp-alist`. Adding one for an in-house tool is a few lines of Lisp; `/work/emacs.d/modules/terminals.el` adds pytest's.
:::

The crate `/work/terminals/wordcount` counts words in a poem, and has two errors to fix.
