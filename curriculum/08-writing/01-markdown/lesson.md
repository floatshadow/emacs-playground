---
title: Markdown with live preview
summary: Format Markdown with markdown-mode's keys, align a table with TAB, and preview the page in Emacs and in the preview pane.
minutes: 12
---

Part 8 is about writing prose: Markdown, Typst and LaTeX, with the result next to the source. It starts with Markdown, which you already write in READMEs. Emacs opens `.md` files in markdown-mode, a :jargon[major mode] whose keys insert the markup for you and keep tables tidy.

:::vscode
Markdown: Open Preview to the Side is `C-c C-c p`, which renders the page with pandoc and shows it in a second window. The preview pane beside the screen plays the role of VS Code's preview tab and reloads whenever the file is saved. There is no table formatter to install: `TAB` inside a table aligns it.
:::

## Markup by key

markdown-mode groups its keys under `C-c`. :try[C-c C-s b] makes the word at point bold, or the selected text if a :jargon[region] is active, and pressing it again on a bold word removes the stars. `C-c C-s i` is italic and `C-c C-s c` inline code: the `s` stands for style. :try[C-c C-l] inserts a link and asks for the URL, then for the text to show.

In a list, :try[M-RET] starts a new item with the same marker and indentation as the one you are in, so `- ` items stay `- ` and numbered items count on.

## Tables

Type a table roughly, with a `|` between cells, then press `TAB` anywhere in it: markdown-mode pads every column to the same width and moves to the next cell. In the last cell of the last row, `TAB` adds a new row. You never count spaces.

## Previews

:try[C-c C-c p] runs pandoc on the file and opens the HTML in eww, the web browser built into Emacs, in a window to the right. In eww, `g` reloads, `q` closes it, and `C-x o` goes back to the source.

The preview pane on the right of the page shows `README.md` rendered by the browser. It reloads every time the file is saved: with `C-x C-s`, or by itself a few seconds after you stop typing, since the playground saves edited files automatically.

:::aside{title="Code blocks"}
Inside a fenced code block, `C-c '` opens the block in a buffer of its own language, with that language's indentation and completion. `C-c '` again puts it back.
:::

This lesson edits the README of Lantern, a small static site generator in `/work/writing`.

::steps
