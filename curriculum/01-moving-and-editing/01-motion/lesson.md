---
title: Motion
summary: Move by character, word, line, expression and function, and jump by line number, without leaving the home row.
minutes: 15
---

Emacs motion keys look arbitrary until you see that they are a grid: the letter says the direction, the modifier says the size of the step. Learn the grid once and you can guess keys you have never pressed.

:::vscode
Arrows still work, but your hands leave the keyboard's home row for them. `C-f` `C-b` `C-n` `C-p` are the arrows; `M-f` and `M-b` are Ctrl+Left and Ctrl+Right; `C-a` and `C-e` are Home and End; `M-<` and `M->` are Ctrl+Home and Ctrl+End; `M-g g` is Ctrl+G. VS Code has no key for "jump over this whole bracketed block": Emacs has `C-M-f`.
:::

## The grid

The letters are mnemonics: **f**orward, **b**ack, **n**ext line, **p**revious line, **a** for the start of a line and **e** for its end. Control moves by the smallest unit, a character or a line: :try[C-f], :try[C-n], :try[C-a], :try[C-e]. Meta (Alt, or Esc and then the key) moves by the next unit up: :try[M-f] and :try[M-b] by words, `M-a` and `M-e` by sentences. :try[M-m] goes to the first non-blank character of the line, which in code is usually where you want to be.

Control and Meta together move by a balanced expression, a :jargon[sexp]: a word, a string, or everything between a bracket and its partner. With the cursor on an opening `{`, :try[C-M-f] jumps past the matching `}`, however many lines away; `C-M-b` jumps back. `C-M-a` and `C-M-e` go to the start and end of the function you are in (a :jargon[defun]), in any language Emacs understands.

## Bigger jumps

:try[M-<] and :try[M->] go to the top and bottom of the buffer. :try[C-v] scrolls down a screen and `M-v` up. :try[M-g g] asks for a line number. Any of these takes a count first: `C-u 10 C-n` moves ten lines, `C-u 3 M-f` three words.

When you jump, the screen follows, but not always where you want it. :try[C-l] scrolls so the cursor's line is in the middle; press it again for the top, again for the bottom. `C-M-l` scrolls to show as much of the current function as fits.

## Five spots

`src/main.rs` has five spots marked in its comments, *(1)* to *(5)*. Reach each with as few keys as you can. Watch the ticker: it names each motion command as you use it, which is the fastest way to learn their names.

::steps
