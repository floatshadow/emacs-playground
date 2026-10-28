---
title: occur and occur-edit
summary: List every line that matches a regexp, in one buffer or many, and edit the matching lines from that list.
minutes: 8
---

:try[M-s o] (`occur`) collects every line of the buffer that matches a regexp into a buffer called *Occur*, one line per match with its line number. It is a search result you can read at a glance, jump from, and, the part that matters here, edit: change the lines in *Occur* and the change goes into the files they came from.

:::vscode
Ctrl+Shift+F lists matches across files, and its replace box changes them all. *Occur* is closer to having the matching lines themselves in an editor: you edit them with every Emacs command, `M-%`, macros, rectangles, and each edit lands on the real line. Part 11 does the same with grep results across a whole project.
:::

## The *Occur* buffer

`M-s o` asks for a regexp and opens *Occur* at the bottom of the frame, a third of it high. The cursor stays where it was. In *Occur*, `RET` visits a line, `n` and `p` move between matches, `g` runs the search again (after the buffer changed), and `q` closes it. `M-g M-n` walks the matches from the source buffer too.

## Editing the matches

In *Occur*, `e` switches to occur-edit mode: the line numbers are protected, the text after them is editable, and every change is made in the source buffer at once. Edit as usual, including `M-%` across all the lines. Start it on the first match line: the summary line at the top belongs to no file, and a replacement there stops `M-%` with an error. `C-c C-c` goes back to the read-only list. `C-x C-q` does the same as `e`, as it does in the other read-only listings of Part 11.

The source buffers are changed but not saved; the base module saves them after a few seconds, or `C-x s` saves them now.

## Many buffers

`M-x multi-occur-in-matching-buffers` asks first for a regexp over buffer *names* (`\.md$` for every open Markdown file), then for the regexp to search. The result is the same *Occur* buffer, with a heading per buffer, and `e` edits all of them at once. `M-x multi-occur` lets you pick the buffers one by one instead.

The exercise is the documentation of a small charting tool, written with British spelling. The project now uses American spelling: change *colour* to *color* in one file, then in all five at once.

::steps
