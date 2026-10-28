"""Report over the output of the Rust program: `NAME is AGE` lines."""

import sys


def parse(text: str) -> list[tuple[str, int]]:
    rows = []
    for line in text.splitlines():
        name, sep, age = line.partition(" is ")
        if sep:
            rows.append((name, int(age)))
    return rows


def get_usr(rows: list[tuple[str, int]], name: str) -> tuple[str, int] | None:
    """The row of the user called NAME, if any."""
    return next((r for r in rows if r[0] == name), None)


if __name__ == "__main__":
    rows = parse(sys.stdin.read())
    print(get_usr(rows, sys.argv[1]) if len(sys.argv) > 1 else rows)
