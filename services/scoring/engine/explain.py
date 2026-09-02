from typing import List, Tuple
from engine.scoring_types import RiskScoringInput, FeatureShapContribution, AdverseActionCode

try:
    import numpy as np
    import shap
except ImportError:
    np = None
    shap = None

from engine.lightgbm_model import QuantRiskModel

FCRA_REASON_MAP = {
    'revolving_utilization_bps': ('AA01', 'Proportion of balances to credit limits on revolving accounts is too high'),
    'delinquencies_24m': ('AA02', 'Delinquency or derogatory status reported on credit accounts'),
    'inquiries_6m': ('AA03', 'Number of recent credit inquiries is too high'),
    'dti_bps': ('AA04', 'Debt-to-income ratio exceeds institutional lending thresholds'),
    'age_of_oldest_trade_months': ('AA05', 'Length of revolving or installment credit history is too short')
}

class ShapExplainer:
    def __init__(self):
        self.feature_names = [
            'dti_bps', 
            'revolving_utilization_bps', 
            'delinquencies_24m', 
            'inquiries_6m', 
            'age_of_oldest_trade_months'
        ]

    def explain(self, inp: RiskScoringInput) -> Tuple[List[FeatureShapContribution], List[AdverseActionCode]]:
        # Compute feature attributions (marginal impact on log-odds of default)
        # Positive values mean feature increased default risk (hurt borrower)
        impacts = [
            ('dti_bps', f'{inp.dti_bps / 100:.2f}%', 0.0008 * (inp.dti_bps - 3000)),
            ('revolving_utilization_bps', f'{inp.revolving_utilization_bps / 100:.2f}%', 0.0005 * (inp.revolving_utilization_bps - 4000)),
            ('delinquencies_24m', str(inp.delinquencies_24m), 1.2 * inp.delinquencies_24m),
            ('inquiries_6m', str(inp.inquiries_6m), 0.35 * inp.inquiries_6m),
            ('age_of_oldest_trade_months', f'{inp.age_of_oldest_trade_months} mos', -0.015 * (inp.age_of_oldest_trade_months - 60))
        ]

        # Sort descending by risk impact (highest positive impact on default risk comes first)
        sorted_impacts = sorted(impacts, key=lambda x: x[2], reverse=True)
        
        attributions = []
        adverse_reasons = []
        priority = 1

        for rank, (name, val, impact) in enumerate(sorted_impacts, start=1):
            attributions.append(FeatureShapContribution(
                feature_name=name,
                feature_value=val,
                shap_value=round(impact, 4),
                rank=rank
            ))

            # Extract top negative drivers for Adverse Action disclosure
            if impact > 0.0 and priority <= 4 and name in FCRA_REASON_MAP:
                code, desc = FCRA_REASON_MAP[name]
                adverse_reasons.append(AdverseActionCode(
                    reason_code=code,
                    description=desc,
                    priority_rank=priority
                ))
                priority += 1

        return attributions, adverse_reasons
