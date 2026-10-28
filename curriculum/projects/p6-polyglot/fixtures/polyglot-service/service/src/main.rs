//! latency: read response times in milliseconds, one per line, from the
//! file named on the command line or from standard input, and print
//! the median, the 99th percentile and the mean.

use std::io::{self, Read};

/// The value below which a fraction `q` (0.0 to 1.0) of the sorted
/// samples fall (nearest rank).
fn percentile(sorted: &[f64], q: f64) -> f64 {
    let rank = (q * sorted.len() as f64).ceil() as usize;
    sorted[rank.clamp(1, sorted.len()) - 1]
}

fn mean(xs: &[f64]) -> f64 {
    xs.iter().sum::<f64>() / xs.len() as f64
}

fn main() -> io::Result<()> {
    let mut input = String::new();
    match std::env::args().nth(1) {
        Some(path) if path != "-" => input = std::fs::read_to_string(path)?,
        _ => {
            io::stdin().read_to_string(&mut input)?;
        }
    }
    let mut samples: Vec<f64> = input
        .lines()
        .filter_map(|l| l.trim().parse().ok())
        .collect();
    if samples.is_empty() {
        eprintln!("latency: no samples");
        std::process::exit(1);
    }
    samples.sort_by(|a, b| a.total_cmp(b));
    println!("p50 {:.1}", percentile(&samples, 0.5));
    println!("p99 {:.1}", percentile(samples, 0.99));
    println!("mean {:.1}", mean(&samples));
    Ok(())
}
