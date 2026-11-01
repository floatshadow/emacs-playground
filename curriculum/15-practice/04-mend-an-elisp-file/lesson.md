---
title: Mend an Elisp file
summary: Make a small Emacs Lisp file compile, fix a call with its arguments swapped, and turn a function into a command.
minutes: 15
---

Practice: the lesson says what to make, not which keys to press, and checks only the result. The tools: :try[C-M-x] evaluates the form around the cursor and `M-x eval-buffer` the file, `C-h f` describes a function, `M-x byte-compile-file` compiles and lists warnings, and `M-x ert` runs tests.

:::vscode
Like fixing a script the linter rejects and whose unit tests fail, with the linter (the byte compiler) and the test runner (ERT) built into the editor.
:::

## The brief

`my-tools.el` has three small text tools, and three mistakes. `my-tools-test.el` has their tests.

1. **Compile.** One form is not closed, so the file does not byte-compile. Make it compile without warnings.
2. **Run.** `my-tools-wrap-region` calls `my-tools-wrap` with its arguments in the wrong order, and it is not a command, so `M-x` does not offer it. Make it wrap the region to 40 columns, from code and with `M-x`.

The checks compile the saved file and run its tests in a separate Emacs. When it all works, a copy in `/work/emacs.d/user-lisp/` (lesson 13.4) would load it in every session.

::steps
