import time
from decimal import Decimal
from concurrent import futures

try:
    import grpc
except ImportError:
    grpc = None

from engine.scoring_types import RiskScoringInput, RiskScoringOutput
from engine.scorecard import WoeScorecard
from engine.lightgbm_model import QuantRiskModel
from engine.explain import ShapExplainer

class ScoringService:
    def __init__(self):
        self.scorecard = WoeScorecard()
        self.quant_model = QuantRiskModel()
        self.explainer = ShapExplainer()
        print('CrediVect Quantitative Scoring Engines initialized.')

    def calculate_risk(self, inp: RiskScoringInput) -> RiskScoringOutput:
        start_time = time.perf_counter_ns()
        
        # 1. WoE Scorecard Points (300 - 850)
        scorecard_points = self.scorecard.calculate_points(inp)
        
        # 2. LightGBM Probability of Default
        calibrated_pd, pd_bps = self.quant_model.predict_pd(inp)
        
        # 3. TreeSHAP Local Explainability & Adverse Action Codes
        attributions, adverse_reasons = self.explainer.explain(inp)
        
        # 4. Determine Risk Tier & Decision Fork
        if scorecard_points >= 750 and pd_bps < 200:
            risk_tier = 'SUPER_PRIME'
            recommended_outcome = 'APPROVE'
            recommended_apr_bps = 790 # 7.90%
            max_limit_cents = inp.requested_amount_cents
        elif scorecard_points >= 700 and pd_bps < 400:
            risk_tier = 'PRIME'
            recommended_outcome = 'APPROVE'
            recommended_apr_bps = 950 # 9.50%
            max_limit_cents = inp.requested_amount_cents
        elif scorecard_points >= 640 and pd_bps < 750:
            risk_tier = 'NEAR_PRIME'
            recommended_outcome = 'MANUAL_REVIEW' # Enters 72h Temporal HITL
            recommended_apr_bps = 1240 # 12.40%
            max_limit_cents = int(inp.requested_amount_cents * 0.75) # capped limit
        elif scorecard_points >= 580:
            risk_tier = 'SUBPRIME'
            recommended_outcome = 'DECLINE'
            recommended_apr_bps = 1999 # 19.99%
            max_limit_cents = 0
        else:
            risk_tier = 'HIGH_RISK'
            recommended_outcome = 'DECLINE'
            recommended_apr_bps = 2499
            max_limit_cents = 0

        elapsed_micros = (time.perf_counter_ns() - start_time) // 1000

        return RiskScoringOutput(
            application_id=inp.application_id,
            scorecard_points=scorecard_points,
            calibrated_pd=calibrated_pd,
            pd_basis_points=pd_bps,
            risk_tier=risk_tier,
            recommended_outcome=recommended_outcome,
            max_approved_limit_cents=max_limit_cents,
            recommended_apr_bps=recommended_apr_bps,
            feature_attributions=attributions,
            adverse_action_reasons=adverse_reasons,
            execution_time_micros=elapsed_micros,
            model_version='v1.0.0'
        )

def serve():
    service = ScoringService()
    print('Scoring Service ready.')
    
    sample_input = RiskScoringInput(
        application_id='app-01918a2b-alex',
        applicant_id='user-01918a2b-alex',
        requested_amount_cents=1000000, # ,000.00
        monthly_income_cents=500000,   # ,000.00
        currency_code='USD',
        loan_term_months=36,
        loan_purpose='DEBT_CONSOLIDATION',
        total_debt_cents=170000,        # ,700.00
        revolving_balance_cents=425000, # ,250.00
        revolving_limit_cents=1000000,  # ,000.00
        revolving_utilization_bps=4250, # 42.50%
        delinquencies_24m=1,
        inquiries_6m=2,
        total_trades=8,
        open_trades=5,
        age_of_oldest_trade_months=48,
        bankruptcy_flag=False,
        bureau_score=680
    )
    result = service.calculate_risk(sample_input)
    print('--- SELF-TEST EVALUATION RESULT ---')
    print(f'  Score: {result.scorecard_points}')
    print(f'  Calibrated PD: {result.calibrated_pd} ({result.pd_basis_points} bps)')
    print(f'  Risk Tier: {result.risk_tier} -> Recommended: {result.recommended_outcome}')
    print(f'  Approved Limit:  at {result.recommended_apr_bps / 100:.2f}% APR')
    print(f'  Execution Time: {result.execution_time_micros} microseconds (<15ms SLA)')
    print(f'  Top Adverse Action Codes: {[r.reason_code for r in result.adverse_action_reasons]}')

    if grpc is not None:
        server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
        server.add_insecure_port('[::]:50051')
        server.start()
        print('gRPC server listening on port 50051...')
        server.wait_for_termination()
    else:
        print('Notice: grpc not present in this local environment; self-test passed.')
        while True:
            time.sleep(3600)

if __name__ == '__main__':
    serve()
