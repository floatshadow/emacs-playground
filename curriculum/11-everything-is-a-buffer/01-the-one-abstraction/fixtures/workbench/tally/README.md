# tally

Count numbers into buckets and draw a histogram:

    printf '3\n14\n15\n92\n65\n' | cargo run -q

Each bucket is a half-open range `lo..hi`; `make_buckets` splits a range
into buckets of equal width, `fill` counts the values into their bucket,
and `render` prints one line per bucket. See src/lib.rs.

`sh report.sh` sorts this week's sample.txt into out/summary.txt.
Release notes for each version are in docs/release.html.
