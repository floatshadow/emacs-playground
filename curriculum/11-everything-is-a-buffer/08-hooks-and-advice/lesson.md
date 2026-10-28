---
title: Hooks and advice as glue
summary: Connect saving, compiling and window placement with ten lines of Emacs Lisp in user-lisp/glue.el, and give one folder its own formatter with .dir-locals.el.
minutes: 12
---

Packages never call each other by name; they announce moments. Saving a file runs `after-save-hook`, a finished build runs `compilation-finish-functions`, every buffer that needs a window goes through `display-buffer-alist`. Each of these is a list you can add to. Ten lines that add to three of them turn "save a Typst file" into "build the PDF quietly and tell me how it went", and no package involved knows about the others.

:::vscode
This is what extensions do with activation events and `onDidSaveTextDocument`, or what `tasks.json` with `runOn` and `.vscode/settings.json` per folder do. In Emacs the hooks are open to you directly: a function and an `add-hook` line in your configuration, evaluated while Emacs runs, with no reload.
:::

## Hooks

A :jargon[hook] is a variable holding a list of functions. `add-hook` puts a function on it, `remove-hook` takes it off, and `C-h v after-save-hook` shows what is there now. Most hooks call their functions with no arguments, in the buffer concerned, so the function asks for itself: "am I in a Typst buffer?" (`derived-mode-p`). Some pass arguments: `compilation-finish-functions` gets the build's buffer and a status string such as `"finished\n"`.

Mode hooks are the other common kind: `rust-ts-mode-hook` runs whenever a buffer enters rust-ts-mode, which is how the playground's modules start Eglot. **Advice** is the last resort: `advice-add` wraps an existing function with your own, before, after or around it, when no hook exists for the moment you need (lesson 13.4).

:::aside{title="Typing Lisp with the completion popup"}
The popup from lesson 4.4 opens while you type a symbol, and `RET` then picks its candidate instead of starting a new line. `C-g` closes the popup first; a space or a parenthesis closes it too.
:::

## Where glue lives

:jargon[user-lisp] is the folder `emacs.d/user-lisp/`. Emacs 31 byte-compiles the files there at startup and collects their `;;;###autoload` lines, but it runs nothing else from them on its own. That suits a lesson: what you evaluate with :try[M-x] `eval-buffer` works until Emacs restarts. To keep a form for good, put `;;;###autoload` on the line before it, or move it into `custom.el` (lesson 13.1).

## Per-folder settings

`.dir-locals.el` in a folder sets variables for the files below it: an indentation width for one project, a compile command, a formatter. `M-x add-dir-local-variable` writes the file for you. Emacs asks before applying values it does not know to be safe; the formatter used here is marked safe by its package, so it applies at once.

The setup opened `user-lisp/glue.el`, which lists the three pieces of glue to write.

::steps
