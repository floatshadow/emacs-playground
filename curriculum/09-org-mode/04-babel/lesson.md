---
title: "Babel: run code, pass results between languages"
summary: Run shell, Python and Rust code blocks inside an Org file, pass one block's result to the next with :var, edit a block in its language's own mode, and tangle a block to a script.
minutes: 15
---

:jargon[Babel] makes an Org file a notebook. A :jargon[code block] runs where it is, and its result is written into the file under it. Give a block a name and another block, in another language, can read that result as a variable. Prose, code and results stay in one plain-text file you can diff and commit.

:::vscode
A Jupyter notebook where every cell can be a different language and the outputs are text in the file. `C-c C-c` is Run Cell; `C-c '` is opening the cell in a full editor. There is no kernel to restart: each run starts fresh unless you ask for a `:session`.
:::

## Blocks and results

```
#+name: samples
#+begin_src sh :results table
awk '{ print $2, $3 }' latency.log
#+end_src
```

:try[C-c C-c] anywhere in the block runs it, in the directory of the Org file, and writes `#+RESULTS: samples` with the output under it. Running it again replaces the result. Header arguments after the language change what happens:

| Header | Does |
|---|---|
| `:results table` / `output` / `value` | a table; everything printed; the value of the last expression |
| `:var rows=samples` | the result of the block named samples, as a variable `rows` |
| `:dir /some/where` | run in that directory |
| `:tangle file.sh` | where `C-c C-v t` writes the block |
| `:exports both` | show code and result when exporting (9.6) |

In the playground, shell, Python, OCaml, Rust and Emacs Lisp blocks run without asking first. Rust blocks are compiled with rustc: a block without `fn main` is wrapped in one, and each `:var` becomes a constant (a table becomes a slice of slices).

## Writing blocks

`C-c C-,` offers templates: `s` for a source block, `q` for a quote, `e` for an example. :kbd[C-c '] in a block opens its code in a buffer of its own, in the language's mode (rust-ts-mode, python-ts-mode, tuareg) with its indentation, colours and completion; `C-c '` again puts it back, `C-c C-k` throws the edits away. The edit buffer is not a file, so Eglot does not start in it.

:::aside{title="Tangling"}
`C-c C-v t` (`org-babel-tangle`) writes every block that has a `:tangle` header to its file, so a document can be the source of real scripts. 9.8 builds a whole configuration file this way.
:::

`pipeline.org` measures the blog's pages, scores them and needs a verdict. The shell block reads `latency.log`, next to it.

::steps
