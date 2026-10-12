"""Weight-of-Evidence scorecard producing a 300-850 credit score.

Points come from data-derived bins (see engine/train.py): each feature is
quantile-binned on the training population, each bin carries its WoE, and the
WoE vector is scored through an unpenalized logistic fit of default log-odds.
The final score follows the standard scale

    score = offset - factor * ln(odds_of_default)

with offset and factor fixed by the base-points / points-to-double-odds pair
recorded in the artifact.
"""

from __future__ import annotations

import json
import os
import bisect
from decimal import Decimal, ROUND_HALF_EVEN

from engine.features import feature_values
from engine.scoring_types import RiskScoringInput

ENGINE_DIR = os.path.dirname(__file__)
SCORECARD_FILE = os.path.join(ENGINE_DIR, "models", "scorecard.json")


class WoeScorecard:
    def __init__(self, artifact_file: str = SCORECARD_FILE) -> None:
        if not os.path.exists(artifact_file):
            raise RuntimeError(
                f"missing scorecard artifact at {artifact_file}; run 'make train' to build it"
            )
        with open(artifact_file, "r", encoding="utf-8") as fh:
            self.spec = json.load(fh)

        self.feature_names = [f["name"] for f in self.spec["features"]]
        if [f["name"] for f in self.spec["features"]] != self.spec["feature_order"]:
            raise RuntimeError("scorecard artifact feature names disagree with feature order")

        scaling = self.spec["scaling"]
        self.factor = float(scaling["factor"])
        self.offset = float(scaling["offset"])
        self.intercept = float(self.spec["log_odds_default"]["intercept"])
        self.coefficients = [float(c) for c in self.spec["log_odds_default"]["coefficients"]]
        self.min_points = int(self.spec["score_bounds"]["min"])
        self.max_points = int(self.spec["score_bounds"]["max"])

    def woe_of(self, feature_index: int, value: int) -> float:
        feat = self.spec["features"][feature_index]
        edges = feat["edges"]
        woe = feat["woe"]
        idx = bisect.bisect_right(edges, value) - 1
        idx = max(0, min(idx, len(woe) - 1))
        return float(woe[idx])

    def log_odds_default(self, inp: RiskScoringInput) -> float:
        values = feature_values(inp)
        total = self.intercept
        for i, value in enumerate(values):
            total += self.coefficients[i] * self.woe_of(i, value)
        return total

    def calculate_points(self, inp: RiskScoringInput) -> int:
        raw = self.offset - self.factor * self.log_odds_default(inp)
        clamped = max(float(self.min_points), min(float(self.max_points), raw))
        return int(Decimal(str(clamped)).quantize(Decimal("1"), rounding=ROUND_HALF_EVEN))

    def information_values(self) -> dict[str, float]:
        return {f["name"]: float(f["information_value"]) for f in self.spec["features"]}

    def describe_bins(self, feature_name: str) -> list[dict]:
        """Bin edges with their WoE and implied points, for governance display."""
        feat = next(f for f in self.spec["features"] if f["name"] == feature_name)
        out = []
        for i, woe in enumerate(feat["woe"]):
            coeff = self.coefficients[self.feature_names.index(feature_name)]
            out.append(
                {
                    "lo": feat["edges"][i],
                    "hi": feat["edges"][i + 1],
                    "woe": round(woe, 6),
                    "points": round(-self.factor * coeff * woe, 2),
                }
            )
        return out
