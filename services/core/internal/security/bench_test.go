package security

import "testing"

// These measure the per-request cost of the gates a borrower read has to pass
// through: a blind index on the way in, a digest compare on every status token,
// and AES-GCM on the stored SSN. They are inline with the request, so their
// total is what the documented sub-50ms target pays before any I/O.
func BenchmarkHasher(b *testing.B) {
	h, err := NewHasher(testKey)
	if err != nil {
		b.Fatalf("NewHasher: %v", err)
	}
	ssn := "582114903"

	b.Run("BlindIndex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = h.BlindIndex(ssn)
		}
	})

	b.Run("Encrypt", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := h.Encrypt(ssn); err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("EncryptDecryptRoundTrip", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sealed, err := h.Encrypt(ssn)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := h.Decrypt(sealed); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkStatusToken(b *testing.B) {
	stored := HashStatusToken("0000000000000000000000000000000000000000000")

	b.Run("IssueHashAndCompare", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			token, err := NewStatusToken()
			if err != nil {
				b.Fatal(err)
			}
			digest := HashStatusToken(token)
			if !StatusTokenMatches(digest, token) {
				b.Fatal("freshly issued token did not verify")
			}
		}
	})

	b.Run("CompareAgainstStoredDigest", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if !StatusTokenMatches(stored, "0000000000000000000000000000000000000000000") {
				b.Fatal("expected match")
			}
		}
	})
}
