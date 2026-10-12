"""gRPC servicer exposing the RiskEngine over proto/credivect/scoring/v1.

Validates the request boundary and translates engine failures into gRPC status
codes; it never returns a partially scored response.
"""

from __future__ import annotations

import logging

import grpc

from engine.risk import RiskEngine
from engine.scoring_types import RiskScoringInput
from credivect.scoring.v1 import scoring_pb2, scoring_pb2_grpc

logger = logging.getLogger("credivect.scoring")

MAX_BPS = 100_000


class ScoringServicer(scoring_pb2_grpc.ScoringServiceServicer):
    def __init__(self, engine: RiskEngine) -> None:
        self.engine = engine

    def _validate(self, request) -> str | None:
        if not request.application_id:
            return "application_id is required"
        if request.monthly_income_cents <= 0:
            return "monthly_income_cents must be greater than zero"
        if request.requested_amount_cents <= 0:
            return "requested_amount_cents must be greater than zero"
        if request.loan_term_months <= 0:
            return "loan_term_months must be greater than zero"
        if not 300 <= request.bureau_score <= 850:
            return "bureau_score must be between 300 and 850"
        if request.revolving_utilization_bps < 0 or request.revolving_utilization_bps > MAX_BPS:
            return "revolving_utilization_bps must be between 0 and 100000 basis points"
        # Each message names exactly one field: a caller that gets INVALID_ARGUMENT
        # back has to be able to tell which value to fix.
        for field in (
            "total_debt_cents",
            "revolving_balance_cents",
            "revolving_limit_cents",
            "delinquencies_24m",
            "inquiries_6m",
            "total_trades",
            "open_trades",
            "age_of_oldest_trade_months",
        ):
            if getattr(request, field) < 0:
                return f"{field} cannot be negative"
        return None

    def CalculateRisk(self, request, context):
        problem = self._validate(request)
        if problem is not None:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, problem)

        inp = RiskScoringInput(
            application_id=request.application_id,
            applicant_id=request.applicant_id,
            requested_amount_cents=int(request.requested_amount_cents),
            monthly_income_cents=int(request.monthly_income_cents),
            currency_code=request.currency_code or "USD",
            loan_term_months=int(request.loan_term_months),
            loan_purpose=request.loan_purpose,
            total_debt_cents=int(request.total_debt_cents),
            revolving_balance_cents=int(request.revolving_balance_cents),
            revolving_limit_cents=int(request.revolving_limit_cents),
            revolving_utilization_bps=int(request.revolving_utilization_bps),
            delinquencies_24m=int(request.delinquencies_24m),
            inquiries_6m=int(request.inquiries_6m),
            total_trades=int(request.total_trades),
            open_trades=int(request.open_trades),
            age_of_oldest_trade_months=int(request.age_of_oldest_trade_months),
            bankruptcy_flag=bool(request.bankruptcy_flag),
            bureau_score=int(request.bureau_score),
        )

        try:
            result = self.engine.score(inp)
        except Exception:  # noqa: BLE001 - surfaced to the caller as UNAVAILABLE
            logger.exception("scoring failed for application %s", request.application_id)
            context.abort(grpc.StatusCode.INTERNAL, "internal scoring error")

        response = scoring_pb2.CalculateRiskResponse(
            application_id=result.application_id,
            scorecard_points=result.scorecard_points,
            calibrated_pd=str(result.calibrated_pd),
            pd_basis_points=result.pd_basis_points,
            risk_tier=result.risk_tier,
            recommended_outcome=result.recommended_outcome,
            max_approved_limit_cents=result.max_approved_limit_cents,
            recommended_apr_bps=result.recommended_apr_bps,
            execution_time_micros=result.execution_time_micros,
            model_version=result.model_version,
            raw_logit=result.raw_logit,
        )
        for attribution in result.feature_attributions:
            response.feature_attributions.add(
                feature_name=attribution.feature_name,
                feature_value=attribution.feature_value,
                shap_value=attribution.shap_value,
                rank=attribution.rank,
            )
        for reason in result.adverse_action_reasons:
            response.adverse_action_reasons.add(
                reason_code=reason.reason_code,
                description=reason.description,
                priority_rank=reason.priority_rank,
            )
        return response

    def HealthCheck(self, request, context):
        return scoring_pb2.HealthCheckResponse(status="SERVING", version=self.engine.model_version)
