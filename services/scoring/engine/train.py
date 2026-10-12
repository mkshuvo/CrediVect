"""Train the CrediVect probability-of-default model.

Builds a reproducible synthetic applicant population from a fixed seed, fits a
monotonic-constrained LightGBM gradient-boosted classifier, Platt-scales the raw
tree margin on a held-out split, and writes three artifacts into engine/models/:

  model.txt            LightGBM native text booster (no pickle)
  calibrator.json      Platt scaling coefficients (slope, intercept)
  metrics.json         AUC / KS / Brier / calibration deciles
  baseline_deciles.json  expected PD histogram used as the PSI baseline
  dataset.sha256       checksum of the generated training matrix and labels

Run with: make train   (or python -m engine.train inside the scoring image)
"""

from __future__ import annotations

import datetime
import hashlib
import json
import math
import os

import lightgbm as lgb
import numpy as np
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import brier_score_loss, roc_auc_score, roc_curve
from sklearn.model_selection import train_test_split

SEED = 20260903
N_APPLICANTS = 200_000

FEATURES = [
    "dti_bps",
    "revolving_utilization_bps",
    "delinquencies_24m",
    "inquiries_6m",
    "age_of_oldest_trade_months",
]

# 1 = PD must not decrease as the feature rises, -1 = PD must not increase.
MONOTONIC_CONSTRAINTS = [1, 1, 1, 1, -1]

MODEL_DIR = os.path.join(os.path.dirname(__file__), "models")
MODEL_FILE = os.path.join(MODEL_DIR, "model.txt")
CALIBRATOR_FILE = os.path.join(MODEL_DIR, "calibrator.json")
SCORECARD_FILE = os.path.join(MODEL_DIR, "scorecard.json")
METRICS_FILE = os.path.join(MODEL_DIR, "metrics.json")
BASELINE_FILE = os.path.join(MODEL_DIR, "baseline_deciles.json")
DATASET_HASH_FILE = os.path.join(MODEL_DIR, "dataset.sha256")

# Scorecard scaling: 660 points at 50:1 good/bad odds, 50 points to double the
# odds (PDO). PDO is chosen so the modelled odds range of a real applicant
# population maps onto the advertised 300-850 band instead of crowding into a
# 100-point slice of it. factor = PDO / ln(2), offset = base - factor*ln(odds).
SCORE_BASE_POINTS = 660.0
SCORE_BASE_ODDS = 50.0
POINTS_TO_DOUBLE_ODDS = 50.0
WOE_BINS = 12
MIN_BIN_SHARE = 0.02


def generate_population(n: int, rng: np.random.Generator) -> tuple[np.ndarray, np.ndarray]:
    """Sample an applicant population and its 24-month default outcome."""
    dti = np.clip(rng.normal(3400, 1300, n), 0, 9000)
    util = np.clip(rng.beta(1.8, 4.5, n) * 11000, 0, 10000)
    delinq = rng.poisson(0.5, n)
    inq = rng.poisson(1.2, n)
    age = np.clip(rng.gamma(2.2, 42.0, n), 3, 480)

    # Latent default log-odds. Delinquency and high utilization dominate; the
    # utilization penalty accelerates above 70% so the ensemble has genuine
    # non-linearity to learn beyond a plain logistic surface.
    z = (
        -3.55
        + 0.00040 * (dti - 3000)
        + 0.00035 * (util - 3500)
        + 0.00055 * np.maximum(util - 7000, 0.0)
        + 0.90 * delinq
        + 0.30 * np.minimum(inq, 8)
        - 0.0115 * (age - 60)
        + 0.00020 * np.maximum(dti - 5000, 0.0)
        + rng.normal(0, 0.55, n)
    )

    prob_default = 1.0 / (1.0 + np.exp(-z))
    labels = (rng.random(n) < prob_default).astype(np.int8)

    X = np.column_stack([dti, util, delinq.astype(np.float64), inq.astype(np.float64), age])
    return X.astype(np.float64), labels


def woe_matrix(X: np.ndarray, spec: dict) -> np.ndarray:
    """Transform a raw feature matrix into its Weight-of-Evidence values."""
    out = np.zeros_like(X, dtype=np.float64)
    for j, feat in enumerate(spec["features"]):
        edges = np.array(feat["edges"], dtype=np.float64)
        woe = np.array(feat["woe"], dtype=np.float64)
        idx = np.clip(np.searchsorted(edges, X[:, j], side="right") - 1, 0, len(woe) - 1)
        out[:, j] = woe[idx]
    return out


