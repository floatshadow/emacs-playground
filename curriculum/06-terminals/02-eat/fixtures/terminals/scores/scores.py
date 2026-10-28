"""Grade a list of quiz scores."""


def grade(score: int) -> str:
    """The letter grade for a score out of 100."""
    if score >= 90:
        return "A"
    if score >= 75:
        return "B"
    if score > 60:
        return "C"
    return "F"
