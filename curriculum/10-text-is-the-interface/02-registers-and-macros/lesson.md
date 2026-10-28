---
title: "Registers and macros, advanced"
summary: Keep text and numbers in registers, format the macro counter, run a macro on every line of a region, and edit a recorded macro as text.
minutes: 10
---

Part 1 recorded a macro and replayed it; Part 2 kept positions in :jargon[registers]. Both go further. A register can hold a piece of text or a number you increment, and a macro can number forty lines with leading zeros, run once per line of a region, and be fixed after recording without recording it again.

:::vscode
VS Code has one clipboard and no macros without an extension. Registers are named clipboards: `a` to `z`, digits and punctuation, each holding its own text or number for the whole session. A macro is a recorded edit that you can replay, inspect and edit like code.
:::

## Text and number registers

Select some text and press :try[C-x r s] followed by a register name, a single character: the text is copied there and stays until you replace it. :try[C-x r i] and the same name inserts it, as many times as you like, in any buffer. The cursor stays before the inserted text; `C-u C-x r i` leaves it after.

A register can also hold a number. `C-u 41 C-x r n b` stores 41 in register *b*, :try[C-x r +] `b` adds one (or the prefix argument), and `C-x r i b` inserts the current value. It is a counter that lives outside any macro, handy for build numbers and section numbers.

`C-x r j` lists the registers with their contents (Part 4 gave it a preview), so you can see what you have kept.

## The counter's format

The macro counter from Part 1 inserts plain numbers: 1, 2, … 10. :try[C-x C-k C-f] sets a format for it, any `format` string with one number: `%02d` gives 01, 02, … 40, and `%03d` three digits. Type the format without quotes. `C-x C-k C-c` sets the starting value, as before.

## A macro for every line

:try[C-x C-k r] runs the last macro once for each line in the region, with the cursor at the start of the line each time. The macro then needs no motion to the next line: record only the edit itself.

## Editing a macro

:try[C-x C-k RET] opens the last macro in a buffer, one key per line with the command it ran as a comment after `;;`. Characters typed in a row share one line (`OPS-  ;; self-insert-command * 4`). Change, add or delete lines, then `C-c C-c` to keep the new version (`C-x k RET` throws the edit away). Correcting one wrong key this way is faster than recording everything again.

In the terminal frame, `F3` reaches Emacs as the characters the terminal sends for it, so the editor shows it as `ESC O` and `R` on two lines. Leave those two lines as they are: together they are still `F3`.

The exercise is a deploy runbook: a signature and a build number from registers, then forty numbered checks from one macro, and the macro edited for a second list.

::steps
