//! Latency budgets per endpoint, in milliseconds.

pub const DEFAULT_P99_BUDGET: f64 = 50.0;

pub fn budget_for(endpoint: &str) -> f64 {
    match endpoint {
        "search" => 80.0,
        "checkout" => 120.0,
        // TODO: read budgets from a file instead
        _ => DEFAULT_P99_BUDGET,
    }
}
