---
title: Dired and the dirvish side window
summary: Manage files from a directory listing in Dired, make folders, move, rename and delete files, and keep a file tree open beside your code.
minutes: 15
---

:jargon[Dired], the directory editor, shows a folder as a buffer: one line per file. Keys act on the file under the cursor, or on every marked file. Because the listing is a buffer, everything you know still works in it: search, motion, even keyboard macros.

:::vscode
The Explorer is Dired. New Folder is `+`, Rename and Move are both `R`, Copy is `C`, Delete is `d` then `x`. Multi-select is marking with `m`. The sidebar itself is `C-x C-j`, a :jargon[dirvish] side window that stays open while you edit.
:::

## Opening a listing

:try[C-x d] asks for a directory and offers the one of the current file; `RET` accepts. Dired also opens when you ask `C-x C-f` for a directory, which is how most people first meet it by accident. In the listing, `RET` opens the file or folder on the line, `^` goes up to the parent folder, `j` jumps to a file by name, and `q` puts the listing away.

## Marks and flags

`m` marks the file on the line with `*`; `u` unmarks it and `U` unmarks everything. `d` flags a file for deletion with `D`, and nothing is deleted until `x` executes the flags, after asking. Commands such as `R` (move or rename), `C` (copy) and `!` (run a shell command on the files) act on the marked files, or on the file under the cursor when nothing is marked. When there are several, Emacs asks for a target directory; when there is one, a new name.

`+` makes a folder. Each operation refreshes the listing, so what you see is always what is on disk.

## The side window

:try[C-x C-j] opens a dirvish side window: the current project's files as a tree along the left edge, like the Explorer. `TAB` on a folder opens it in place, `RET` opens a file in the main window, and `q` in the tree closes it. In the playground every Dired buffer uses dirvish's display, with a header and file sizes; the keys are Dired's.

:::aside{title="Edit the listing itself"}
`C-x C-q` in Dired makes the file names editable: rename files by editing text, even with a replace or a macro, then `C-c C-c` to apply. That comes back in Part 11.
:::

The folder for this lesson, `inbox`, is a mess on purpose; its `brief.md` lists the tidying to do.

::steps
