from dataclasses import dataclass, field
from decimal import Decimal, ROUND_HALF_EVEN
from typing import List, Optional

@dataclass(frozen=True)
class RiskScoringInput:
    application_id: str
    applicant_id: str
    # Financial data in integer minor units (USD Cents)
    requested_amount_cents: int
    monthly_income_cents: int
    currency_code: str
    loan_term_months: int
    loan_purpose: str
    # Bureau data in integer minor units & basis points (1 bp = 0.01%)
    total_debt_cents: int
    revolving_balance_cents: int
    revolving_limit_cents: int
    revolving_utilization_bps: int
    delinquencies_24m: int
    inquiries_6m: int
    total_trades: int
    open_trades: int
    age_of_oldest_trade_months: int
    bankruptcy_flag: bool
    bureau_score: int

    @property
    def dti_bps(self) -> int:
        """Calculates DTI in integer basis points using Banker's Rounding."""
        if self.monthly_income_cents <= 0:
            return 10000 # 100%
        # total debt / income in basis points
        raw_dti = (Decimal(self.total_debt_cents) * Decimal(10000)) / Decimal(self.monthly_income_cents)
        return int(raw_dti.quantize(Decimal('1'), rounding=ROUND_HALF_EVEN))

@dataclass
class FeatureShapContribution:
    feature_name: str
    feature_value: str
    shap_value: float # Negative means it hurts score / increases default risk
    rank: int

@dataclass
class AdverseActionCode:
    reason_code: str
    description: str
    priority_rank: int

@dataclass
class RiskScoringOutput:
    application_id: str
    scorecard_points: int # 300 to 850
    calibrated_pd: Decimal # 0.000000 to 1.000000
    pd_basis_points: int # e.g. 480 = 4.80%
    risk_tier: str # SUPER_PRIME, PRIME, NEAR_PRIME, SUBPRIME, HIGH_RISK
    recommended_outcome: str # APPROVE, MANUAL_REVIEW, DECLINE
    max_approved_limit_cents: int
    recommended_apr_bps: int
    feature_attributions: List[FeatureShapContribution] = field(default_factory=list)
    adverse_action_reasons: List[AdverseActionCode] = field(default_factory=list)
    execution_time_micros: int = 0
    model_version: str = "v1.0.0"
