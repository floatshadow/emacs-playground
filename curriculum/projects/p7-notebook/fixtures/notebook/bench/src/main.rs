//! bench: sort pseudo-random lists of growing size and count the
//! comparisons the standard library's sort makes. The input is generated
//! from a fixed seed, so every run prints the same table:
//! one line per size, "SIZE COMPARISONS".

use std::cell::Cell;

/// xorshift64: a tiny deterministic pseudo-random generator.
fn xorshift(state: &mut u64) -> u64 {
    *state ^= *state << 13;
    *state ^= *state >> 7;
    *state ^= *state << 17;
    *state
}

fn comparisons(n: usize, seed: u64) -> u64 {
    let mut state = seed;
    let mut xs: Vec<u64> = (0..n).map(|_| xorshift(&mut state) % 1_000_000).collect();
    let count = Cell::new(0u64);
    xs.sort_by(|a, b| {
        count.set(count.get() + 1);
        a.cmp(b)
    });
    assert!(xs.windows(2).all(|w| w[0] <= w[1]));
    count.get()
}

fn main() {
    for n in [1_000, 10_000, 100_000] {
        println!("{} {}", n, comparisons(n, 0x9e37_79b9_7f4a_7c15));
    }
}
