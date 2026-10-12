"""Model feature construction shared by the scorecard, model and explainer.

Kept in one place so the three engines can never disagree about feature order or
units. All values are integers or fixed-point basis points; no floats enter the
feature vector from the request boundary.
"""

from __future__ import annotations

import numpy as np

from engine.scoring_types import RiskScoringInput

FEATURE_ORDER: tuple[str, ...] = (
    "dti_bps",
    "revolving_utilization_bps",
    "delinquencies_24m",
    "inquiries_6m",
    "age_of_oldest_trade_months",
)

FEATURE_LABELS: dict[str, str] = {
    "dti_bps": "debt-to-income ratio",
    "revolving_utilization_bps": "revolving balance versus limit",
    "delinquencies_24m": "delinquencies reported in the last 24 months",
    "inquiries_6m": "recent credit inquiries",
    "age_of_oldest_trade_months": "length of credit history",
}


def feature_values(inp: RiskScoringInput) -> tuple[int, int, int, int, int]:
    return (
        inp.dti_bps,
        inp.revolving_utilization_bps,
        inp.delinquencies_24m,
        inp.inquiries_6m,
        inp.age_of_oldest_trade_months,
    )


def to_row(inp: RiskScoringInput) -> np.ndarray:
    """Return the applicant as a single-row float64 matrix in FEATURE_ORDER."""
    return np.array([list(feature_values(inp))], dtype=np.float64)


def display_value(name: str, value: int) -> str:
    if name.endswith("_bps"):
        return f"{value / 100:.2f}%"
    if name == "age_of_oldest_trade_months":
        return f"{value} mos"
    return str(value)
