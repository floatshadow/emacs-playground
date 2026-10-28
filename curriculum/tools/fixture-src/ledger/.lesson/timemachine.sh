# 5.5: README.md had a Rounding section from the second commit until the
# third removed it. The working tree has two hunks: a good edit of the
# first paragraph and a stray line of Vim keys.
. .lesson/lib.sh
fresh
mkdir -p .lesson/final && cp README.md .lesson/final/
awk '/^## Rounding/{exit} {print}' .lesson/final/README.md | sed '$d' > .lesson/v1.md
cp .lesson/v1.md README.md
commit "Ada Park" "2026-01-12 10:00" "Start the ledger"
cp .lesson/final/README.md README.md
commit "Grace Mori" "2026-02-03 14:20" "Explain rounding"
cp .lesson/v1.md README.md
commit "Lin Chen" "2026-03-17 11:05" "Trim the README"
printf '\n## Example\n\n    $ cargo run < data/expenses.csv\n    total 93.20 EUR\n' >> README.md
commit "Sam Okafor" "2026-04-02 16:40" "Add an example"
sed -i 's/^Reads expense lines from standard input and prints the total\.$/Reads expense lines from standard input and prints the total in euros./' README.md
sed -i 's/^commas\. Blank or malformed lines are skipped\.$/&\njjjjkkkk:wq/' README.md
done_setup
