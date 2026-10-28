---
title: Notes with org-roam
summary: Keep many small linked notes with org-roam - find or make a note by title, link notes as you write, see what links back, and keep a daily note.
minutes: 10
---

:jargon[org-roam] keeps a folder of small Org notes, one idea each, linked to each other. Every note has an id, so links survive renaming a file; a database follows the links in both directions, so each note can show its :jargon[backlink]s, the notes that point to it. The notes stay plain Org files in `/work/org/roam`.

:::vscode
Foam or Obsidian for Org files: wiki links, a backlinks panel and daily notes. Links are by id rather than by file name, and `C-c n f` is the quick open that also creates.
:::

## Finding, making, linking

| Keys | Does |
|---|---|
| `C-c n f` | find a note by title; a new title makes the note |
| `C-c n i` | insert a link to a note here; a new title makes it first |
| `C-c n l` | show or hide the backlinks window |
| `C-c n d` | today's daily note |
| `C-c n c` | capture into a note without leaving where you are |

:try[C-c n f] completes over the titles of your notes. A title no note has yet opens a capture window on a new file; `C-c C-c` saves it, `C-c C-k` gives up. If what you type also matches an existing title, the highlighted note wins: `M-RET` takes your text as typed.

:kbd[C-c n i] works the same way but leaves a link at the cursor, shown as the note's title. `C-c C-o` on a link follows it.

## Backlinks and daily notes

`C-c n l` opens a window beside the note listing every note that links to it, with the line around each link; `RET` on one goes there. The window follows the note in the selected window. The database learns about a link when the note is saved.

`C-c n d` opens a note named after today in `roam/daily/`, a place for what you did, read or decided, which you can link like any other note.

`roam/` starts with two notes, Emacs and Org mode; Org mode already links to Emacs.

::steps
