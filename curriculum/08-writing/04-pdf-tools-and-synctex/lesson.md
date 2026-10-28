---
title: pdf-tools and SyncTeX
summary: Read a PDF inside Emacs with pdf-tools, and jump between a sentence in the LaTeX source and the same sentence in the PDF in both directions.
minutes: 10
---

In the graphical frame Emacs draws PDFs as pages with pdf-tools: sharp at any zoom, searchable, with an outline and annotations. Paired with SyncTeX, which LaTeX writes next to the PDF, it links every line of the source to its place on the page, so you can go from a sentence you are editing to the page it lands on and back.

:::vscode
This is LaTeX Workshop's internal PDF viewer. SyncTeX from cursor (Ctrl+Alt+J) is `C-c C-v` in the source; Ctrl+click in the PDF is a double-click here. The outline view is `o`, and Ctrl+F in the PDF is `C-s`, as in any buffer.
:::

:::gui-only
This lesson runs in the graphical frame. The terminal frame shows a PDF as its extracted text instead.
:::

## Reading

A PDF opens in pdf-view-mode. `n` and `p` turn pages, :try[SPC] scrolls and turns the page at the bottom, `M-<` and `M->` go to the first and last page, and `M-g g` asks for a page number. `+` and `-` zoom, `0` resets, and `W` fits the page width. The mode line shows `P2/3`: page two of three.

`o` opens the outline, the PDF's table of contents, in a window beside the PDF: on a heading, `RET` jumps there and selects the PDF, and `M-RET` jumps there and closes the outline. `q` closes it without jumping. `C-s` searches the text of the PDF: every match on the page is highlighted, `C-s` again goes to the next one, `RET` stops.

## Source and PDF, both ways

In a LaTeX buffer, :try[C-c C-v] shows the PDF in the other window, on the page of the line the cursor is on, and flashes that line. In the PDF, double-click a word: the source opens with the cursor on it. From the keyboard, search for the words with `C-s`, then press `M-s s` while the search is active: the source of the current match.

## Annotations

pdf-tools writes annotations into the PDF itself, readable by any viewer. Select text with the mouse, then `C-c C-a h` highlights it (`u` underlines, `o` strikes it out); `C-c C-a t` and a click add a note; `C-c C-a l` lists them all. Notes on a PDF that LaTeX rebuilds are lost with the next build, so annotate papers you read, not ones you write.

The paper from lesson 8.3, `/work/writing/paper.tex`, has been built: `paper.pdf` and `paper.synctex.gz` are next to it.

::steps
