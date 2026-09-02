import unittest
from decimal import Decimal
from engine.scoring_types import RiskScoringInput
from engine.scorecard import WoeScorecard
from engine.lightgbm_model import QuantRiskModel
from engine.explain import ShapExplainer
from server import ScoringService

class TestCrediVectScoring(unittest.TestCase):
    def setUp(self):
        self.service = ScoringService()

    def test_prime_borrower_approved(self):
        prime_input = RiskScoringInput(
            application_id="app-test-prime",
            applicant_id="user-test-prime",
            requested_amount_cents=1500000, # $15,000
            monthly_income_cents=1200000,  # $12,000
            currency_code="USD",
            loan_term_months=36,
            loan_purpose="HOME_IMPROVEMENT",
            total_debt_cents=150000,
            revolving_balance_cents=100000,
            revolving_limit_cents=2000000,
            revolving_utilization_bps=500,  # 5.0%
            delinquencies_24m=0,
            inquiries_6m=0,
            total_trades=12,
            open_trades=8,
            age_of_oldest_trade_months=120,
            bankruptcy_flag=False,
            bureau_score=785
        )
        res = self.service.calculate_risk(prime_input)
        self.assertGreaterEqual(res.scorecard_points, 720)
        self.assertIn(res.risk_tier, ["SUPER_PRIME", "PRIME"])
        self.assertEqual(res.recommended_outcome, "APPROVE")
        self.assertLess(res.pd_basis_points, 400) # < 4.0% default probability

    def test_subprime_borrower_declined_with_adverse_codes(self):
        risky_input = RiskScoringInput(
            application_id="app-test-risky",
            applicant_id="user-test-risky",
            requested_amount_cents=3000000, # $30,000
            monthly_income_cents=200000,   # $2,000
            currency_code="USD",
            loan_term_months=60,
            loan_purpose="DEBT_CONSOLIDATION",
            total_debt_cents=150000,
            revolving_balance_cents=950000,
            revolving_limit_cents=1000000,
            revolving_utilization_bps=9500, # 95.0% utilization
            delinquencies_24m=3,
            inquiries_6m=6,
            total_trades=4,
            open_trades=3,
            age_of_oldest_trade_months=12,
            bankruptcy_flag=True,
            bureau_score=520
        )
        res = self.service.calculate_risk(risky_input)
        self.assertLess(res.scorecard_points, 600)
        self.assertEqual(res.recommended_outcome, "DECLINE")
        self.assertGreater(len(res.adverse_action_reasons), 0)
        # Verify reason codes exist
        codes = [r.reason_code for r in res.adverse_action_reasons]
        self.assertTrue("AA01" in codes or "AA02" in codes or "AA03" in codes)

    def test_zero_float_invariants(self):
        sample = RiskScoringInput(
            application_id="app-zero-float",
            applicant_id="user-zero-float",
            requested_amount_cents=1000000,
            monthly_income_cents=500000,
            currency_code="USD",
            loan_term_months=24,
            loan_purpose="PERSONAL",
            total_debt_cents=170000,
            revolving_balance_cents=425000,
            revolving_limit_cents=1000000,
            revolving_utilization_bps=4250,
            delinquencies_24m=1,
            inquiries_6m=1,
            total_trades=6,
            open_trades=4,
            age_of_oldest_trade_months=36,
            bankruptcy_flag=False,
            bureau_score=670
        )
        res = self.service.calculate_risk(sample)
        self.assertIsInstance(res.scorecard_points, int)
        self.assertIsInstance(res.pd_basis_points, int)
        self.assertIsInstance(res.calibrated_pd, Decimal)
        self.assertIsInstance(res.max_approved_limit_cents, int)
        self.assertIsInstance(res.recommended_apr_bps, int)

if __name__ == "__main__":
    unittest.main()
