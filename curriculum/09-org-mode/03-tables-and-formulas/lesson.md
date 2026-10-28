---
title: Tables and spreadsheet formulas
summary: Type a table with | and TAB and let Org align it, add rows, name cells by coordinates, and compute a mean and a column with formulas that recalculate on demand.
minutes: 12
---

An Org table is lines that start with `|`. You type it roughly; Org keeps it aligned. Add formulas and it becomes a small spreadsheet whose formulas are stored as plain text under the table, so the file stays readable anywhere.

:::vscode
A Markdown table that aligns itself while you type, plus the formula bar of a spreadsheet. There is no grid to click: you move with `TAB` and `RET`, and formulas are text in a `#+TBLFM:` line.
:::

## Typing a table

Type `|endpoint|before|after|` and press `TAB`: Org aligns the row and moves to the next cell. On a new line, `|-` and `TAB` makes a rule under the header. In a table:

| Keys | Does |
|---|---|
| `TAB`, `S-TAB` | align, next or previous cell; past the last cell, a new row |
| `RET` | the cell below; a new row before a rule |
| `M-<up>`, `M-<down>`, `M-<left>`, `M-<right>` | move the row or column |
| `M-S-<down>`, `M-S-<up>` | insert or delete a row |
| `C-c -` | a rule below this row |

Typing right after `TAB` or `RET` replaces what the cell held, so overwriting a number needs no deleting.

## Coordinates and formulas

Formulas name cells as `@row$column`. Rows count from 1 at the top and rules do not count; `@>` is the last row; `@I` and `@II` are the first and second rules, so `@I..@II` is the block of data between them in the current column. :try[C-c }] shows the numbers around the table.

A cell that starts with `:=` gets a formula for that cell; `C-c =` asks for a formula for the whole column, applied to every row below the header. Formulas use Emacs Calc: `vmean`, `vsum`, `vmax`, arithmetic on `$2` and `$3` (this row's cells), and `;%.0f` after a formula to format it. Calc reads `a/b*c` as `a/(b*c)`, so put a multiplication first: `100*($3-$2)/$2`. Org writes every formula to the `#+TBLFM:` line under the table; `C-c '` on that line edits them in a buffer of their own.

Formulas run when you install them, and then only in the cell you are in; they do not rerun when a number changes. `C-c *` recalculates the current row; :kbd[C-u C-c *] recalculates the whole table.

`latency.org` holds measurements of the blog's pages before and after a cache went in. Finish the table and let it compute the mean and the change.

::steps
