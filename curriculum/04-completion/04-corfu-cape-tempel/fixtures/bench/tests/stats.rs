#[path = "../src/stats.rs"]
mod stats;

#[test]
fn p99_of_a_hundred_is_the_99th() {
    let samples: Vec<f64> = (1..=100).map(f64::from).collect();
    assert_eq!(stats::p99(&samples), 99.0);
}
