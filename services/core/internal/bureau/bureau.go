package bureau

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"credivect/core/internal/domain"
)

// Request is everything a credit bureau pull needs. The blind index, not the
// SSN, identifies the consumer file.
type Request struct {
	ApplicationID        string
	ApplicantID          string
	SSNBlindIndex        string
	FirstName            string
	LastName             string
	DateOfBirth          string
	MonthlyIncomeCents   int64
	RequestedAmountCents int64
}

// Provider is the pluggable credit bureau boundary.
type Provider interface {
	Fetch(ctx context.Context, req Request) (domain.BureauReport, error)
	Name() string
}

// DeterministicProvider replays a reproducible consumer file derived from the
// SSN blind index. CrediVect has no contract with a consumer reporting agency,
// so this stand-in is what the durable saga wraps: it honours context
// cancellation, applies configurable latency and a configurable failure rate,
// and gives identical input an identical file so a retried activity is
// idempotent.
type DeterministicProvider struct {
	latency    time.Duration
	failRate   float64
	bureauName string
}

func NewDeterministicProvider() *DeterministicProvider {
	return &DeterministicProvider{
		latency:    envDuration("BUREAU_LATENCY_MS", 0),
		failRate:   envFloat("BUREAU_FAILURE_RATE", 0),
		bureauName: envString("BUREAU_NAME", "CREDIVECT-SIM"),
	}
}

func (p *DeterministicProvider) Name() string { return p.bureauName }

func (p *DeterministicProvider) Fetch(ctx context.Context, req Request) (domain.BureauReport, error) {
	if req.SSNBlindIndex == "" {
		return domain.BureauReport{}, fmt.Errorf("bureau pull requires an ssn blind index")
	}
	if p.latency > 0 {
		select {
		case <-ctx.Done():
			return domain.BureauReport{}, ctx.Err()
		case <-time.After(p.latency):
		}
	}
	rng := rand.New(rand.NewSource(seedFor(req.SSNBlindIndex)))
	if p.failRate > 0 && rng.Float64() < p.failRate {
		return domain.BureauReport{}, fmt.Errorf("bureau %s unavailable: simulated upstream timeout", p.bureauName)
	}

	// The five scored features are drawn from the same marginals the model was
	// trained on (services/scoring/engine/train.py generate_population), and
	// they are drawn independently for the same reason: the training population
	// is independent, so matching it feature-by-feature is what lets the
	// governance PSI report genuine drift instead of the simulator's own
	// correlation structure. The bureau headline score below is the one field
	// derived from the file, so the reported file still reads coherently.
	normal := rng.NormFloat64

	dtiBps := clamp64(int64(3400+1300*normal()), 0, 9000)
	utilBps := clamp64(int64(3140+1840*normal()), 0, 10000)
	// Affordability improves with income and weakens on a thin file.
	if req.MonthlyIncomeCents >= 1_200_00 {
		dtiBps = clamp64(dtiBps-600, 0, 9000)
	} else if req.MonthlyIncomeCents < 300_00 {
		dtiBps = clamp64(dtiBps+500, 0, 9000)
	}

	ageOldest := clamp64(int64(gammaSample(rng, 2.2, 42.0*math.Exp(-0.45*normal()))), 3, 480)
	thin := ageOldest < 24

	lambdaDelinq := 0.5 * math.Exp(1.0*normal())
	delinquencies := poissonSample(rng, math.Min(lambdaDelinq, 6.0))
	inquiries := poissonSample(rng, math.Min(1.2*math.Exp(0.7*normal()), 8.0))

	revLimit := 200_000 + int64(rng.Float64()*2_800_000) // $2,000 - $30,000 of revolving credit
	revBalance := revLimit * utilBps / 10000

	totalTrades := int32(2 + rng.Intn(18))
	if thin {
		// A file younger than two years cannot have accumulated much history.
		totalTrades = int32(1 + rng.Intn(5))
	}
	openTrades := int32(1 + rng.Intn(int(max32(totalTrades, 1))))
	if openTrades > totalTrades {
		openTrades = totalTrades
	}

	bankruptcy := delinquencies >= 4 && rng.Intn(100) < 20

	// The bureau score is a headline number the borrower and the underwriter both
	// read, so it is derived from the file rather than drawn independently.
	badness := (float64(dtiBps)/9000.0 + float64(utilBps)/10000.0 +
		float64(delinquencies)/5.0 + float64(inquiries)/7.0 +
		(1.0 - float64(ageOldest)/480.0)) / 5.0
	if bankruptcy {
		badness = math.Max(badness, 0.92)
	}
	score := clamp32(int32(845-545*badness), 300, 850)

	// total_debt_cents is the monthly obligation flow on the file, which is what
	// the dti_bps feature divides by monthly income; it is not a balance sheet.
	totalDebt := roundCents(req.MonthlyIncomeCents * dtiBps / 10000)

	now := time.Now().UTC()
	return domain.BureauReport{
		BureauName:              p.bureauName,
		BureauScore:             score,
		TotalDebtCents:          totalDebt,
		RevolvingBalanceCents:   revBalance,
		RevolvingLimitCents:     revLimit,
		RevolvingUtilizationBps: int32(utilBps),
		Delinquencies24m:        delinquencies,
		Inquiries6m:             inquiries,
		TotalTrades:             totalTrades,
		OpenTrades:              openTrades,
		BankruptcyFlag:          bankruptcy,
		AgeOfOldestTradeMonths:  int32(ageOldest),
		ValidTime:               now,
		TransactionTime:         now,
	}, nil
}

func seedFor(blindIndex string) int64 {
	sum := sha256.Sum256([]byte(blindIndex))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

func roundCents(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}

func clamp32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

func envString(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return fallback
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 1 {
		return fallback
	}
	return f
}

func clamp64(v, lo, hi int64) int64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// poissonSample is Knuth's sampler; every rate here is under 8, so the loop is
// short and exact.
func poissonSample(rng *rand.Rand, lambda float64) int32 {
	if lambda <= 0 {
		return 0
	}
	limit := math.Exp(-lambda)
	k, p := 0, rng.Float64()
	for p > limit {
		k++
		p *= rng.Float64()
	}
	return int32(k)
}

// gammaSample uses Marsaglia and Tsang's shape-conditional method for shape >= 1.
func gammaSample(rng *rand.Rand, shape, scale float64) float64 {
	d := shape - 1.0/3.0
	c := 1.0 / math.Sqrt(9.0*d)
	for {
		x := rng.NormFloat64()
		v := math.Pow(1.0+c*x, 3.0)
		if v <= 0 {
			continue
		}
		u := rng.Float64()
		if u < 1.0-0.0331*x*x*x*x {
			return scale * d * v
		}
		if math.Log(u) < 0.5*x*x+d*(1.0-v+math.Log(v)) {
			return scale * d * v
		}
	}
}
