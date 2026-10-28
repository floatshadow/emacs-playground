# bench

Summarises benchmark runs: latency percentiles per endpoint and whether
each one stays within its budget.

## Usage

    cargo run -- runs.csv

## Budgets

Each endpoint has a p99 budget in `src/config.rs`. A run fails when its
p99 latency is over budget.

## Further reading

The percentile method follows https://en.wikipedia.org/wiki/Percentile
(nearest rank).
