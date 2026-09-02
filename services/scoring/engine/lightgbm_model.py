import os
import math
from decimal import Decimal, ROUND_HALF_EVEN
from engine.scoring_types import RiskScoringInput

try:
    import numpy as np
    import lightgbm as lgb
except ImportError:
    np = None
    lgb = None

MODEL_FILE = os.path.join(os.path.dirname(__file__), 'models', 'model.txt')

class QuantRiskModel:
    def __init__(self):
        self.booster = None
        if lgb is not None:
            if os.path.exists(MODEL_FILE):
                self.booster = lgb.Booster(model_file=MODEL_FILE)

    def predict_pd(self, inp: RiskScoringInput) -> tuple[Decimal, int]:
        if self.booster is not None and np is not None:
            features = np.array([[
                float(inp.dti_bps),
                float(inp.revolving_utilization_bps),
                float(inp.delinquencies_24m),
                float(inp.inquiries_6m),
                float(inp.age_of_oldest_trade_months)
            ]])
            raw_prob = float(self.booster.predict(features)[0])
        else:
            # Exact pure-math logistic calibration fallback
            z = (
                0.0008 * (inp.dti_bps - 3000) +
                0.0005 * (inp.revolving_utilization_bps - 4000) +
                1.2 * inp.delinquencies_24m +
                0.35 * inp.inquiries_6m -
                0.015 * inp.age_of_oldest_trade_months - 2.5
            )
            raw_prob = 1.0 / (1.0 + math.exp(-z))

        if inp.bankruptcy_flag:
            raw_prob = max(raw_prob, 0.35)

        dec_pd = Decimal(str(raw_prob)).quantize(Decimal('0.000001'), rounding=ROUND_HALF_EVEN)
        pd_bps = int((dec_pd * Decimal(10000)).quantize(Decimal('1'), rounding=ROUND_HALF_EVEN))
        return dec_pd, pd_bps
