# 5.4: main and categories both changed src/lib.rs (two places),
# README.md and data/expenses.csv; the merge of categories into main
# stops with conflicts in all three.
. .lesson/lib.sh
fresh
mkdir -p .lesson/final && cp src/lib.rs README.md .lesson/final/
sed -i '/^pub fn total_for/,/^$/d; s/"EUR"/"USD"/' src/lib.rs
awk '/^## Rounding/{exit} {print}' .lesson/final/README.md | sed '$d' > README.md
commit "Ada Park" "2026-01-12 10:00" "Start the ledger"
retry git checkout -q -B categories
sed -i 's/"USD"/"GBP"/; s|^pub fn total(|/// Every expense added up, in cents.\npub fn total(|' src/lib.rs
sed -i 's/, train$/, transport/' data/expenses.csv
commit "Sam Okafor" "2026-03-20 15:10" "Use transport as a category"
printf '\n## Categories\n\nThe third field is free text, such as coffee, books or transport.\n' >> README.md
commit "Sam Okafor" "2026-03-21 09:40" "Document categories"
retry git checkout -q main
sed -i 's/"USD"/"EUR"/; s|^pub fn total(|/// Sum of all expenses, in cents.\npub fn total(|' src/lib.rs
sed -i 's/, train$/, travel/' data/expenses.csv
commit "Lin Chen" "2026-03-18 11:05" "Switch to euros, rename train to travel"
cp .lesson/final/README.md README.md
commit "Lin Chen" "2026-03-19 10:30" "Explain rounding"
retry sh -c 'git merge -q --no-edit categories >/dev/null 2>&1; test -f .git/MERGE_HEAD'
done_setup
