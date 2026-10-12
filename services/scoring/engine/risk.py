"""Dual-engine risk scorer: WoE scorecard plus calibrated LightGBM plus TreeSHAP.

This is the only place that composes the engines, so a scoring run always
produces a consistent (score, PD, attributions, tier, terms) tuple.
"""

from __future__ import annotations

import time

from engine import policy
from engine.explain import ShapExplainer
from engine.lightgbm_model import QuantRiskModel
from engine.scorecard import WoeScorecard
from engine.scoring_types import RiskScoringInput, RiskScoringOutput


class RiskEngine:
    def __init__(self) -> None:
        self.scorecard = WoeScorecard()
        self.model = QuantRiskModel()
        self.explainer = ShapExplainer(self.model)

    @property
    def model_version(self) -> str:
        return self.model.model_version

    def score(self, inp: RiskScoringInput) -> RiskScoringOutput:
        start = time.perf_counter_ns()

        scorecard_points = self.scorecard.calculate_points(inp)
        calibrated_pd, pd_bps, raw_logit = self.model.predict_pd(inp)
        attributions, adverse_reasons = self.explainer.explain(inp)
        outcome = policy.decide(
            scorecard_points=scorecard_points,
            pd_bps=pd_bps,
            requested_amount_cents=inp.requested_amount_cents,
            bankruptcy_flag=inp.bankruptcy_flag,
        )

        elapsed_micros = (time.perf_counter_ns() - start) // 1000

        return RiskScoringOutput(
            application_id=inp.application_id,
            scorecard_points=scorecard_points,
            calibrated_pd=calibrated_pd,
            pd_basis_points=pd_bps,
            risk_tier=outcome.risk_tier,
            recommended_outcome=outcome.recommended_outcome,
            max_approved_limit_cents=outcome.max_approved_limit_cents,
            recommended_apr_bps=outcome.recommended_apr_bps,
            feature_attributions=attributions,
            adverse_action_reasons=adverse_reasons,
            execution_time_micros=elapsed_micros,
            model_version=self.model_version,
            raw_logit=raw_logit,
            base_log_odds=self.explainer.base_value,
        )
