"""Strip blank lines and comments from a numbers file before tally reads it."""

import sys


def clean(lines):
    for line in lines:
        line = line.strip()
        if line and not line.startswith("#"):
            yield line


if __name__ == "__main__":
    for line in clean(sys.stdin):
        print(line)
