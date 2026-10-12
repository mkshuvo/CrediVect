package api

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// validRequest is a conforming submission; each case breaks exactly one field so
// the failure is attributable.
func validRequest() *CreateApplicationRequest {
	return &CreateApplicationRequest{
		FirstName: "Alex", LastName: "Chen", Email: "alex.chen@example.com",
		DateOfBirth: "1994-06-15", SSN: "123-45-6789",
		MonthlyIncomeCents: 600_000, RequestedAmountCents: 1_200_000,
		LoanTermMonths: 36, LoanPurpose: "DEBT_CONSOLIDATION",
		EmploymentStatus: "EMPLOYED", ConsentFCRA: true,
	}
}

func TestValidRequestPasses(t *testing.T) {
	if fields := validateApplication(validRequest()); fields != nil {
		t.Fatalf("a conforming application was rejected: %v", fields)
	}
}

func TestFieldBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateApplicationRequest)
		field  string
	}{
		{"short first name", func(r *CreateApplicationRequest) { r.FirstName = "A" }, "first_name"},
		{"numeric first name", func(r *CreateApplicationRequest) { r.FirstName = "Alex2" }, "first_name"},
		{"empty last name", func(r *CreateApplicationRequest) { r.LastName = "  " }, "last_name"},
		{"missing at sign", func(r *CreateApplicationRequest) { r.Email = "alex.example.com" }, "email"},
		{"missing tld", func(r *CreateApplicationRequest) { r.Email = "alex@example" }, "email"},
		{"eight digit ssn", func(r *CreateApplicationRequest) { r.SSN = "12345678" }, "ssn"},
		{"ssn with letters", func(r *CreateApplicationRequest) { r.SSN = "12345678a" }, "ssn"},
		{"unparseable dob", func(r *CreateApplicationRequest) { r.DateOfBirth = "15/06/1994" }, "date_of_birth"},
		{"future dob", func(r *CreateApplicationRequest) { r.DateOfBirth = "2099-01-01" }, "date_of_birth"},
		{"underage borrower", func(r *CreateApplicationRequest) { r.DateOfBirth = yearAgoNow(17) }, "date_of_birth"},
		{"implausible dob", func(r *CreateApplicationRequest) { r.DateOfBirth = "1900-01-01" }, "date_of_birth"},
		{"income below floor", func(r *CreateApplicationRequest) { r.MonthlyIncomeCents = minIncomeCents - 1 }, "monthly_income_cents"},
		{"income above ceiling", func(r *CreateApplicationRequest) { r.MonthlyIncomeCents = maxIncomeCents + 1 }, "monthly_income_cents"},
		{"loan below floor", func(r *CreateApplicationRequest) { r.RequestedAmountCents = minLoanCents - 1 }, "requested_amount_cents"},
		{"loan above ceiling", func(r *CreateApplicationRequest) { r.RequestedAmountCents = maxLoanCents + 1 }, "requested_amount_cents"},
		{"term too short", func(r *CreateApplicationRequest) { r.LoanTermMonths = minLoanTermMonths - 1 }, "loan_term_months"},
		{"term too long", func(r *CreateApplicationRequest) { r.LoanTermMonths = maxLoanTermMonths + 1 }, "loan_term_months"},
		{"unknown purpose", func(r *CreateApplicationRequest) { r.LoanPurpose = "CRYPTO_TRADING" }, "loan_purpose"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validRequest()
			c.mutate(req)
			fields := validateApplication(req)
			if fields == nil {
				t.Fatalf("%s was accepted", c.name)
			}
			if _, ok := fields[c.field]; !ok {
				t.Errorf("expected a complaint about %s, got %v", c.field, fields)
			}
		})
	}
}

func TestBoundaryValuesAreAccepted(t *testing.T) {
	// The CHECK constraints in scripts/init-db.sql use the same inclusive bounds;
	// a request on the edge must pass both.
	for _, req := range []*CreateApplicationRequest{
		withMutation(func(r *CreateApplicationRequest) {
			r.MonthlyIncomeCents = minIncomeCents
			r.RequestedAmountCents = minLoanCents
		}),
		withMutation(func(r *CreateApplicationRequest) {
			r.MonthlyIncomeCents = maxIncomeCents
			r.RequestedAmountCents = maxLoanCents
		}),
		withMutation(func(r *CreateApplicationRequest) { r.LoanTermMonths = minLoanTermMonths }),
		withMutation(func(r *CreateApplicationRequest) { r.LoanTermMonths = maxLoanTermMonths }),
		withMutation(func(r *CreateApplicationRequest) { r.DateOfBirth = yearAgoNow(maxAgeYears) }),
		withMutation(func(r *CreateApplicationRequest) { r.DateOfBirth = yearAgoNow(minAgeYears) }),
	} {
		if fields := validateApplication(req); fields != nil {
			t.Errorf("conforming boundary request rejected: %v", fields)
		}
	}
}

