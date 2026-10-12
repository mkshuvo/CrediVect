package api

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	emailRe   = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)
	ssnRe     = regexp.MustCompile(`^[0-9]{9}$`)
	nameRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z .'\-]{0,99}$`)
	purposeOK = map[string]bool{
		"DEBT_CONSOLIDATION": true, "MEDICAL": true, "HOME_IMPROVEMENT": true,
		"AUTO": true, "EDUCATION": true, "SMALL_BUSINESS": true, "LARGE_PURCHASE": true,
	}
)

const (
	// These are the product contract for a self-serve application. The column
	// CHECKs in scripts/init-db.sql must stay at least as wide, otherwise a
	// request that passes here fails on insert.
	minLoanCents      = 100_000
	maxLoanCents      = 5_000_000
	minIncomeCents    = 50_000
	maxIncomeCents    = 50_000_000
	minLoanTermMonths = 6
	maxLoanTermMonths = 84
	minAgeYears       = 18
	maxAgeYears       = 100
)

// validateApplication enforces the request boundary. Every rule here maps to a
// column CHECK in scripts/init-db.sql, so a request that passes validation can
// not fail on insert for the same reason.
func validateApplication(req *CreateApplicationRequest) map[string]string {
	fields := map[string]string{}
	switch {
	case utf8.RuneCountInString(strings.TrimSpace(req.FirstName)) < 2:
		fields["first_name"] = "must be at least 2 characters"
	case !nameRe.MatchString(strings.TrimSpace(req.FirstName)):
		fields["first_name"] = "may contain only letters, spaces, apostrophes and hyphens"
	}
	switch {
	case utf8.RuneCountInString(strings.TrimSpace(req.LastName)) < 2:
		fields["last_name"] = "must be at least 2 characters"
	case !nameRe.MatchString(strings.TrimSpace(req.LastName)):
		fields["last_name"] = "may contain only letters, spaces, apostrophes and hyphens"
	}
	if !emailRe.MatchString(strings.TrimSpace(req.Email)) {
		fields["email"] = "must be a valid email address"
	}
	if !ssnRe.MatchString(onlyDigits(req.SSN)) {
		fields["ssn"] = "must be 9 digits"
	}
	age, err := ageYears(req.DateOfBirth)
	if err != nil {
		fields["date_of_birth"] = "must be YYYY-MM-DD"
	} else if age < minAgeYears {
		fields["date_of_birth"] = fmt.Sprintf("borrower must be at least %d years old", minAgeYears)
	} else if age > maxAgeYears {
		fields["date_of_birth"] = "date of birth is not plausible"
	}
	switch {
	case req.MonthlyIncomeCents < minIncomeCents:
		fields["monthly_income_cents"] = "must be at least 50000 cents ($500.00)"
	case req.MonthlyIncomeCents > maxIncomeCents:
		fields["monthly_income_cents"] = "must not exceed 50000000 cents ($500,000.00)"
	}
	switch {
	case req.RequestedAmountCents < minLoanCents:
		fields["requested_amount_cents"] = "must be at least 100000 cents ($1,000.00)"
	case req.RequestedAmountCents > maxLoanCents:
		fields["requested_amount_cents"] = "must not exceed 5000000 cents ($50,000.00)"
	}
	if req.LoanTermMonths < minLoanTermMonths || req.LoanTermMonths > maxLoanTermMonths {
		fields["loan_term_months"] = "must be between 6 and 84 months"
	}
	if !purposeOK[strings.ToUpper(strings.TrimSpace(req.LoanPurpose))] {
		fields["loan_purpose"] = "must be one of the supported loan purposes"
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}

func ageYears(dob string) (int, error) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(dob))
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	if t.After(now) {
		return 0, fmt.Errorf("date of birth is in the future")
	}
	age := now.Year() - t.Year()
	if now.YearDay() < t.YearDay() {
		age--
	}
	return age, nil
}

func onlyDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			out = append(out, s[i])
		}
	}
	return string(out)
}
