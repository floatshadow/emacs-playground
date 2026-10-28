"""Score latency output: read `p50 X`, `p99 Y`, `mean Z` lines (from the
file named on the command line, or standard input) and print a score
out of 100. The budget is 20 ms at p50 and 200 ms at p99."""

import sys

BUDGET = {"p50": 20.0, "p99": 200.0}


def parse(text: str) -> dict[str, float]:
    stats = {}
    for line in text.splitlines():
        name, _, value = line.partition(" ")
        if name in ("p50", "p99", "mean"):
            stats[name] = float(value)
    return stats


def score(stats: dict[str, float]) -> int:
    points = 100.0
    for name, budget in BUDGET.items():
        over = max(0.0, stats[name] - budget) / budget
        points -= 50.0 * min(1.0, over)
    return round(points)


def main() -> None:
    text = open(sys.argv[1]).read() if len(sys.argv) > 1 else sys.stdin.read()
    stats = parse(text)
    print(f"p50 {stats['p50']:.1f} ms, p99 {stats['p99']:.1f} ms")
    print(f"score: {score(stats)}/100")


if __name__ == "__main__":
    main()
