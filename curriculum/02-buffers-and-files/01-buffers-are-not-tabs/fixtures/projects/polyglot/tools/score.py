import json
import sys


def score(rows: list[dict]) -> float:
    return sum(r["p99"] for r in rows) / len(rows)


if __name__ == "__main__":
    print(score(json.load(sys.stdin)))
