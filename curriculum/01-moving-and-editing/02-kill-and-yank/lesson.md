---
title: Kill, yank and the kill ring
summary: Select with the mark, cut, copy and paste with the kill ring, and get back text you cut several kills ago.
minutes: 15
---

Emacs called cutting *killing* and pasting *yanking* before cut and paste had names. The words are old; the idea behind them is better than a clipboard: every piece of text you remove is kept, in order, and you can get any of them back.

:::vscode
`C-w` is Cut, `M-w` is Copy and `C-y` is Paste. There is no separate "select" step: you put down the :jargon[mark] with `C-SPC`, move, and the text in between is the :jargon[region]. The :jargon[kill ring] is clipboard history built in: `M-y` right after `C-y` swaps the paste for the kill before it. Ctrl+Shift+K (delete line) is `C-k`, which cuts instead of deleting.
:::

## Mark, point, region

The cursor's position is called :jargon[point]. :try[C-SPC] sets the mark where point is; then any motion key moves point, and the text between mark and point is highlighted as the region. :try[C-x h] marks the whole buffer. :try[C-x C-x] swaps point and mark, which is how you go back to where the region started, or extend it at the other end.

## Kill, copy, yank

:try[C-w] kills the region and :try[M-w] copies it. Both put the text on the kill ring. :try[C-y] yanks the most recent kill at point.

Many kill commands need no region at all. :try[C-k] kills from point to the end of the line; at the end of a line it kills the line break, so `C-k C-k` takes a whole line. `M-d` kills the next word and `M-DEL` the previous one. `M-z` asks for a character and kills up to and including it. Kills made one after another join into one entry, so three `C-k` in a row come back with one `C-y`.

## The ring

Right after `C-y`, :try[M-y] replaces what you just yanked with the kill before it, and again with the one before that. Nothing you killed in this session is lost. `M-y` on its own, without a `C-y` first, shows the whole ring as a list to pick from.

The playground connects the kill ring to your browser's clipboard: what you copy in Emacs can be pasted outside, and the other way round.

::steps
