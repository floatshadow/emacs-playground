---
title: "Recent files, bookmarks and registers"
summary: Reopen recent files, bookmark the places you return to, keep positions in registers, and let Emacs remember where you were in every file.
minutes: 8
---

Getting back to a file is a different problem from finding it the first time. Emacs remembers three kinds of "back": files you opened lately, places you named, and where the cursor was in every file you have ever closed.

:::vscode
Open Recent is `C-x C-r`, and Reopen Closed Editor is `C-x C-r` followed by the file. Bookmarks need an extension in VS Code; in Emacs they are built in and survive restarts. Registers are lighter: a letter that holds a position or some text for this session.
:::

## Recent files

:try[C-x C-r] lists the files you opened recently, newest first, across restarts. Type part of the path and `TAB` to narrow it, `RET` to open. Part 4 makes this list fuzzy, so that typing `todo` is enough.

## Bookmarks

A :jargon[bookmark] is a named place: a file and a position in it. :try[C-x r m] sets one at point and asks for a name, offering the buffer name. :try[C-x r b] jumps to a bookmark by name and opens the file if it is not open. :try[C-x r l] lists every bookmark; `RET` on a line jumps there, `r` renames one, `d` then `x` deletes. Bookmarks are saved as soon as you set them.

## Registers

A :jargon[register] is a slot named by a single character. `C-x r SPC a` stores the current position in register *a*, and `C-x r j a` jumps back to it. Registers also hold text (`C-x r s` to copy, `C-x r i` to insert) and, as Part 3 shows, whole window layouts. They last for the session; use bookmarks for anything you want tomorrow.

## Your place in every file

Emacs remembers where the cursor was in each file you visit, even after you kill the buffer or restart (:jargon[save-place]). Reopen a file and you are where you left off, so killing buffers you are done with costs nothing.

::steps
