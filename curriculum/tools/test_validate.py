#!/usr/bin/env python3
"""Tests for the content rules of validate.py (newcomer review, 2026-10-27).

    python3 -m unittest discover -s curriculum/tools -p 'test_*.py'
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))

import validate as v  # noqa: E402


class JsRegexTest(unittest.TestCase):
    def test_portable(self):
        for rx in [
            "M-x",
            r"\((y or n|yes or no)\)",
            r"^[A-Z][a-z]+( to| in| from)? buffer\b",
            r"[Bb]ookmark[^?]*$",
            "(?i)buffer",  # a leading flag group becomes RegExp flags
            "[(?]x",  # inside a class, (? is literal
            r"a\(?x",  # an escaped parenthesis is literal
            "(?:a|b)c",
        ]:
            self.assertIsNone(v.js_regex_problem(rx), rx)

    def test_not_portable(self):
        for rx in [
            "a(?i)b",
            "(?i:file)",
            "(?P<name>a)",
            r"\Afoo",
            r"foo\z",
            r"\pL+",
            r"\x{263a}",
            "[[:alpha:]]+",
        ]:
            self.assertIsNotNone(v.js_regex_problem(rx), rx)


class HintTest(unittest.TestCase):
    STEP = {"goal": "Copy the whole file", "detail": "`C-x h` selects the whole buffer. `M-w` copies it."}

    def test_restating_hint_fails(self):
        self.assertEqual(v.restating_hint(self.STEP, {"hint": "C-x, then h."}), "C-x, then h.")
        self.assertIsNotNone(v.restating_hint(self.STEP, {"hint": "M-w is Alt and w."}))

    def test_hint_with_information_passes(self):
        long = "Control x, then h without Control: the whole buffer turns highlighted."
        self.assertIsNone(v.restating_hint(self.STEP, {"hint": long}))
        # A key the step does not give is information too.
        self.assertIsNone(v.restating_hint(self.STEP, {"hint": "C-g first, then C-x h."}))
        # No keys at all: not this rule's business.
        self.assertIsNone(v.restating_hint(self.STEP, {"hint": "Select everything."}))


class ExitPromptTest(unittest.TestCase):
    def rows(self):
        return [r for r in v.load_yaml(HERE.parent / "tables" / "exits.yaml") if isinstance(r, dict)]

    def test_table_matches_samples(self):
        rep = v.Report()
        v.check_exit_prompts(self.rows(), rep)
        self.assertEqual(rep.errors, [])

    def test_overlapping_rows_are_reported(self):
        rows = self.rows()
        # The buffer row as it was before the fix: it also matched the
        # kill-anyway question.
        for r in rows:
            if r["id"] == "minibuffer-buffer":
                r["when"]["minibuffer.prompt"] = {"regex": "(?i)buffer"}
        rep = v.Report()
        v.check_exit_prompts(rows, rep)
        self.assertTrue(any("kill anyway" in e for e in rep.errors), rep.errors)

    def test_exit_regexes_are_portable(self):
        for r in self.rows():
            for rx in v.predicate_regexes(r.get("when")):
                self.assertIsNone(v.js_regex_problem(rx), (r["id"], rx))


if __name__ == "__main__":
    unittest.main()
