package bureau

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"credivect/core/internal/domain"
)

func provider() *DeterministicProvider {
	return &DeterministicProvider{latency: 0, failRate: 0, bureauName: "TEST-BUREAU"}
}

func pull(t *testing.T, blindIndex string, income int64) domain.BureauReport {
	t.Helper()
	report, err := provider().Fetch(context.Background(), Request{
		ApplicationID: "app-1", ApplicantID: "user-1", SSNBlindIndex: blindIndex,
		FirstName: "Test", LastName: "Borrower", DateOfBirth: "1990-01-01",
		MonthlyIncomeCents: income, RequestedAmountCents: 1_000_000,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return report
}

// scoredFields projects only the file contents the model reads, so the
// determinism test is not upset by the wall-clock timestamp the provider stamps
// on every pull.
func scoredFields(r domain.BureauReport) [10]int64 {
	return [10]int64{
		int64(r.BureauScore), r.TotalDebtCents, r.RevolvingBalanceCents, r.RevolvingLimitCents,
		int64(r.RevolvingUtilizationBps), int64(r.Delinquencies24m), int64(r.Inquiries6m),
		int64(r.TotalTrades), int64(r.OpenTrades), int64(r.AgeOfOldestTradeMonths),
	}
}

func TestFetchRequiresABlindIndex(t *testing.T) {
	if _, err := provider().Fetch(context.Background(), Request{}); err == nil {
		t.Fatal("a pull without a blind index must be rejected; the index is the only consumer key we hold")
	}
}

func TestFetchIsDeterministicPerConsumer(t *testing.T) {
	// The saga retries the bureau activity with backoff. A retry that produced a
	// different credit file would make a scoring run irreproducible.
	first := pull(t, "aa11", 600_000)
	second := pull(t, "aa11", 600_000)
	if scoredFields(first) != scoredFields(second) {
		t.Fatalf("same blind index produced different files:\n %+v\n %+v", first, second)
	}
	if first.BureauName != "TEST-BUREAU" {
		t.Errorf("report carries bureau %q, want the configured name", first.BureauName)
	}
}

func TestFetchSpreadsTheBookAcrossScoreBands(t *testing.T) {
	// A simulator that always returns the same shape is what made the whole
	// seeded book read as HIGH_RISK. A realistic file mix is a product
	// requirement for the queue and for the drift measure.
	seen := map[string]int{}
	for i := 0; i < 400; i++ {
		r := pull(t, "spread-"+strconv.Itoa(i), 500_00+int64(i)*1_000)
		seen[band(r.BureauScore)]++

		if r.BureauScore < 300 || r.BureauScore > 850 {
			t.Fatalf("bureau score %d outside 300-850", r.BureauScore)
		}
		if r.RevolvingUtilizationBps < 0 || r.RevolvingUtilizationBps > 10000 {
			t.Fatalf("utilization %d bps outside 0-10000", r.RevolvingUtilizationBps)
		}
		if r.AgeOfOldestTradeMonths < 3 || r.AgeOfOldestTradeMonths > 480 {
			t.Fatalf("oldest trade %d months outside 3-480", r.AgeOfOldestTradeMonths)
		}
		if r.TotalTrades < 1 || r.OpenTrades < 1 || r.OpenTrades > r.TotalTrades {
			t.Fatalf("trade counts impossible: total %d open %d", r.TotalTrades, r.OpenTrades)
		}
		if r.RevolvingLimitCents <= 0 || r.RevolvingBalanceCents < 0 {
			t.Fatalf("revolving fields impossible: %+v", r)
		}
	}
	if len(seen) < 4 {
		t.Errorf("400 files produced only %d score bands: %+v", len(seen), seen)
	}
	t.Logf("score band spread over 400 files: %+v", seen)
}

func TestTotalDebtIsAMonthlyObligationNotABalance(t *testing.T) {
	// The model's dti feature divides total_debt_cents by monthly income, so the
	// bureau field has to be the monthly obligation flow. Reading it as a balance
	// produced 800% DTI and declined every application in the book.
	for i := 0; i < 300; i++ {
		r := pull(t, "dti-"+strconv.Itoa(i), 600_00)
		dtiBps := r.TotalDebtCents * 10000 / 600_00
		if dtiBps > 9000 {
			t.Fatalf("file %d implies a %d bps DTI; total_debt_cents must stay a monthly flow", i, dtiBps)
		}
	}
}

func TestUtilizationMatchesBalancesAndLimits(t *testing.T) {
	for i := 0; i < 300; i++ {
		r := pull(t, "util-"+strconv.Itoa(i), 500_00)
		implied := r.RevolvingBalanceCents * 10000 / r.RevolvingLimitCents
		if math.Abs(float64(implied-int64(r.RevolvingUtilizationBps))) > 2 {
			t.Errorf("utilization field %d disagrees with balance/limit %d", r.RevolvingUtilizationBps, implied)
		}
	}
}

func TestThinFilesLookThinAndBankruptcyLooksDerelict(t *testing.T) {
	bankruptcies := 0
	for i := 0; i < 800; i++ {
		r := pull(t, "thin-"+strconv.Itoa(i), 400_00)
		if r.AgeOfOldestTradeMonths < 24 && r.TotalTrades > 5 {
			t.Errorf("a %d month file reported %d trades", r.AgeOfOldestTradeMonths, r.TotalTrades)
		}
		if r.BankruptcyFlag {
			bankruptcies++
			if r.Delinquencies24m < 4 {
				t.Errorf("bankruptcy reported without a delinquency history: %+v", r)
			}
			if r.BureauScore > 600 {
				t.Errorf("bankruptcy reported alongside a %d score", r.BureauScore)
			}
		}
	}
	if bankruptcies == 0 {
		t.Error("800 draws produced no bankruptcy; the knockout path would never be exercised")
	}
}

func TestSimulatedFailureRateIsBoundedAndHonest(t *testing.T) {
	p := &DeterministicProvider{latency: 0, failRate: 0.5, bureauName: "TEST-BUREAU"}
	failures := 0
	for i := 0; i < 300; i++ {
		if _, err := p.Fetch(context.Background(), Request{SSNBlindIndex: "flaky-" + strconv.Itoa(i)}); err != nil {
			failures++
		}
	}
	if failures < 100 || failures > 200 {
		t.Errorf("failRate 0.5 produced %d/300 failures", failures)
	}
}

func TestFetchHonoursContextCancellation(t *testing.T) {
	p := &DeterministicProvider{latency: time.Second, failRate: 0, bureauName: "TEST-BUREAU"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Fetch(ctx, Request{SSNBlindIndex: "slow"}); !errors.Is(err, context.Canceled) {
		t.Errorf("Fetch ignored cancellation, got %v", err)
	}
}

func band(score int32) string {
	switch {
	case score >= 720:
		return "super"
	case score >= 670:
		return "prime"
	case score >= 610:
		return "near"
	case score >= 520:
		return "sub"
	default:
		return "risk"
	}
}
