use std::io::{self, Read};
use tally::{fill, make_buckets, render};

/// Read integers from standard input, one per line, and print a
/// histogram of eight buckets over 0..100.
fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).expect("read stdin");
    let values: Vec<i64> = input.lines().filter_map(|l| l.trim().parse().ok()).collect();
    let mut buckets = make_buckets(0, 100, 8);
    fill(&mut buckets, &values);
    print!("{}", render(&buckets));
}
