---
title: Line tools and dynamic abbreviations
summary: Keep, flush, deduplicate, sort and reverse lines, align columns on a separator, and complete words from text that is already on the screen.
minutes: 8
---

Many edits are about whole lines: keep the ones that match, drop the ones that do not, remove duplicates, sort them, line up a column. Emacs has a command for each, and each works on the region, so you choose exactly which lines it touches.

:::vscode
VS Code's palette has Sort Lines Ascending and Delete Duplicate Lines; keeping or dropping lines by pattern, or aligning a column, takes extensions such as Sort Lines. Here all of them are built in, and all take a region. Word completion from other open files (VS Code's "word based suggestions") is `M-/`, and it needs no language server.
:::

## Lines in a region

All of these are `M-x` commands that act on the active region (`keep-lines` and `flush-lines` act from the cursor to the end of the buffer when there is none):

| Command | Does |
|---|---|
| `keep-lines` | keeps only the lines that match a regexp |
| `flush-lines` | deletes the lines that match a regexp |
| `delete-duplicate-lines` | keeps the first of identical lines |
| `sort-lines` | sorts alphabetically; with `C-u`, in reverse |
| `reverse-region` | reverses the order of the lines |
| `align-regexp` | lines up the lines on a regexp, such as `=` |

`keep-lines` and `flush-lines` ask for a regexp, so `^#include` means "starts with #include". `align-regexp` adds spaces before the first match on each line until they all line up.

## Dynamic abbreviations

Type the start of a word and press :try[M-/]: Emacs completes it with the nearest word that starts the same way, looking backward first, then forward, then in your other buffers. `M-/` again replaces it with the next candidate. It works in any buffer, in comments, in prose, with no setup.

`C-M-/` lists all the candidates instead. Since Part 4 they appear in the completion popup, where you pick one with `RET`.

The exercise cleans up the include list of a small C file and its constants, then completes a long variable name with `M-/`.

::steps
