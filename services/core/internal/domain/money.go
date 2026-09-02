package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Money represents a monetary amount strictly stored in integer minor units (e.g. Cents for USD).
// Binary floating point numbers (float32/float64) are strictly prohibited in financial code.
type Money struct {
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"` // ISO-4217 code (e.g. "USD")
}

func NewMoney(cents int64, currency string) Money {
	if currency == "" {
		currency = "USD"
	}
	return Money{
		Cents:    cents,
		Currency: strings.ToUpper(currency),
	}
}

func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: %s vs %s", m.Currency, other.Currency)
	}
	return Money{Cents: m.Cents + other.Cents, Currency: m.Currency}, nil
}

func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: %s vs %s", m.Currency, other.Currency)
	}
	return Money{Cents: m.Cents - other.Cents, Currency: m.Currency}, nil
}

// MulInt multiplies the money by an integer factor
func (m Money) MulInt(factor int64) Money {
	return Money{Cents: m.Cents * factor, Currency: m.Currency}
}

// DivIntWithBankersRounding divides cents by a divisor applying Banker's Rounding (ROUND_HALF_EVEN)
func (m Money) DivIntWithBankersRounding(divisor int64) (Money, error) {
	if divisor == 0 {
		return Money{}, fmt.Errorf("division by zero")
	}
	
	quotient := m.Cents / divisor
	remainder := m.Cents % divisor
	absRemainder := remainder
	if absRemainder < 0 {
		absRemainder = -absRemainder
	}
	absDivisor := divisor
	if absDivisor < 0 {
		absDivisor = -absDivisor
	}

	// If remainder is exactly half the divisor, apply Banker's rounding to even integer
	if 2*absRemainder == absDivisor {
		if quotient%2 != 0 {
			if m.Cents >= 0 {
				quotient++
			} else {
				quotient--
			}
		}
	} else if 2*absRemainder > absDivisor {
		if m.Cents >= 0 {
			quotient++
		} else {
			quotient--
		}
	}

	return Money{Cents: quotient, Currency: m.Currency}, nil
}

// FormatDollars returns human-readable formatted dollars, e.g. "$15,250.75"
func (m Money) FormatDollars() string {
	dollars := float64(m.Cents) / 100.0
	return fmt.Sprintf("$%.2f", dollars)
}

// ParseDollarsToCents parses "$1,250.50" or "1250.50" into integer cents (125050)
func ParseDollarsToCents(s string) (int64, error) {
	cleaned := strings.ReplaceAll(s, "$", "")
	cleaned = strings.ReplaceAll(cleaned, ",", "")
	cleaned = strings.TrimSpace(cleaned)

	parts := strings.Split(cleaned, ".")
	if len(parts) == 1 {
		val, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		return val * 100, nil
	} else if len(parts) == 2 {
		dollars, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, err
		}
		centsPart := parts[1]
		if len(centsPart) == 1 {
			centsPart += "0"
		} else if len(centsPart) > 2 {
			centsPart = centsPart[:2]
		}
		cents, err := strconv.ParseInt(centsPart, 10, 64)
		if err != nil {
			return 0, err
		}
		return dollars*100 + cents, nil
	}
	return 0, fmt.Errorf("invalid dollar format: %s", s)
}
