//! polyglot: latency samples from three languages.

/// Greet the person running the service.
pub fn greet(name: &str) -> String {
    format!("Hello, {name}!")
}

/// How long a request takes, in milliseconds: the median and the
/// 99th percentile of one run.
///
/// The numbers come from the benchmark in each language's folder;
/// data/latency.json holds the same values for the Python tool.
#[derive(Debug, Clone)]
pub struct Sample {
    pub name: String,
    pub p50: f64,
    pub p99: f64,
}

/// One line about a sample.
pub fn describe(s: &Sample) -> String {
    format!("{:<8} p50 {:>5.1} ms  p99 {:>5.1} ms", s.name, s.p50, s.p99)
}

/// The sample with the slowest p99.
pub fn worst(samples: &[Sample]) -> Option<&Sample> {
    samples.iter().max_by(|a, b| a.p99.total_cmp(&b.p99))
}
