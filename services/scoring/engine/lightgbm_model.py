"""Calibrated LightGBM probability-of-default engine.

The booster is loaded from the LightGBM native text format (no pickle, no
numpy .npy, no torch). If the trained artifacts are absent this raises instead
of silently falling back to a hand-written formula, so a mis-deployed container
fails its health check rather than returning fake probabilities.
"""

from __future__ import annotations

import hashlib
import json
import math
import os
from decimal import Decimal, ROUND_HALF_EVEN

from engine.features import FEATURE_ORDER, to_row
from engine.scoring_types import RiskScoringInput

ENGINE_DIR = os.path.dirname(__file__)
MODEL_DIR = os.path.join(ENGINE_DIR, "models")
MODEL_FILE = os.path.join(MODEL_DIR, "model.txt")
CALIBRATOR_FILE = os.path.join(MODEL_DIR, "calibrator.json")
DATASET_HASH_FILE = os.path.join(MODEL_DIR, "dataset.sha256")

try:
    import lightgbm as lgb
except ImportError:  # pragma: no cover - the runtime image always ships lightgbm
    lgb = None


def _sha256(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as fh:
        for chunk in iter(lambda: fh.read(65536), b""):
            digest.update(chunk)
    return digest.hexdigest()


class QuantRiskModel:
    """Monotonic-constrained tree ensemble plus a Platt scaling layer."""

    def __init__(
        self,
        model_file: str = MODEL_FILE,
        calibrator_file: str = CALIBRATOR_FILE,
        dataset_hash_file: str = DATASET_HASH_FILE,
    ) -> None:
        if lgb is None:
            raise RuntimeError("lightgbm is not installed in this image")
        for path, label in ((model_file, "model"), (calibrator_file, "calibrator")):
            if not os.path.exists(path):
                raise RuntimeError(
                    f"missing {label} artifact at {path}; run 'make train' to build it"
                )

        self.booster = lgb.Booster(model_file=model_file)
        with open(calibrator_file, "r", encoding="utf-8") as fh:
            calibrator = json.load(fh)
        self.slope = float(calibrator["slope"])
        self.intercept = float(calibrator["intercept"])
        if calibrator.get("features", list(FEATURE_ORDER)) != list(FEATURE_ORDER):
            raise RuntimeError("calibrator artifact was trained on a different feature order")

        self.artifact_sha256 = _sha256(model_file)
        dataset_sha = ""
        if os.path.exists(dataset_hash_file):
            with open(dataset_hash_file, "r", encoding="utf-8") as fh:
                dataset_sha = fh.read().strip()
        self.dataset_sha256 = dataset_sha
        self.num_trees = self.booster.num_trees()
        self.model_version = (
            f"v1.1.0-{self.artifact_sha256[:8]}"
            if not dataset_sha
            else f"v1.1.0-{dataset_sha[:8]}"
        )

    def margin(self, inp: RiskScoringInput) -> float:
        """Raw tree ensemble margin (log-odds before Platt scaling).

        raw_score=True is required: the default predict() output is already
        sigmoid-transformed, and the Platt layer was fitted against the margin.
        """
        return float(self.booster.predict(to_row(inp), raw_score=True)[0])

    def predict_pd(self, inp: RiskScoringInput) -> tuple[Decimal, int, float]:
        """Return (calibrated PD, PD in basis points, tree margin).

        The third value is the uncalibrated ensemble margin, exactly as the proto
        documents raw_logit: it is the quantity TreeSHAP attributes against and
        the one scoring_runs.raw_ml_logit stores for calibration monitoring. The
        Platt parameters belong to the PD, not to the persisted score.
        """
        raw_margin = self.margin(inp)
        logit = self.slope * raw_margin + self.intercept
        prob = 1.0 / (1.0 + math.exp(-logit))

        dec_pd = Decimal(f"{prob:.10f}").quantize(Decimal("0.000001"), rounding=ROUND_HALF_EVEN)
        pd_bps = int((dec_pd * Decimal(10000)).quantize(Decimal("1"), rounding=ROUND_HALF_EVEN))
        return dec_pd, pd_bps, raw_margin
