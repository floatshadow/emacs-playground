---
title: OCaml
summary: Build with dune from tuareg buffers, ask merlin for types, explore in utop beside the code, and make an expect test pass.
minutes: 15
---

OCaml in Emacs is tuareg, the major mode, with ocaml-lsp-server behind Eglot. ocaml-lsp runs merlin, the engine that knows the type of every expression, and the ocaml-eglot package adds merlin's own commands on top. utop, the friendly toplevel, runs in a buffer beside your code, and dune builds and tests through `compile`.

:::vscode
The OCaml Platform extension uses the same ocaml-lsp-server. Hovering for a type is `C-c C-t`. Sending a selection to the terminal running utop is `C-c C-e`, the phrase under the cursor. dune build and dune test become `C-c C-c` and `C-c C-r` in the source buffer.
:::

## Types on demand

:try[C-c C-t] shows the type of the expression at point in the echo area: on `split_on_char` it says `char -> string -> string list`. Right after it, `C-<up>` widens to the enclosing expression and `C-<down>` narrows back, so you can read how a pipeline's types fit together; `C-c C-t` again expands type abbreviations, and any other key ends it. merlin needs dune's view of the project: when a fresh project shows *No config found*, `C-c C-c` (dune build) once fixes it.

## utop beside the source

:try[C-c C-s] starts utop below the source and moves the cursor into it. In a dune project it runs `dune utop`, so the project's libraries and their dependencies are loaded. `C-c C-z` switches between utop and the source buffer, in both directions. From the source :try[C-c C-e] sends the phrase at point (a whole `let`) and moves to the next one, and `C-c C-b` sends the whole buffer. In utop, a phrase ends with `;;` and `RET` evaluates it; `M-p` brings back earlier input, and `TAB` completes names.

## dune and expect tests

`C-c C-c` runs `dune build`, `C-c C-r` runs `dune runtest`, and dune's errors (`File "lib/wordfreq.ml", line 7, characters 16-20`) are links for `M-g M-n`. An :jargon[expect test] prints something and states what it should print in `[%expect {| ... |}]`; when the output differs, `dune runtest` shows the difference as a diff, and `C-c C-p` (dune promote) copies the actual output into the file when it is the right one. ocamlformat formats on save, following the project's `.ocamlformat`.

The library `/work/lsp/wordfreq` has a function `count_words` still to write, and an expect test that says what it should return.
