//! tally: count numbers into buckets and draw a histogram.

/// A half-open range `lo..hi` and how many values fell into it.
#[derive(Debug, Clone, PartialEq)]
pub struct Bucket {
    pub lo: i64,
    pub hi: i64,
    pub count: usize,
}

/// Split `lo..hi` into `n` buckets of equal width.
pub fn make_buckets(lo: i64, hi: i64, n: usize) -> Vec<Bucket> {
    let width = ((hi - lo) as usize).div_ceil(n) as i64;
    (0..n as i64)
        .map(|i| Bucket {
            lo: lo + i * width,
            hi: lo + (i + 1) * width,
            count: 0,
        })
        .collect()
}

/// Count each value into the bucket that holds it; values outside are ignored.
pub fn fill(buckets: &mut [Bucket], values: &[i64]) {
    for &v in values {
        if let Some(b) = buckets.iter_mut().find(|b| b.lo <= v && v < b.hi) {
            b.count += 1;
        }
    }
}

/// One line per bucket: its range and a bar of `#`.
pub fn render(buckets: &[Bucket]) -> String {
    buckets
        .iter()
        .map(|b| format!("{:>4}..{:<4} {}\n", b.lo, b.hi, "#".repeat(b.count)))
        .collect()
}
