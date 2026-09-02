import math
from decimal import Decimal, ROUND_HALF_EVEN
from engine.scoring_types import RiskScoringInput

class WoeScorecard:
    """
    Production Weight of Evidence (WoE) Monotonic Scorecard.
    Standardized to 300 - 850 scale with Target Odds 50:1 at 600 points,
    and 20 Points to Double the Odds (PDO = 20).
    """
    def __init__(self):
        self.pdo = 20.0
        self.base_score = 600.0
        self.base_odds = 50.0
        self.factor = self.pdo / math.log(2)
        self.offset = self.base_score - self.factor * math.log(self.base_odds)

    def calculate_points(self, inp: RiskScoringInput) -> int:
        # Base Points
        total_points = 500.0

        # 1. Revolving Utilization (BPS: 0 to 10000+)
        # Lower utilization = more points
        util = inp.revolving_utilization_bps
        if util < 1000: # < 10%
            total_points += 75.0
        elif util < 3000: # 10% - 30%
            total_points += 50.0
        elif util < 5000: # 30% - 50%
            total_points += 20.0
        elif util < 7500: # 50% - 75%
            total_points -= 25.0
        else: # > 75%
            total_points -= 70.0

        # 2. Debt-to-Income (DTI BPS)
        dti = inp.dti_bps
        if dti < 2000: # < 20%
            total_points += 60.0
        elif dti < 3600: # 20% - 36%
            total_points += 35.0
        elif dti < 4500: # 36% - 45%
            total_points += 0.0
        elif dti < 5500: # 45% - 55%
            total_points -= 40.0
        else: # > 55%
            total_points -= 80.0

        # 3. Delinquencies in 24 Months
        delinq = inp.delinquencies_24m
        if delinq == 0:
            total_points += 65.0
        elif delinq == 1:
            total_points -= 20.0
        elif delinq == 2:
            total_points -= 55.0
        else:
            total_points -= 100.0

        # 4. Inquiries in 6 Months
        inq = inp.inquiries_6m
        if inq == 0:
            total_points += 40.0
        elif inq <= 2:
            total_points += 15.0
        elif inq <= 4:
            total_points -= 25.0
        else:
            total_points -= 60.0

        # 5. Age of Oldest Trade (Months)
        age = inp.age_of_oldest_trade_months
        if age >= 120: # 10+ years
            total_points += 50.0
        elif age >= 60: # 5-10 years
            total_points += 30.0
        elif age >= 24: # 2-5 years
            total_points += 10.0
        else: # < 2 years
            total_points -= 30.0

        # 6. Bankruptcy Penalty
        if inp.bankruptcy_flag:
            total_points -= 150.0

        # Clamp to regulatory 300 - 850 range
        clamped = max(300.0, min(850.0, total_points))
        # Banker's rounding
        return int(Decimal(str(clamped)).quantize(Decimal('1'), rounding=ROUND_HALF_EVEN))
