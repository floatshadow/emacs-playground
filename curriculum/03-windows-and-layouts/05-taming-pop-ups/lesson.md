---
title: Taming pop-ups
summary: Decide where help, build output and other pop-up buffers appear, with display-buffer-alist rules, side windows and popper.
minutes: 15
---

Emacs shows a lot of buffers by itself: help, build output, search results, shells. Where each one appears is not hard-wired. Every time Emacs needs to show a buffer it consults a list of rules, `display-buffer-alist`, and the first rule that matches decides. Once you can read and write a rule, :jargon[pop-ups] go where you want them.

:::vscode
The panel at the bottom (Problems, Output, Terminal) is a habit Emacs can reproduce: build output already opens at the bottom, a third of the screen high, and ``C-` `` toggles the latest pop-up the way Ctrl+` toggles the terminal panel. Where VS Code's layout is fixed, here each kind of buffer gets the place you give it.
:::

## Reading a rule

Each rule is a list of three parts: which buffers, how to show them, and parameters.

```elisp
((derived-mode . compilation-mode)          ; which buffers
 (display-buffer-reuse-window               ; how: reuse a window showing it,
  display-buffer-at-bottom)                 ;      else open one at the bottom
 (window-height . 0.33))                    ; a third of the frame high
```

The playground's windows module has one rule like this for build output, grep, occur and xref results; you can read it with :try[C-h v] `display-buffer-alist`, or in the file `/work/emacs.d/modules/windows.el`. Help has no rule, so it uses Emacs's default: split the selected window and show it there.

## Side windows

`display-buffer-in-side-window` puts a buffer in a :jargon[side window], glued to one edge of the frame. Side windows survive `C-x 1`, which only deletes ordinary windows, and :try[C-x w s] hides all of them and shows them again, like toggling VS Code's panel. Rules give the side (`left`, `right`, `top`, `bottom`) and a size as a fraction of the frame.

## Writing one

The file `notes/popups.el` holds a rule for *Help* with two wrong values. You will fix them, save, and run the file with `M-x eval-buffer`, which adds the rule at the front of the list. Rules take effect immediately; there is nothing to restart. To make a rule permanent, the same lines go into your configuration, which Part 13 shows.

:::aside{title="popper"}
popper keeps track of which buffers are pop-ups. ``C-` `` hides or shows the latest one, ``M-` `` cycles through them, and ``C-M-` `` turns the current buffer into a pop-up or back. Where a terminal cannot send ``C-` ``, `M-x popper-toggle` does the same.
:::

::steps
