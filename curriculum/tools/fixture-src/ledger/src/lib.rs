//! A tiny ledger: read expense lines, add them up.

pub struct Expense {
    pub date: String,
    pub cents: i64,
    pub category: String,
}

pub const CURRENCY: &str = "EUR";

pub fn parse_line(line: &str) -> Option<Expense> {
    let mut parts = line.split(',');
    let date = parts.next()?.trim().to_string();
    let amount: f64 = parts.next()?.trim().parse().ok()?;
    let category = parts.next()?.trim().to_string();
    Some(Expense { date, cents: (amount * 100.0).round() as i64, category })
}

pub fn total(expenses: &[Expense]) -> i64 {
    expenses.iter().map(|e| e.cents).sum()
}

pub fn total_for(expenses: &[Expense], category: &str) -> i64 {
    expenses.iter().filter(|e| e.category == category).map(|e| e.cents).sum()
}

pub fn format_cents(cents: i64) -> String {
    format!("{}.{:02} {CURRENCY}", cents / 100, cents % 100)
}
