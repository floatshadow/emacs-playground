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


class SessionRowsTest(unittest.TestCase):
    """Exit rows on the browser's session root (frames, prefix, echo)."""

    def matching(self, session, **state):
        rows = [r for r in v.load_yaml(HERE.parent / "tables" / "exits.yaml") if isinstance(r, dict)]
        st = {"frames": 2, "windows": [{"id": 1, "buf": "main.rs", "mode": "rust-ts-mode", "selected": True}]}
        st.update(state)
        doc = v.pc.state_doc(st)
        doc["session"] = {"prefix": None, "emacs": None, "mode": "terminal", "echo": None, **session}
        return [r["id"] for r in rows if v.pc.predicate_match(r["when"], doc)]

    def test_help_prefix_replaces_the_generic_row(self):
        self.assertIn("help-prefix", self.matching({"prefix": "C-h"}))
        self.assertNotIn("prefix-pending", self.matching({"prefix": "C-h"}))
        for prefix in ("C-x", "C-x r", "C-h 4", "M-g", "C-c", "<f1>"):
            got = self.matching({"prefix": prefix})
            self.assertIn("prefix-pending", got, prefix)
            self.assertNotIn("help-prefix", got, prefix)
        got = self.matching({})
        self.assertNotIn("prefix-pending", got)
        self.assertNotIn("help-prefix", got)

    def test_query_replace_phase(self):
        for echo in ("Query replacing colour with color: (? for help) ",
                     "Query replacing regexp p\\([0-9]+\\)_ms with p\\1_millis: (? for help) "):
            self.assertIn("query-replace-match", self.matching({"echo": echo}), echo)
        self.assertNotIn("query-replace-match", self.matching({"echo": "Replaced 3 occurrences"}))

    def test_extra_frame(self):
        self.assertNotIn("extra-frame", self.matching({}))
        self.assertIn("extra-frame", self.matching({}, frames=3))
        self.assertEqual([r for r in self.matching({"mode": "gui"}, frames=3, frame="gui") if r.startswith("extra-frame")], [])
        self.assertIn("extra-frame-gui", self.matching({"mode": "gui"}, frames=4, frame="gui"))


class PlainScalarCommentTest(unittest.TestCase):
    def test_comment_cutting_a_plain_scalar(self):
        src = "- text: there is a ## Thursday heading\n  hint: fine\n"
        self.assertEqual(v.plain_scalar_comments(src), [(1, "there is a")])

    def test_quoted_and_own_line_comments_pass(self):
        src = (
            "# a comment of its own\n"
            "- text: \"there is a ## Thursday heading\"\n"
            "  # and another\n"
            "  modules: [base, agent]\n"
            "  hint: C-x#1 is not a comment\n"
        )
        self.assertEqual(v.plain_scalar_comments(src), [])

    def test_curriculum_has_none(self):
        for p in sorted(HERE.parent.glob("*/*/scenario.yaml")) + sorted((HERE.parent / "tables").glob("*.yaml")):
            self.assertEqual(v.plain_scalar_comments(p.read_text(encoding="utf-8")), [], p)


class ConditionKindsTest(unittest.TestCase):
    def errors(self, cond):
        rep = v.Report()
        v.check_condition("c", cond, rep, False, set(), set())
        return rep.errors

    def test_buffer_box_and_long_shell_pass(self):
        for c in [
            {"buffer": {"name": "*vterm*", "process": "running"}},
            {"buffer": {"name": "main.rs", "mode": "rust-ts-mode", "modified": False}},
            {"file": {"host": "box", "path": "/srv/www/index.html", "contains": "Hello"}},
            {"shell": {"host": "box", "run": "systemctl is-active nginx"}},
            {"shell": {"run": "cargo test", "timeout": 300}},
        ]:
            self.assertEqual(self.errors(c), [], c)

    def test_bad_fields_fail(self):
        for c in [
            {"buffer": {"process": "running"}},
            {"buffer": {"name": "x", "process": "alive"}},
            {"file": {"host": "web", "path": "a", "exists": True}},
            {"file": {"path": "/abs", "exists": True}},
            {"shell": {"run": "make", "timeout": 301}},
        ]:
            self.assertNotEqual(self.errors(c), [], c)

    def test_uses_box(self):
        self.assertTrue(v.uses_box({"all": [{"not": {"shell": {"host": "box", "run": "true"}}}]}))
        self.assertFalse(v.uses_box({"any": [{"shell": {"run": "true"}}]}))


if __name__ == "__main__":
    unittest.main()
