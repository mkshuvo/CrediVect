package workflow

import (
	"encoding/json"
	"math"

	"credivect/core/internal/domain"
)

// activities is a nil-typed method locator: Temporal resolves activity names
// from the method value without invoking it, so the workflow can reference
// methods without holding a live dependency graph.
var activities *Activities

// bureauPayloadJSON stores the raw bureau response in the bitemporal snapshot
// so a scoring run can always be reproduced from the file the engine saw.
func bureauPayloadJSON(b domain.BureauReport) string {
	v := map[string]any{
		"bureau_name":                b.BureauName,
		"bureau_score":               b.BureauScore,
		"total_debt_cents":           b.TotalDebtCents,
		"revolving_balance_cents":    b.RevolvingBalanceCents,
		"revolving_limit_cents":      b.RevolvingLimitCents,
		"revolving_utilization_bps":  b.RevolvingUtilizationBps,
		"delinquencies_24m":          b.Delinquencies24m,
		"inquiries_6m":               b.Inquiries6m,
		"total_trades":               b.TotalTrades,
		"open_trades":                b.OpenTrades,
		"bankruptcy_flag":            b.BankruptcyFlag,
		"age_of_oldest_trade_months": b.AgeOfOldestTradeMonths,
		"valid_time":                 b.ValidTime,
	}
	blob, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(blob)
}

func roundHalfEven(v float64) float64 {
	return math.RoundToEven(v)
}
