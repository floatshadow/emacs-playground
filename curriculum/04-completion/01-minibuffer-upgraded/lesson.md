---
title: The minibuffer, upgraded
summary: Find any command by a few words in any order, exclude and quote terms, read the notes beside each candidate, and accept exactly what you typed.
minutes: 10
---

Part 4 turns on the completion module, and every prompt changes. :try[M-x] no longer waits for `TAB`: it shows a vertical list of :jargon[candidates] at once and narrows it with each key. You do not need the start of a name any more. Type a few words from anywhere in it, in any order, and the list keeps only what contains them all.

:::vscode
This is the fuzzy command palette, in every prompt: commands, files, buffers, help, bookmarks. Where Ctrl+Shift+P matches letters in order, here you type whole words in any order, which finds commands whose names you only half remember.
:::

## Words in any order

Three packages work together. :jargon[vertico] draws the list, :jargon[orderless] decides what matches, and :jargon[marginalia] writes a note beside each candidate.

Each space-separated word is a separate term, and a candidate must match every term. `M-x trailing white` and `M-x white trailing` both find `delete-trailing-whitespace`. A term is also a :jargon[regexp], so `^kill` means names starting with *kill*.

- `C-n` and `C-p` (or the arrows) move the highlight; `RET` runs or opens the highlighted candidate.
- `TAB` copies the highlighted candidate into the prompt, so you can edit it.
- `M-p` still brings back what you typed before.

## Exclude, quote

Two characters at the start of a term change its meaning:

| Term | Matches |
|---|---|
| `!region` | candidates *without* region |
| `=.` | a literal dot (a plain `.` is a regexp and matches anything) |

## Notes beside each candidate

Marginalia annotates every list. In `M-x` each command shows its key in parentheses, `count-words-region (M-=)`, and the first line of its documentation. Files show their size and age, buffers their mode and file. You can learn a key from `M-x` and use it next time.

## Accept what you typed

`RET` takes the *highlighted* candidate, not the text you typed. Most of the time that is what you want. It surprises you when you want a new name that starts like an existing one: to create `idea.md` beside `ideas.md`, `RET` opens `ideas.md`. `M-RET` accepts the input exactly as typed.

::steps
