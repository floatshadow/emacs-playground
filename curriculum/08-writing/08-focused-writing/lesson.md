---
title: Focused writing
summary: Give prose a calm, centred page, fix misspellings with M-$, wrap paragraphs softly or hard, and count words.
minutes: 8
---

Code wants a wide window; prose wants a column you can read. This lesson sets up a page for writing and the habits that go with it: spelling fixed from the keyboard, paragraphs wrapped the way the file needs, and a word count when the editor asks for one.

:::vscode
Zen Mode is olivetti, which you met in lesson 3.8. Word wrap (Alt+Z) is visual-line-mode. The spell-checker extensions' quick fix is `M-$`, from jinx, which checks every prose buffer as you type with the same Hunspell dictionaries. The status bar's word count is `C-u M-=` or `M-x count-words`.
:::

## A page for prose

`M-x olivetti-mode` puts the text in a centred column with empty margins, and turns on `visual-line-mode` with it: long lines wrap at word boundaries on screen without changing the file, and `C-a`, `C-e` and `C-k` work on what you see as a line. The mode line shows `Olv Wrap`. That suits files where each paragraph is one long line, like this lesson's draft. `M-x visual-line-mode` alone wraps without the column. Each command toggles its mode; with `C-u` before `M-x`, it only ever turns the mode on.

## Spelling

jinx underlines misspelt words on screen as you type, in Markdown, Org, LaTeX, Typst and commit messages; code buffers are left alone. :try[M-$] corrects the misspelling nearest the cursor: the suggestions appear at the bottom, numbered, and a digit picks one. The first suggestion is usually right, not always. `C-u M-$` walks through every misspelling in the buffer, one after another. In the list, `@` saves the word to your personal dictionary, so names stop being flagged. `C-M-$` switches between American and British English.

## Hard wrapping

Some files want real line breaks: commit messages, plain-text email, sources that version control diffs line by line. :try[M-q] refills the paragraph around the cursor so that no line is longer than the fill column, 80 here. `C-x f` changes the fill column for this buffer; `C-u M-q` also justifies.

## Counting

`C-u M-=` counts the lines, words and characters of the whole buffer. Plain `M-=` counts only from the mark to the cursor, even when no region is shown, so its numbers are often smaller than you expect. `M-x count-words` counts the region when one is active and the buffer otherwise.

The draft is `/work/writing/draft.md`.

::steps
