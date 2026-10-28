# 5.3: two commits on main and an unfinished average() in src/lib.rs,
# not staged, to be stashed.
. .lesson/lib.sh
fresh
mkdir -p .lesson/final && cp src/lib.rs .lesson/final/
sed -i '/^pub fn total_for/,/^$/d' src/lib.rs
commit "Ada Park" "2026-01-12 10:00" "Start the ledger"
cp .lesson/final/lib.rs src/lib.rs
commit "Sam Okafor" "2026-04-02 16:40" "Add totals per category"
cat >> src/lib.rs <<'RS'

pub fn average(expenses: &[Expense]) -> i64 {
    total(expenses) / expenses.len() as i64
}
RS
done_setup
