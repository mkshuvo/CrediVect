package tests

import (
	"testing"
	"credivect/core/internal/domain"
)

func TestMoneyZeroFloatInvariants(t *testing.T) {
	m1 := domain.NewMoney(1500000, "USD") // $15,000.00
	m2 := domain.NewMoney(250000, "USD")  // $2,500.00

	sum, err := m1.Add(m2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sum.Cents != 1750000 {
		t.Errorf("expected 1750000 cents, got %d", sum.Cents)
	}

	diff, err := m1.Sub(m2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if diff.Cents != 1250000 {
		t.Errorf("expected 1250000 cents, got %d", diff.Cents)
	}
}

func TestBankersRounding(t *testing.T) {
	// Round half to even: 25 cents divided by 2 should round to 12 (even), not 13
	m := domain.NewMoney(25, "USD")
	res, err := m.DivIntWithBankersRounding(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Cents != 12 {
		t.Errorf("Banker's rounding failure: expected 12, got %d", res.Cents)
	}

	// 35 cents divided by 2 should round to 18 (even)
	m2 := domain.NewMoney(35, "USD")
	res2, err := m2.DivIntWithBankersRounding(2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res2.Cents != 18 {
		t.Errorf("Banker's rounding failure: expected 18, got %d", res2.Cents)
	}
}

func TestParseDollarsToCents(t *testing.T) {
	cents, err := domain.ParseDollarsToCents("$12,450.75")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cents != 1245075 {
		t.Errorf("expected 1245075 cents, got %d", cents)
	}
}

func TestUUIDv7TimeOrdered(t *testing.T) {
	id1 := domain.NewUUIDv7()
	id2 := domain.NewUUIDv7()

	if len(id1) != 36 || len(id2) != 36 {
		t.Fatalf("invalid UUID format length: %d", len(id1))
	}
	if id1 >= id2 {
		t.Errorf("UUIDv7 should be time-ordered: id1=%s, id2=%s", id1, id2)
	}
}
