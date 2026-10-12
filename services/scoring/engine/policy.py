"""Credit policy: maps engine output onto a tier, outcome and terms.

Kept separate from the engines so underwriting appetite is auditable and
changeable without touching the model. Thresholds below are calibrated against
the scored distribution of the training population (see
engine/models/metrics.json, woe_scorecard_percentiles) rather than inherited
from the placeholder scale that shipped first.
"""

from __future__ import annotations

from dataclasses import dataclass

SUPER_PRIME = "SUPER_PRIME"
PRIME = "PRIME"
NEAR_PRIME = "NEAR_PRIME"
SUBPRIME = "SUBPRIME"
HIGH_RISK = "HIGH_RISK"

APPROVE = "APPROVE"
MANUAL_REVIEW = "MANUAL_REVIEW"
DECLINE = "DECLINE"

# scorecard points, probability of default in basis points
TIER_RULES: tuple[tuple[str, int, int, str, int, float], ...] = (
    (SUPER_PRIME, 720, 250, APPROVE, 790, 1.00),
    (PRIME, 670, 500, APPROVE, 950, 1.00),
    (NEAR_PRIME, 610, 900, MANUAL_REVIEW, 1240, 0.75),
    (SUBPRIME, 520, 100_000, DECLINE, 1999, 0.00),
    (HIGH_RISK, 300, 100_000, DECLINE, 2499, 0.00),
)


@dataclass(frozen=True)
class PolicyOutcome:
    risk_tier: str
    recommended_outcome: str
    recommended_apr_bps: int
    max_approved_limit_cents: int


def decide(scorecard_points: int, pd_bps: int, requested_amount_cents: int, bankruptcy_flag: bool) -> PolicyOutcome:
    # A bankruptcy on file is a policy knockout: it is not a scored feature, and
    # no point total or modelled PD can approve it.
    if bankruptcy_flag:
        return PolicyOutcome(HIGH_RISK, DECLINE, 2499, 0)

    for tier, min_points, max_pd_bps, outcome, apr_bps, limit_share in TIER_RULES:
        if scorecard_points >= min_points and pd_bps <= max_pd_bps:
            return PolicyOutcome(
                risk_tier=tier,
                recommended_outcome=outcome,
                recommended_apr_bps=apr_bps,
                max_approved_limit_cents=int(round(requested_amount_cents * limit_share)),
            )

    return PolicyOutcome(HIGH_RISK, DECLINE, 2499, 0)
