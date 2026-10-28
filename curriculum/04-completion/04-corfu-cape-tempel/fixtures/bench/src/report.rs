//! One line per endpoint: percentiles and the budget verdict.

use crate::config::budget_for;
use crate::stats::{mean, p50, p99};

pub struct Summary {
    pub endpoint: String,
    pub mean: f64,
    pub p50: f64,
    pub p99: f64,
    pub budget: f64,
}

pub fn summarise(endpoint: &str, samples: &[f64]) -> Summary {
    Summary {
        endpoint: endpoint.to_string(),
        mean: mean(samples),
        p50: p50(samples),
        p99: p99(samples),
        budget: budget_for(endpoint),
    }
}

pub fn verdict(summary: &Summary) -> &'static str {
    // TODO: a warning band between 90% and 100% of the budget
    if summary.p99 <= summary.budget { "ok" } else { "budget exceeded" }
}

pub fn render_line(summary: &Summary) -> String {
    format!(
        "{:<10} mean {:>6.1}  p50 {:>6.1}  p99 {:>6.1}  {}",
        summary.endpoint, summary.mean, summary.p50, summary.p99, verdict(summary)
    )
}

pub fn render_table(summaries: &[Summary]) -> String {
    // TODO: sort by p99, slowest first
    summaries.iter().map(render_line).collect::<Vec<_>>().join("\n")
}
