def clamp_confidence(value: float) -> float:
    return max(0.0, min(1.0, value))
