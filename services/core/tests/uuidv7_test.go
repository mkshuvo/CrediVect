package tests

import (
	"regexp"
	"sync"
	"testing"

	"credivect/core/internal/domain"
)

// A UUIDv7 whose rand_a bits are pure random sorts by luck inside one
// millisecond, which is exactly the window a seeded book or a burst of
// submissions occupies. These tests pin the ordering the primary keys rely on.

func TestUUIDv7BurstStaysOrdered(t *testing.T) {
	const n = 20000
	ids := make([]string, n)
	for i := range ids {
		ids[i] = domain.NewUUIDv7()
	}
	for i := 1; i < n; i++ {
		if ids[i-1] >= ids[i] {
			t.Fatalf("id %d (%s) does not sort after id %d (%s)", i, ids[i], i-1, ids[i-1])
		}
	}
	seen := make(map[string]bool, n)
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}

var uuidv7Re = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestUUIDv7Shape(t *testing.T) {
	// Version 7 in the high nibble of byte 6, RFC 4122 variant in the top two bits
	// of byte 8. A malformed id fails the UUID column CHECK on insert.
	for i := 0; i < 500; i++ {
		id := domain.NewUUIDv7()
		if !uuidv7Re.MatchString(id) {
			t.Fatalf("%s is not a canonical UUIDv7", id)
		}
	}
}

func TestUUIDv7IsConcurrencySafe(t *testing.T) {
	const workers = 16
	const perWorker = 500

	before := domain.NewUUIDv7()
	ids := make([]string, workers*perWorker)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				ids[w*perWorker+i] = domain.NewUUIDv7()
			}
		}(w)
	}
	wg.Wait()
	after := domain.NewUUIDv7()

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !uuidv7Re.MatchString(id) {
			t.Fatalf("%s is malformed under contention", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %s from concurrent callers", id)
		}
		seen[id] = true
		if id <= before || id >= after {
			t.Fatalf("%s escaped the bracketing ids %s / %s", id, before, after)
		}
	}
}
