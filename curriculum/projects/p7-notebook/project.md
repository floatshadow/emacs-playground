---
title: Literate experiment notebook
summary: One Org file runs a Rust benchmark, scores it in Python, checks it in OCaml, tabulates it with formulas, exports to HTML and PDF and runs a block on the box.
minutes: 75
---

How many comparisons does a sort make? `/work/notebook/bench` is a Rust program that sorts lists of 1000, 10000 and 100000 numbers and counts them. You will build the experiment's notebook in `notebook.org`: prose, code in four languages, results and a table, in one plain-text file that also exports to a report.

:::vscode
Like a Jupyter notebook, cells run and their output lands under them, but here a cell is a :jargon[code block] in an ordinary text file, each block can be a different language, results pass from block to block by name, and the file diffs cleanly in git. There is no kernel to restart: each block runs on its own (or in a `:session`).
:::

## Blocks and results

A block is `#+begin_src LANGUAGE HEADERS` … `#+end_src`. :try[C-c C-c] with the cursor inside runs it and writes `#+RESULTS:` below, replacing the previous result. Headers change how: `:dir bench` runs in that directory, `:results table` turns output into an Org table, `:results output` keeps it verbatim. `#+name: bench` above a block names its results, and `:var rows=bench` hands them to another block, as a list in Python and an array in OCaml. :try[C-c '] edits a block in its language's own mode, with its language server; `C-c '` again comes back. Typing a block line by line, end lines with `C-j`: `RET` while a completion popup is open inserts the candidate instead.

## Tables that compute

In an Org table `TAB` moves to the next cell and realigns, `C-c RET` adds a rule. A cell starting with `=` becomes a column formula when you leave it: Org writes it as `#+TBLFM:` under the table, in Calc syntax (`$2` is column 2, `log(x, 2)` a base-2 logarithm, `;%.3f` the format). :try[C-u C-c *] recomputes the whole table.

## Out of Emacs

:try[C-c C-e] opens the export menu: `h h` for HTML, `l p` for PDF through LaTeX. `#+property: header-args :eval never-export` at the top exports the results you have rather than running every block again. Last, `:dir /ssh:box:` runs a block on the box over TRAMP, and its output comes home into the notebook like any other result.

::steps
