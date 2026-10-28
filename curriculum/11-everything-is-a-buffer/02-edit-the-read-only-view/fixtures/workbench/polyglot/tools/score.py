"""Average the p99 latency of every language in data/latency.json."""

import json
from pathlib import Path


def score(rows: list[dict]) -> float:
    return sum(r["p99"] for r in rows) / len(rows)


if __name__ == "__main__":
    data = Path(__file__).parent.parent / "data" / "latency.json"
    print(f"mean p99: {score(json.loads(data.read_text())):.2f} ms")
