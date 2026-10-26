#!/usr/bin/env python3
"""Copy shared fixture trees into the lessons that use them.

Several lessons in a part work on the same small project (the `hello`
crate in Parts 0, 2 and 3; the `editing` crate in Part 1; ...). Each lesson
must ship its own fixtures (setup.workspace is relative to the lesson
directory and fixture tarballs are built per lesson), so the canonical
trees live in curriculum/tools/fixture-src/<tree> and this script copies
them to <lesson>/fixtures/<tree> for every scenario whose setup.workspace
is "fixtures/<tree>".

  python3 curriculum/tools/sync_fixtures.py          # copy
  python3 curriculum/tools/sync_fixtures.py --check  # exit 1 on drift

validate.py runs the --check logic too.
"""

from __future__ import annotations

import filecmp
import shutil
import sys
from pathlib import Path

import yaml

TOOLS = Path(__file__).resolve().parent
CURRICULUM = TOOLS.parent
SRC = TOOLS / "fixture-src"


def scenarios():
    for sc in sorted(CURRICULUM.glob("*/*/scenario.yaml")):
        if "tools" in sc.parts:
            continue
        with open(sc, encoding="utf-8") as f:
            doc = yaml.safe_load(f) or {}
        ws = ((doc.get("setup") or {}).get("workspace") or "").rstrip("/")
        if ws.startswith("fixtures/") and ws.count("/") == 1:
            tree = ws.split("/", 1)[1]
            if (SRC / tree).is_dir():
                yield sc.parent, tree


def same_tree(a: Path, b: Path) -> bool:
    if not b.is_dir():
        return False
    cmp = filecmp.dircmp(a, b)

    def walk(c) -> bool:
        if c.left_only or c.right_only or c.funny_files:
            return False
        _, mismatch, errors = filecmp.cmpfiles(c.left, c.right, c.common_files, shallow=False)
        if mismatch or errors:
            return False
        return all(walk(sub) for sub in c.subdirs.values())

    return walk(cmp)


def drift() -> list[str]:
    out = []
    for lesson, tree in scenarios():
        if not same_tree(SRC / tree, lesson / "fixtures" / tree):
            out.append(f"{lesson.relative_to(CURRICULUM)}/fixtures/{tree} differs from tools/fixture-src/{tree}")
    return out


def main() -> int:
    if "--check" in sys.argv[1:]:
        d = drift()
        for line in d:
            print(line)
        return 1 if d else 0
    for lesson, tree in scenarios():
        dst = lesson / "fixtures" / tree
        if dst.exists():
            shutil.rmtree(dst)
        shutil.copytree(SRC / tree, dst)
        print(f"synced {dst.relative_to(CURRICULUM)}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
