# A guide to bucket widths

Every histogram splits its range into buckets of one width. The helper
below picks the width for a range and a bucket count. It does not
compile yet: the division gives an `f64`, but the function promises a
`u32`.

```rust
/// Width of each bucket when `lo..hi` is split into `n` buckets.
pub fn bucket_width(lo: u32, hi: u32, n: u32) -> u32 {
    let span = (hi - lo) as f64;
    (span / n as f64).ceil()
}
```

With `lo = 0`, `hi = 100` and `n = 8`, the width is 13.
