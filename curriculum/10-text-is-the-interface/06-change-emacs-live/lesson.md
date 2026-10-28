---
title: Change Emacs while it runs
summary: Evaluate Lisp anywhere, find the source of a command, redefine it while Emacs runs and undo that by loading the file again; build regexps live and look at timers and processes.
minutes: 12
---

Emacs is a Lisp program that is running right now, and every part of it can be inspected and replaced while it runs: no restart, no build. A command is a function; evaluate a new definition and the next key that runs the command runs the new code. Loading the file the function came from brings the old one back.

:::vscode
Changing VS Code's behaviour means writing an extension, packaging it and reloading the window. In Emacs the configuration and the editor are the same language and the same running program. `M-:` is a console attached to the editor itself, and any function's source is one link away from its help page.
:::

## Evaluating Lisp

:try[M-:] reads one expression in the minibuffer, evaluates it and shows the value in the echo area: `(+ 1 2)`, `(buffer-name)`, `(count-lines (point-min) (point-max))`. In a Lisp file, `C-x C-e` evaluates the expression that ends just before the cursor, and `C-M-x` evaluates the whole top-level form around it, typically a `defun`. `M-x eval-buffer` evaluates the whole buffer and records which file each definition came from; a definition made with `C-x C-e` or `C-M-x` is not tied to a file, so its help page cannot link to the source.

`M-x ielm` opens a Lisp prompt, a :jargon[REPL] for Emacs itself: type an expression, `RET`, and the value is printed below. `M-p` brings back earlier input.

## From help to source

:try[C-h f] describes a function. The help page names the file it was defined in, as a link: `RET` on it, or `s` anywhere in the help window, opens the source at the definition. The same works for built-in commands, whose Lisp source ships with Emacs. (Part 13 replaces this help with helpful, which shows the source inline.)

## Redefining, and going back

Edit a `defun` in its source buffer and press `C-M-x`: the new definition replaces the old one in the running Emacs, without saving the file. The next call uses it. `M-x load-file` reads the file from disk and evaluates all of it, which brings back every definition as it is saved.

This lesson turns off saving on its own (the base module saves edited files after a few seconds), so the file on disk keeps the original until you save it yourself with `C-x C-s`.

## Looking around

- `M-x re-builder` builds a regexp interactively: type it in the small window at the bottom and every match in the buffer above lights up as you type. `C-c C-q` quits.
- `M-x list-timers` lists the timers that will run Lisp later or repeatedly, with the function each one calls.
- `M-x proced` lists the processes on the machine, like `top`; `k` sends a signal to the one on the line.

The exercise is a tiny command, `stamp-line`, that marks a line as reviewed. You will load it, call it from ielm, find its source through help, change what it does, and undo the change by loading the file again.

::steps
