# ledger

Reads expense lines from standard input and prints the total.

    cargo run < data/expenses.csv

## Input

One expense per line: a date, an amount and a category, separated by
commas. Blank or malformed lines are skipped.

## Rounding

Amounts are stored as whole cents, rounded half away from zero, so
totals never drift the way sums of floating-point numbers do.
