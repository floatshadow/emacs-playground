---
title: Shell output is a buffer
summary: Treat build and test output as text to work on, with occur, shell pipes, macros and saving, and turn a test run into a Markdown checklist of failures.
minutes: 10
---

In a terminal, the output of a command scrolls past and is gone unless you thought to pipe it somewhere first. In Emacs the output of `compile`, of Eshell and of `M-!` lands in a buffer, and a buffer is material: you can search it, filter it, copy it, rewrite it with a macro and save it as a file.

:::vscode
The integrated terminal's output can be selected and copied, and that is about all. Here the test run's output is a buffer like any file: `M-s o` lists its matching lines, `C-u M-|` pipes it through `grep` and `sort`, and `F3` records a macro over it.
:::

## Output knows its locations

compile recognises the locations that compilers and test runners print. In cargo's test output, each failure's `panicked at src/lib.rs:49:9` is a link: `M-g M-n` jumps to the next one from anywhere, and `RET` on the line jumps to that one (lesson 11.3). Eshell output gets the same treatment with `M-x compilation-shell-minor-mode`.

## Output is text

Everything from Parts 1 and 10 applies to output buffers:

- `M-s o` (occur) lists the lines that match a regexp, here every test that ended in `FAILED`.
- `C-x h` `M-w` copies all of it; `C-y` puts it in a file of your own.
- :try[C-u M-|] pipes the region through any shell command and replaces it with the result (lesson 10.1). `grep 'FAILED$' | sort` keeps the failures, in order.
- A keyboard macro turns one line into the shape you want; `F4` repeats it on the next.
- `C-x C-s` saves the result. `C-x C-w` writes any buffer to a file you name, so a whole Eshell session or build log can be kept as it is.

The `checks` crate has six tests and three of them fail. The goal is a file `FAILING.md` that lists them as a Markdown checklist, ready for an issue tracker.

::steps
