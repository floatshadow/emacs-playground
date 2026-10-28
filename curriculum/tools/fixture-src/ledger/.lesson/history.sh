# 5.2: five commits by four authors; the CURRENCY line was added by Grace
# Mori and changed to EUR by Lin Chen; the last commit's message has a typo.
. .lesson/lib.sh
fresh
mkdir -p .lesson/final && cp -r src README.md .lesson/final/
sed -i '/^pub const CURRENCY/,/^$/d; /^pub fn total_for/,/^$/d; s/ {CURRENCY}//' src/lib.rs
awk '/^## Rounding/{exit} {print}' .lesson/final/README.md | sed '$d' > README.md
commit "Ada Park" "2026-01-12 10:00" "Start the ledger: parse and total"
cp .lesson/final/src/lib.rs src/lib.rs
sed -i '/^pub fn total_for/,/^$/d; s/"EUR"/"USD"/' src/lib.rs
commit "Grace Mori" "2026-02-03 14:20" "Show the currency in totals"
sed -i 's/"USD"/"EUR"/' src/lib.rs
commit "Lin Chen" "2026-03-17 11:05" "Switch the currency to euros"
cp .lesson/final/src/lib.rs src/lib.rs
commit "Sam Okafor" "2026-04-02 16:40" "Add totals per category"
cp .lesson/final/README.md README.md
commit "Learner" "2026-04-20 09:15" "Explain rounding in teh README"
done_setup
