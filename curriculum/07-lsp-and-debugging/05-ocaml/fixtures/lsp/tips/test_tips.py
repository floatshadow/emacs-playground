from tips import split, with_tip


def test_tip():
    assert with_tip(100) == 115


def test_split():
    assert split(90, 3, percent=10) == [33.0, 33.0, 33.0]
