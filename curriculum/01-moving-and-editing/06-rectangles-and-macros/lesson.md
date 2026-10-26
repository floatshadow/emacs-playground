---
title: Rectangles and keyboard macros
summary: Edit columns of text as rectangles, record a keyboard macro with a counter, and replay it over many lines.
minutes: 15
---

Two tools for repetitive edits. A :jargon[rectangle] treats the region as columns instead of lines, for when the thing to change is lined up vertically. A :jargon[keyboard macro] records what you do once and replays it as often as you like, for when the change is the same on every line but not in the same column.

:::vscode
Column selection (Shift+Alt and drag) is `C-x SPC`, and the rectangle commands live under `C-x r`. There is no "repeat last edit" in VS Code; in Emacs you record any sequence of keys with `F3`, stop with `F4`, and `F4` replays it. A macro is not limited to typing: searches, motions and commands are all recorded.
:::

## Rectangles

Put the mark at one corner and point at the opposite corner, and the rectangle is everything between them, column by column. :try[C-x SPC] shows it while you move. :try[C-x r k] kills the rectangle, closing the gap on every line at once; `C-x r y` yanks the last killed rectangle at point; :try[C-x r t] replaces each line of the rectangle with the text you type, which is how you add the same prefix to many lines. `C-x r N` numbers the lines.

## Recording a macro

:try[<f3>] starts recording; *Def* appears in the mode line. Do the edit once, carefully, including the motion to the place where the next repetition should start. :try[<f4>] stops recording. Every key between them is in the macro.

The motion matters most. A good macro starts at the beginning of a line and ends at the beginning of the next one, so that replaying it walks down the file. `C-a` at the start and `C-n C-a` at the end make it robust to lines of different lengths.

## Replaying and counting

After recording, `F4` replays the macro once, and `C-u 4 F4` four times. `C-x C-k r` runs it once on every line of the region.

A macro has a counter. While recording, `F3` inserts its current value and adds one, so each replay inserts the next number. `C-x C-k C-c` sets where the counter starts; set it before you record.

:::aside{title="Keep it"}
A macro lasts until you record another. `C-x C-k n` gives the last one a name, so you can run it with `M-x` later in the session, and `C-x C-k e` edits it as text when one key was wrong.
:::

The exercise turns a plain list of languages into a numbered Markdown table with one rectangle and one macro.

::steps