func TestEveryDocumentedPurposeIsValid(t *testing.T) {
	// The wizard's option list and this map must not drift apart.
	for _, purpose := range []string{
		"DEBT_CONSOLIDATION", "MEDICAL", "HOME_IMPROVEMENT", "AUTO", "EDUCATION", "SMALL_BUSINESS", "LARGE_PURCHASE",
	} {
		req := validRequest()
		req.LoanPurpose = purpose
		if fields := validateApplication(req); fields != nil {
			t.Errorf("purpose %s rejected: %v", purpose, fields)
		}
	}
}

func TestConsentIsEnforcedSeparatelyFromShape(t *testing.T) {
	// A well-formed application without FCRA consent is a permissible-purpose
	// problem, not a formatting one, so the wizard must be able to show it apart.
	req := validRequest()
	req.ConsentFCRA = false
	if fields := validateApplication(req); fields != nil {
		t.Fatalf("validateApplication should not own the consent rule, got %v", fields)
	}
}

func TestNamesWithSpacesHyphensAndApostrophes(t *testing.T) {
	for _, name := range []string{"Mary Jane", "O'Brien", "Smith-Jones", "De La Cruz"} {
		req := validRequest()
		req.FirstName = name
		if fields := validateApplication(req); fields != nil {
			t.Errorf("name %q rejected: %v", name, fields)
		}
	}
}

func TestAgeOfExactlyEighteenPasses(t *testing.T) {
	req := validRequest()
	req.DateOfBirth = yearAgoNow(18)
	if fields := validateApplication(req); fields != nil {
		t.Errorf("an 18 year old was rejected: %v", fields)
	}
}

func TestMessagesQuoteTheBoundsTheyEnforce(t *testing.T) {
	// A message that disagrees with its own constant is how the income ceiling
	// drifted to a tenth of the documented limit undetected: the text said
	// $500,000 while the constant held $50,000.
	cases := []struct {
		field string
		req   *CreateApplicationRequest
		cents int64
	}{
		{"monthly_income_cents", withMutation(func(r *CreateApplicationRequest) { r.MonthlyIncomeCents = minIncomeCents - 1 }), minIncomeCents},
		{"monthly_income_cents", withMutation(func(r *CreateApplicationRequest) { r.MonthlyIncomeCents = maxIncomeCents + 1 }), maxIncomeCents},
		{"requested_amount_cents", withMutation(func(r *CreateApplicationRequest) { r.RequestedAmountCents = minLoanCents - 1 }), minLoanCents},
		{"requested_amount_cents", withMutation(func(r *CreateApplicationRequest) { r.RequestedAmountCents = maxLoanCents + 1 }), maxLoanCents},
	}
	for _, c := range cases {
		fields := validateApplication(c.req)
		msg, ok := fields[c.field]
		if !ok {
			t.Fatalf("%s was not reported", c.field)
		}
		if !strings.Contains(msg, strconv.FormatInt(c.cents, 10)) {
			t.Errorf("%s message %q does not quote %d cents", c.field, msg, c.cents)
		}
	}
}

func TestAPIBoundsFitTheDatabaseChecks(t *testing.T) {
	// Mirrors scripts/init-db.sql. The API may be stricter than the column CHECK,
	// never looser: a request that passes here must be insertable.
	const (
		dbMinIncomeCents = 50_000
		dbMaxIncomeCents = 50_000_000
		dbMinLoanCents   = 100_000
		dbMaxLoanCents   = 50_000_000
		dbMinTermMonths  = 6
		dbMaxTermMonths  = 84
	)
	if minIncomeCents < dbMinIncomeCents || maxIncomeCents > dbMaxIncomeCents {
		t.Errorf("income bounds [%d,%d] escape the DB CHECK [%d,%d]", minIncomeCents, maxIncomeCents, dbMinIncomeCents, dbMaxIncomeCents)
	}
	if minLoanCents < dbMinLoanCents || maxLoanCents > dbMaxLoanCents {
		t.Errorf("loan bounds [%d,%d] escape the DB CHECK [%d,%d]", minLoanCents, maxLoanCents, dbMinLoanCents, dbMaxLoanCents)
	}
	if minLoanTermMonths < dbMinTermMonths || maxLoanTermMonths > dbMaxTermMonths {
		t.Errorf("term bounds [%d,%d] escape the DB CHECK [%d,%d]", minLoanTermMonths, maxLoanTermMonths, dbMinTermMonths, dbMaxTermMonths)
	}

	// applicants.first_name/last_name are VARCHAR(100) and the name regex admits at
	// most 100 runes, so a maximal accepted name still fits the column.
	long := "A" + strings.Repeat("b", 99)
	req := validRequest()
	req.FirstName = long
	if fields := validateApplication(req); fields != nil {
		t.Errorf("100 rune name rejected: %v", fields)
	}
	req.FirstName = "A" + strings.Repeat("b", 100)
	if fields := validateApplication(req); fields == nil {
		t.Error("101 rune name accepted: it would not fit VARCHAR(100)")
	}
}

func withMutation(mutate func(*CreateApplicationRequest)) *CreateApplicationRequest {
	req := validRequest()
	mutate(req)
	return req
}

func yearAgoNow(age int) string {
	// A computed date keeps the age cases correct as the suite ages. The extra day
	// back guarantees the borrower is at least `age`, not a day short of it.
	return time.Now().UTC().AddDate(-age, 0, -1).Format("2006-01-02")
}