def fit_woe_scorecard(X: np.ndarray, y: np.ndarray) -> dict:
    """Fit a Weight-of-Evidence scorecard on the same population as the model.

    Each feature is quantile-binned (small bins merged), assigned WoE =
    ln(share_good / share_bad), then the WoE matrix is regressed on the default
    label with an unpenalized logistic fit. Points follow the standard scale
    offset + factor * ln(odds_good_to_bad) with PDO points per doubling.
    """
    n = X.shape[0]
    good_total = int((y == 0).sum())
    bad_total = int((y == 1).sum())
    factor = POINTS_TO_DOUBLE_ODDS / math.log(2)
    offset = SCORE_BASE_POINTS - factor * math.log(SCORE_BASE_ODDS)

    features = []
    W = np.zeros_like(X, dtype=np.float64)

    for j, name in enumerate(FEATURES):
        col = X[:, j]
        edges = list(np.unique(np.quantile(col, np.linspace(0.0, 1.0, WOE_BINS + 1))))
        while len(edges) > 2:
            counts = np.histogram(col, bins=edges)[0]
            thin = [i for i, c in enumerate(counts) if c / n < MIN_BIN_SHARE]
            if not thin:
                break
            del edges[thin[0] + 1]

        counts = np.histogram(col, bins=edges)[0]
        woe_per_bin = []
        iv = 0.0
        for b in range(len(counts)):
            lo, hi = edges[b], edges[b + 1]
            last = b == len(counts) - 1
            mask = (col >= lo) & ((col < hi) | last)
            g = int(((y == 0) & mask).sum())
            bad = int(((y == 1) & mask).sum())
            share_good = (g + 0.5) / (good_total + 0.5)
            share_bad = (bad + 0.5) / (bad_total + 0.5)
            woe = math.log(share_good / share_bad)
            woe_per_bin.append(round(woe, 8))
            iv += (share_good - share_bad) * woe
            W[mask, j] = woe

        features.append(
            {
                "name": name,
                "edges": [round(float(e), 6) for e in edges],
                "woe": woe_per_bin,
                "information_value": round(iv, 6),
            }
        )

    # Default is the positive class, so coefficients come out negative: rising
    # WoE (safer bin) must lower the modelled odds of default.
    lr = LogisticRegression(C=1e6, solver="lbfgs", max_iter=2000).fit(W, y)
    coefficients = [round(float(c), 8) for c in lr.coef_[0]]
    intercept = round(float(lr.intercept_[0]), 8)

    return {
        "feature_order": FEATURES,
        "monotone_directions": MONOTONIC_CONSTRAINTS,
        "scaling": {
            "base_points": SCORE_BASE_POINTS,
            "base_odds": SCORE_BASE_ODDS,
            "points_to_double_odds": POINTS_TO_DOUBLE_ODDS,
            "factor": round(factor, 8),
            "offset": round(offset, 8),
        },
        "log_odds_default": {"intercept": intercept, "coefficients": coefficients},
        "features": features,
        "score_bounds": {"min": 300, "max": 850},
    }


def scorecard_points(X: np.ndarray, spec: dict) -> np.ndarray:
    """Map a raw feature matrix onto the 300-850 scorecard scale."""
    W = woe_matrix(X, spec)
    lin = spec["log_odds_default"]
    log_odds = lin["intercept"] + W @ np.array(lin["coefficients"], dtype=np.float64)
    scaling = spec["scaling"]
    points = scaling["offset"] - scaling["factor"] * log_odds
    return np.clip(points, 300.0, 850.0)


