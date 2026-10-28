# 5.1: one commit; src/lib.rs has three unstaged hunks: a doc comment, a
# bug fix (10.0 -> 100.0) and a debug line to discard.
. .lesson/lib.sh
fresh
sed -i 's/amount \* 100\.0/amount * 10.0/' src/lib.rs
commit "Ada Park" "2026-03-02 09:30" "Start the ledger"
sed -i 's/amount \* 10\.0/amount * 100.0/' src/lib.rs
sed -i 's|^pub struct Expense {|/// One line of the ledger: when, how much, what for.\npub struct Expense {|' src/lib.rs
sed -i 's|^    format!("{}.{:02} {CURRENCY}"|    eprintln!("DEBUG format_cents({cents})");\n    format!("{}.{:02} {CURRENCY}"|' src/lib.rs
done_setup
