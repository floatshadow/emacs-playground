---
title: Calc
summary: Compute with Emacs's stack calculator, convert units, simplify algebra, grab numbers from a document and put results back into it.
minutes: 12
---

Calc is a full calculator inside Emacs: arbitrary precision, units, matrices, symbolic algebra. What makes it more than a calculator app is that it reads numbers from your buffers and writes results back into them, so a sum in a document is computed where it stands.

:::vscode
VS Code has no calculator; you would switch to another app and copy the numbers over by hand. Calc opens below the file you are editing with :try[C-x * c], and every number it needs can come from the buffer.
:::

## The stack

Calc uses :jargon[RPN]: you enter the numbers first, then the operation. `35 RET 210 +` puts 35 on the stack, then 210, and `+` replaces both with 245. The stack is the list in the *Calculator* window, numbered from the bottom (`1:` is the top). `DEL` drops the top entry and `U` undoes. :try[C-x * c] opens Calc, and the same key closes it again; while Calc is open, `C-x o` moves between your file, the *Calculator* window and the trail beside it (a log of every result).

`'` (the apostrophe) starts **algebraic entry**: type a formula the usual way, `(x + 1)^2`, and `RET` puts it on the stack.

## Units

A number can carry a unit: `' 1200 us RET` is 1200 microseconds. `u c` converts the top of the stack to the units you type (`ms`, `s`, `km/h`). It wants one unit to start from: a sum such as `1200 us + 35 ms` first needs `u s`, which simplifies a mix of units into one. Calc knows SI prefixes and hundreds of units.

## Algebra

Calc manipulates formulas, not only numbers. `a s` simplifies the top of the stack, so `3 x + 2 (x - 1) - x` becomes `4 x - 2` (a space between a number and a variable multiplies). `a x` expands products and powers, so `(x + 1)^2 - (x - 1)^2` becomes `4 x`; `a f` factors, `a d` differentiates.

## Grabbing from the buffer and writing back

`C-x * g` (`calc-grab-region`) reads the numbers in the region onto the stack as a vector. For a column of a table, select it as a rectangle (`C-x SPC`, Part 1) and use `C-x * r`; with `C-u` first, each line is read as one formula, so `1200 us` stays a number with its unit. `V R +` then adds up all the entries of the vector.

`C-x * y` (`y` inside Calc) inserts the top of the stack into the buffer at point.

## Embedded mode

:try[C-x * e] turns the formula under the cursor into a live Calc entry, right in the text: Calc keys now act on it, and the result replaces the formula. A formula is the text between blank lines, or between `$` signs inside a line. `C-x * e` again goes back to normal editing.

The exercise is a latency budget in Markdown. You will practise on the stack, then add up the table's time column, write the total into the document, and work out what is left of the budget in place.

::steps
