package domain

import "testing"

// The primary keys and the money arithmetic are on the request path for every
// origination and every read that formats an amount, and both are pure CPU.
func BenchmarkNewUUIDv7(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewUUIDv7()
	}
}

func BenchmarkMoneyArithmetic(b *testing.B) {
	monthly := NewMoney(780000, "USD")
	b.Run("DivIntWithBankersRounding", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := monthly.DivIntWithBankersRounding(12); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("AddAndFormat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sum, err := monthly.Add(NewMoney(2400000, "USD"))
			if err != nil {
				b.Fatal(err)
			}
			_ = sum.FormatDollars()
		}
	})
}

func BenchmarkParseDollarsToCents(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseDollarsToCents("7,800.55"); err != nil {
			b.Fatal(err)
		}
	}
}