def main() -> None:
    os.makedirs(MODEL_DIR, exist_ok=True)
    rng = np.random.default_rng(SEED)
    X, y = generate_population(N_APPLICANTS, rng)

    base_rate = float(y.mean())
    print(f"Generated {N_APPLICANTS} applicants, observed default rate {base_rate:.4%}")

    X_train, X_holdout, y_train, y_holdout = train_test_split(
        X, y, test_size=0.25, random_state=SEED, stratify=y
    )
    X_fit, X_val, y_fit, y_val = train_test_split(
        X_train, y_train, test_size=0.20, random_state=SEED, stratify=y_train
    )

    train_set = lgb.Dataset(X_fit, y_fit, feature_name=FEATURES, categorical_feature=[])
    val_set = lgb.Dataset(X_val, y_val, feature_name=FEATURES, reference=train_set)

    params = {
        "objective": "binary",
        "boosting_type": "gbdt",
        "learning_rate": 0.08,
        "num_leaves": 31,
        "max_depth": 5,
        "min_data_in_leaf": 200,
        "feature_fraction": 0.9,
        "bagging_fraction": 0.8,
        "bagging_freq": 5,
        "lambda_l2": 1.0,
        "monotone_constraints": MONOTONIC_CONSTRAINTS,
        "monotone_constraints_method": "advanced",
        "verbosity": -1,
        "seed": SEED,
        "num_threads": 4,
    }

    booster = lgb.train(
        params,
        train_set,
        num_boost_round=600,
        valid_sets=[val_set],
        callbacks=[lgb.early_stopping(40, verbose=False), lgb.log_evaluation(0)],
    )
    booster.save_model(MODEL_FILE, num_iteration=booster.best_iteration)
    print(f"Saved booster with {booster.num_trees()} trees to {MODEL_FILE}")

    # Platt scaling on the held-out split: refit sigmoid(a * margin + b).
    margin_holdout = booster.predict(X_holdout, raw_score=True, num_iteration=booster.best_iteration)
    platt = LogisticRegression(C=1e6, solver="lbfgs", max_iter=1000)
    platt.fit(margin_holdout.reshape(-1, 1), y_holdout)
    slope = float(platt.coef_[0][0])
    intercept = float(platt.intercept_[0])
    print(f"Platt calibrator: slope={slope:.6f} intercept={intercept:.6f}")

    pd_holdout = 1.0 / (1.0 + np.exp(-(slope * margin_holdout + intercept)))

    auc = float(roc_auc_score(y_holdout, pd_holdout))
    brier = float(brier_score_loss(y_holdout, pd_holdout))
    fpr, tpr, _ = roc_curve(y_holdout, pd_holdout)
    ks = float(np.max(tpr - fpr))

    # Calibration by predicted-PD decile.
    order = np.argsort(pd_holdout)
    deciles = []
    edges = np.array_split(order, 10)
    for idx, chunk in enumerate(edges, start=1):
        deciles.append(
            {
                "decile": idx,
                "predicted_pd_mean": round(float(pd_holdout[chunk].mean()), 6),
                "observed_default_rate": round(float(y_holdout[chunk].mean()), 6),
                "count": int(len(chunk)),
            }
        )

    scorecard = fit_woe_scorecard(X, y)
    with open(SCORECARD_FILE, "w", encoding="utf-8") as fh:
        json.dump(scorecard, fh, indent=2, sort_keys=True)
        fh.write("\n")

    pts = scorecard_points(X, scorecard)
    score_deciles = [round(float(np.quantile(pts, q)), 1) for q in np.linspace(0.05, 0.95, 19)]
    scorecard_auc = float(roc_auc_score(y, -pts))
    pd_full = 1.0 / (1.0 + np.exp(-(slope * booster.predict(X, raw_score=True, num_iteration=booster.best_iteration) + intercept)))
    print(f"WoE scorecard: standalone AUC {scorecard_auc:.4f}, IV per feature "
          + ", ".join(f"{f['name']}={f['information_value']:.3f}" for f in scorecard["features"]))
    print("Scorecard point percentiles (p05..p95): " + ", ".join(str(s) for s in score_deciles))
    print("PD basis-point percentiles (p05..p95): "
          + ", ".join(str(round(float(np.quantile(pd_full * 10000, q)))) for q in np.linspace(0.05, 0.95, 19)))
    for cut in (200, 400, 750, 1200, 2000, 3500):
        share = float(((pd_full * 10000) < cut).mean())
        print(f"  share with PD < {cut:>5} bps: {share:6.2%}")

    metrics = {
        "trained_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "seed": SEED,
        "n_applicants": N_APPLICANTS,
        "base_default_rate": round(base_rate, 6),
        "best_iteration": int(booster.best_iteration),
        "num_trees": int(booster.num_trees()),
        "auc_holdout": round(auc, 6),
        "ks_statistic_holdout": round(ks, 6),
        "brier_score_holdout": round(brier, 6),
        "woe_scorecard_auc": round(scorecard_auc, 6),
        "woe_scorecard_iv": {f["name"]: f["information_value"] for f in scorecard["features"]},
        "woe_scorecard_percentiles": {
            f"p{int(q * 100):02d}": round(float(np.quantile(pts, q)), 1)
            for q in (0.01, 0.05, 0.25, 0.50, 0.75, 0.95, 0.99)
        },
        "platt_slope": round(slope, 8),
        "platt_intercept": round(intercept, 8),
        "monotone_constraints": dict(zip(FEATURES, MONOTONIC_CONSTRAINTS)),
        "calibration_deciles": deciles,
    }

    with open(METRICS_FILE, "w", encoding="utf-8") as fh:
        json.dump(metrics, fh, indent=2, sort_keys=True)
        fh.write("\n")

    with open(CALIBRATOR_FILE, "w", encoding="utf-8") as fh:
        json.dump({"slope": slope, "intercept": intercept, "features": FEATURES}, fh, indent=2)
        fh.write("\n")

    # Ten equal-count bins of the training PD distribution: the PSI baseline.
    train_pd = 1.0 / (1.0 + np.exp(-(slope * booster.predict(X, raw_score=True, num_iteration=booster.best_iteration) + intercept)))
    bin_edges = np.quantile(train_pd, np.linspace(0, 1, 11))
    bin_edges[0] = 0.0
    bin_edges[-1] = 1.0
    counts, _ = np.histogram(train_pd, bins=bin_edges)
    baseline = {
        "feature_name": "OVERALL_PD",
        "n_samples": int(len(train_pd)),
        "bin_edges": [round(float(e), 8) for e in bin_edges],
        "expected_share": [round(float(c) / len(train_pd), 8) for c in counts],
    }
    with open(BASELINE_FILE, "w", encoding="utf-8") as fh:
        json.dump(baseline, fh, indent=2)
        fh.write("\n")

    digest = hashlib.sha256()
    digest.update(X.tobytes())
    digest.update(y.tobytes())
    with open(DATASET_HASH_FILE, "w", encoding="utf-8") as fh:
        fh.write(digest.hexdigest() + "\n")

    print(f"Holdout AUC {auc:.4f}  KS {ks:.4f}  Brier {brier:.6f}")
    print(f"Dataset sha256 {digest.hexdigest()}")
    print(f"Model file size {os.path.getsize(MODEL_FILE):,} bytes")


if __name__ == "__main__":
    main()
