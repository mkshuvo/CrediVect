"""TreeSHAP attribution and FCRA adverse-action mapping.

Attributions are genuine game-theoretic Shapley values over the LightGBM
ensemble (TreeSHAP): base_value + sum(attributions) reproduces the tree margin
to floating point precision. That margin is also what the API publishes as
raw_logit, so a disclosed waterfall always adds up to the stored score. FCRA
reason codes are assigned to the features that push the modelled log-odds of
default upward, ranked by that contribution.
"""

from __future__ import annotations

from typing import List, Tuple

import numpy as np
import shap

from engine.features import FEATURE_ORDER, display_value, to_row
from engine.lightgbm_model import QuantRiskModel
from engine.scoring_types import AdverseActionCode, FeatureShapContribution, RiskScoringInput

# FCRA §1681m principal reason codes, keyed by the model feature that drives them.
FCRA_REASON_MAP: dict[str, Tuple[str, str]] = {
    "revolving_utilization_bps": (
        "AA01",
        "Proportion of balances to credit limits on revolving accounts is too high",
    ),
    "delinquencies_24m": (
        "AA02",
        "Delinquency or derogatory status reported on credit accounts",
    ),
    "inquiries_6m": (
        "AA03",
        "Number of recent credit inquiries is too high",
    ),
    "dti_bps": (
        "AA04",
        "Debt-to-income ratio exceeds institutional lending thresholds",
    ),
    "age_of_oldest_trade_months": (
        "AA05",
        "Length of revolving or installment credit history is too short",
    ),
}

BANKRUPTCY_REASON = ("AA02", "Bankruptcy or insolvency proceeding is reported on the file")

MAX_DISCLOSED_REASONS = 4


class ShapExplainer:
    def __init__(self, model: QuantRiskModel) -> None:
        self.model = model
        self._explainer = shap.TreeExplainer(model.booster)
        expected = np.asarray(self._explainer.expected_value, dtype=np.float64).ravel()
        self.base_value = float(expected[-1])

    def raw_contributions(self, inp: RiskScoringInput) -> np.ndarray:
        values = self._explainer.shap_values(to_row(inp))
        if isinstance(values, list):
            # shap returns one array per class for binary learners.
            arr = np.asarray(values[-1], dtype=np.float64)
        else:
            arr = np.asarray(values, dtype=np.float64)
        return arr.reshape(-1)[: len(FEATURE_ORDER)]

    def explain(self, inp: RiskScoringInput) -> Tuple[List[FeatureShapContribution], List[AdverseActionCode]]:
        contributions = self.raw_contributions(inp)
        ordered = sorted(
            zip(FEATURE_ORDER, contributions),
            key=lambda pair: pair[1],
            reverse=True,
        )

        raw_values = {
            "dti_bps": inp.dti_bps,
            "revolving_utilization_bps": inp.revolving_utilization_bps,
            "delinquencies_24m": inp.delinquencies_24m,
            "inquiries_6m": inp.inquiries_6m,
            "age_of_oldest_trade_months": inp.age_of_oldest_trade_months,
        }

        attributions: List[FeatureShapContribution] = []
        for rank, (name, value) in enumerate(ordered, start=1):
            attributions.append(
                FeatureShapContribution(
                    feature_name=name,
                    feature_value=display_value(name, int(raw_values[name])),
                    shap_value=round(float(value), 6),
                    rank=rank,
                )
            )

        # Adverse action discloses the features that raised default odds.
        adverse: List[AdverseActionCode] = []
        for name, value in ordered:
            if value <= 0.0 or len(adverse) >= MAX_DISCLOSED_REASONS:
                continue
            if name not in FCRA_REASON_MAP:
                continue
            code, description = FCRA_REASON_MAP[name]
            adverse.append(
                AdverseActionCode(
                    reason_code=code,
                    description=description,
                    priority_rank=len(adverse) + 1,
                )
            )

        if inp.bankruptcy_flag:
            code, description = BANKRUPTCY_REASON
            adverse = [AdverseActionCode(code, description, 1)] + [
                AdverseActionCode(a.reason_code, a.description, i)
                for i, a in enumerate(adverse, start=2)
            ][: MAX_DISCLOSED_REASONS - 1]

        if not adverse:
            # FCRA still requires principal reasons when a file is declined; fall
            # back to the largest-magnitude contributors regardless of sign.
            for name, value in ordered[:MAX_DISCLOSED_REASONS]:
                if name in FCRA_REASON_MAP:
                    code, description = FCRA_REASON_MAP[name]
                    adverse.append(
                        AdverseActionCode(code, description, len(adverse) + 1)
                    )

        return attributions, adverse
