---
title: Buffers are not tabs
summary: Tell buffers from files, list every buffer Emacs holds, switch to one by name and kill one without touching its file.
minutes: 8
---

A :jargon[buffer] is text that Emacs holds in memory. When you open a file, Emacs reads it into a buffer, you edit the buffer, and saving writes the buffer back. But many buffers have no file at all: help pages, the list of buffers, a scratch area, a shell. Seeing that difference is the first step to feeling at home.

:::vscode
A buffer is closer to an open editor than to a tab: it exists whether or not a :jargon[window] shows it, there is no tab bar, and dozens open at once is normal. Closing a tab is `C-x k`, which kills the buffer and never touches the file. Untitled-1 is any buffer you create with `C-x b` and a new name.
:::

## Listing buffers

:try[C-x C-b] lists every buffer, grouped by project, with its size, :jargon[major mode] and, in the last column, the file it visits. Buffers whose names start and end with stars, like *\*scratch\** and *\*Messages\**, are :jargon[special buffers] that Emacs made itself. A buffer can have an ordinary-looking name and still no file: this lesson's *notes* buffer exists only in memory.

The list is a buffer too, so the usual keys work in it: `n` and `p` move, `RET` opens the buffer on the line, `q` leaves. `m` marks a line and `u` unmarks it; lesson 2.5 shows what marks are for.

## Switching by name

:try[C-x b] asks for a buffer name and switches the selected window to it. `TAB` completes names; `RET` alone goes back to the buffer you were in before. If you type a name that does not exist, Emacs creates an empty buffer with that name, which is how you get a scratch pad. It has no file until you save it with `C-x C-w`.

## Killing

:try[C-x k] kills a buffer: it disappears from the list and from every window that showed it. If the buffer has unsaved changes, Emacs asks first. The file on disk is untouched, and `C-x C-f` reads it back whenever you want. Killing the buffers you are done with is optional; keeping fifty open costs almost nothing.

::steps
