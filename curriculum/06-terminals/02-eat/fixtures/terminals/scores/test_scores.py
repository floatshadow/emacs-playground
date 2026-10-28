from scores import grade


def test_top_marks():
    assert grade(95) == "A"


def test_pass_mark():
    assert grade(60) == "C"


def test_fail():
    assert grade(12) == "F"
