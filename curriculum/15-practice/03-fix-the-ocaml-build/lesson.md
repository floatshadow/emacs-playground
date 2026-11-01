---
title: Fix the OCaml build
summary: Make a small dune library build, then make its expect test pass with the code formatted, with the tools of Part 7.
minutes: 15
---

Practice: the lesson says what to make, not which keys to press, and checks only the result. Part 7's tools are all here: :try[C-c C-c] builds, Flymake underlines, `C-c C-t` shows a type, the compile window links each error to its line, and saving formats the buffer.

:::vscode
The OCaml Platform's diagnostics and the dune tasks: the same compiler, the same ocamlformat, in buffers whose lines are links.
:::

## The brief

`stats` is a dune library of small statistics over lists of integers, with one test, `test/test_stats.ml`, whose output must match `test/test_stats.expected`.

1. **Build.** `dune build` stops at a type error in `lib/stats.ml`; there is a second one behind it. Fix both.
2. **Test.** `dune test` shows one line that differs from the expected output. Fix the library, not the expected file, and keep every file formatted the way `.ocamlformat` says (`dune fmt` checks).

The checks run dune on the saved files.

::steps
