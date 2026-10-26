---
title: Multiple cursors and iedit
summary: Edit every occurrence of a name at once with iedit, put a cursor on each line, and grow a selection by syntax.
minutes: 12
---

Some edits are the same small change in several places at once: renaming a local variable, adding a word to the start of three lines, selecting exactly the string you are in. Emacs has a tool for each, and each is quicker than a macro for its job.

:::vscode
Rename symbol in file is `C-;` (iedit), and limiting it to one function is `C-0 C-;`. Ctrl+D (add the next match) is `C->`, and Shift+Alt+I (a cursor on every selected line) is `C-c m l`. Expand selection, Shift+Alt+Right, is `C-=`; shrink is `C--`.
:::

## iedit: one name, every occurrence

With the cursor on a symbol, :try[C-;] highlights every occurrence of it in the buffer, and editing one edits them all. With a prefix of zero, `C-0 C-;` (or `M-0 C-;`), only the occurrences in the current function take part, which is exactly what renaming a local variable needs. `C-;` again ends it and keeps the edits. *Where am I* shows *Iedit* while it is on.

## Multiple cursors

`C-c m l` puts a cursor at the start of each line of the :jargon[region]. Everything you type then happens at every cursor: typing, motions, even kills, each with its own kill ring. `C->` adds a cursor at the next match of the current selection, and `C-<` at the previous one. `C-g` goes back to a single cursor.

:::aside{title="When multiple cursors ask a question"}
The first time you use some command with several cursors, multiple-cursors asks whether to run it for all of them or only once. The answer is remembered.
:::

## Growing a selection

:try[C-=] selects the smallest piece of syntax around point, a word, and each further press grows the selection to the next larger piece: the string with its quotes, the argument list, the whole call. `C--` shrinks it back one step. It is the quickest way to select "this string" or "this block" without counting characters.

Some terminals cannot send `C-;`, `C-=` or `C--`. The playground's terminal can; if yours does not, `M-x iedit-mode` and `M-x expreg-expand` are the commands behind them.

::steps
