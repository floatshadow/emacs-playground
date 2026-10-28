"""Split a restaurant bill, tip included."""


def with_tip(total: float, percent: float = 15) -> float:
    """The total with a tip of `percent` per cent added."""
    return total + total * percent


def split(total: float, people: int, percent: float = 15) -> list[float]:
    """Each person's share of the total with the tip, rounded up to the cent."""
    share = math.ceil(with_tip(total, percent) * 100 / people) / 100
    return [share] * people
