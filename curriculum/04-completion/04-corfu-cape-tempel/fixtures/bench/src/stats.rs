//! Percentiles over latency samples, in milliseconds.

/// Arithmetic mean of the samples.
pub fn mean(samples: &[f64]) -> f64 {
    samples.iter().sum::<f64>() / samples.len() as f64
}

/// Nearest-rank percentile: the smallest sample with at least `pct`
/// percent of the samples at or below it.
pub fn percentile(samples: &[f64], pct: f64) -> f64 {
    let mut sorted = samples.to_vec();
    sorted.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let rank = ((pct / 100.0) * sorted.len() as f64).ceil() as usize;
    sorted[rank.saturating_sub(1)]
}

/// The median.
pub fn p50(samples: &[f64]) -> f64 {
    percentile(samples, 50.0)
}

/// The tail: what the slowest one request in a hundred sees.
pub fn p99(samples: &[f64]) -> f64 {
    percentile(samples, 99.0)
}
