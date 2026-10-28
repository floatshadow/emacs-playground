---
title: A dune library
summary: An OCaml library from dune init to a documented, formatted, tested first commit, with utop and Merlin beside the code.
minutes: 75
---

You build `wordstat`, a small OCaml library that splits text into words. The loop is the one OCaml programmers use every day: try an idea in utop, pin the behaviour down with expect tests, let Merlin answer type questions and write boilerplate, let ocamlformat and odoc take care of style and documentation. The brief is `README.md` in the preview; from task 6 the preview shows the library's own documentation.

:::vscode
The OCaml Platform extension talks to the same ocaml-lsp-server, so hovers, diagnostics and the Destruct code action behave as you know them. What it lacks is the toplevel beside the code: utop runs in a buffer, and `C-c C-e` sends the definition at point to it, the way you would select code and run it in a terminal, without copying.
:::

## The keys of this project

| Keys | Does |
|---|---|
| `M-&` | run a shell command in the background |
| `C-c C-s` / `C-c C-e` / `C-c C-z` | start utop / send the phrase at point / go to utop |
| `C-c C-c` / `C-c C-r` / `C-c C-p` | dune build / dune runtest / dune promote |
| `C-x i` | insert a file at point |
| `C-c C-t` | type of the expression at point (again: the enclosing one) |
| `C-c \|` | destruct: a match with every case |
| `C-x p c` | any command in the project root, such as `dune fmt` or `dune build @doc` |
| `C-x g` | Magit status |

## Expect tests

dune itself starts from a shell command: :try[M-&] runs one in the background, in the current buffer's directory, and shows its output in a buffer of its own.

An expect test prints something and states what it should print, in a `[%expect {| … |}]` block. `dune runtest` shows a mismatch as a diff, and `dune promote` (`C-c C-p`) copies what the code printed into the test. So you can write a test with an empty expectation, look at the output, and accept it once it is right: the test then guards that output from then on.

:::aside{title="No config found"}
The language server learns about a dune project from a build. If Flymake reports *No config found* in a new file, build once (`C-c C-c`, or starting utop does it) and type anything: the message goes.
:::

Each task saves a checkpoint when it starts; the list on the left restores one. When every task is done, **Again, without hints** runs the whole project once more against the clock.

::steps
