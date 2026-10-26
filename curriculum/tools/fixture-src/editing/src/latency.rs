//! Latency percentiles for one benchmark run, in milliseconds.

pub struct Latency {
    pub p50_ms: f64,
    pub p90_ms: f64,
    pub p99_ms: f64,
    /// The budget is a limit, not a measurement: keep its name.
    pub p99_budget: f64,
}

impl Latency {
    pub fn within_budget(&self) -> bool {
        self.p99_ms <= self.p99_budget
    }

    pub fn spread(&self) -> f64 {
        self.p99_ms - self.p50_ms
    }

    pub fn describe(&self) -> String {
        format!("p50 {} ms, p90 {} ms, p99 {} ms", self.p50_ms, self.p90_ms, self.p99_ms)
    }
}
