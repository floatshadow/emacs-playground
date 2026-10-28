import unittest

from score import parse, score


class ScoreTest(unittest.TestCase):
    def test_within_budget(self):
        self.assertEqual(score({"p50": 10.0, "p99": 100.0}), 100)

    def test_parse(self):
        self.assertEqual(parse("p50 1.0\np99 2.5\nmean 1.2\n")["p99"], 2.5)


if __name__ == "__main__":
    unittest.main()
