# polyglot

Latency samples from three languages. The Rust CLI describes each
sample and names the slowest, `tools/score.py` averages the p99 column
of `data/latency.json`, and `docs/report.typ` tabulates it.

The team now calls the 99th percentile the *tail*. The code, the data
and the report should say so too, in every language.
