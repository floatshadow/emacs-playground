import unittest

from usr_report import get_usr, parse


class ReportTest(unittest.TestCase):
    def test_lookup(self):
        rows = parse("carol is 42\nzoe: no such user\n")
        self.assertEqual(get_usr(rows, "carol"), ("carol", 42))
        self.assertIsNone(get_usr(rows, "zoe"))


if __name__ == "__main__":
    unittest.main()
