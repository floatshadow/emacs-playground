---
title: Switching, saving and killing buffers
summary: Move between open files, save one or all, save under a new name, kill buffers, revert one, and read uniquified names.
minutes: 12
---

With several files open, the everyday moves are switching to the one you want, saving, and getting rid of what you no longer need. Each has a key, and a few details, such as two files with the same name, look odd until you know what Emacs is doing.

This lesson works with two small projects in `/work/projects`: *hello* in `/work/projects/hello/` and *polyglot* in `/work/projects/polyglot/`. (`/work/hello` is the older copy of hello from lesson 2.3; nothing here uses it.)

:::vscode
Ctrl+Tab is `C-x b RET`, which flips to the previous buffer, or `C-x <left>` and `C-x <right>`, which walk this window's history. Ctrl+S is `C-x C-s`, Save All is `C-x s`, Save As is `C-x C-w` and Ctrl+W is `C-x k`. Where VS Code shows *main.rs hello/src* in a tab, Emacs names the buffer `main.rs<hello>`.
:::

## Switching

:try[C-x b] switches by name; `RET` with nothing typed returns to the buffer you were just in, so pressing `C-x b RET` repeatedly flips between two files. :try[C-x <left>] goes back through the buffers this window has shown, and `C-x <right>` forward, like a browser's back button for the window.

When two open files have the same name, Emacs adds just enough of the directory to tell them apart, in angle brackets: `main.rs<hello>` and `main.rs<polyglot>`. This is called :jargon[uniquify]. When one of them is killed, the other gets its plain name back.

## Saving

:try[C-x C-s] saves the current buffer. :try[C-x s] offers to save every modified buffer, one at a time: `y` saves, `n` skips, `!` saves all the rest without asking. :try[C-x C-w] writes the buffer to a new file, and the buffer follows it: from then on it visits the new file, and the old file stays on disk as it was.

## Killing and reverting

`C-x k` kills a buffer; it asks before throwing away unsaved changes. :try[C-x x g] reverts: it reloads the buffer from its file, discarding unsaved changes after asking. You rarely need it for changes made outside Emacs, because the playground reloads buffers automatically when their files change on disk (*auto-revert*), for example after a checkpoint restore or a `git checkout`.

:::aside{title="Saving in the playground"}
Files normally save themselves a few seconds after you stop typing. This lesson switches that off so you can see the `**` flags and save them yourself.
:::

::steps
