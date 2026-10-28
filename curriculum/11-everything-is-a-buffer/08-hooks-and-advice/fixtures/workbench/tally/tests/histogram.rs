use tally::{fill, make_buckets, render};

#[test]
fn buckets_cover_the_range() {
    let buckets = make_buckets(0, 100, 8);
    assert_eq!(buckets.len(), 8);
    assert_eq!(buckets[0].lo, 0);
    assert!(buckets[7].hi >= 100);
}

#[test]
fn values_land_in_their_bucket() {
    let mut buckets = make_buckets(0, 10, 2);
    fill(&mut buckets, &[1, 2, 7, 42]);
    assert_eq!(buckets[0].count, 2);
    assert_eq!(buckets[1].count, 1);
}

#[test]
fn render_draws_one_line_per_bucket() {
    let mut buckets = make_buckets(0, 4, 2);
    fill(&mut buckets, &[0, 1, 3]);
    assert_eq!(render(&buckets), "   0..2    ##\n   2..4    #\n");
}
