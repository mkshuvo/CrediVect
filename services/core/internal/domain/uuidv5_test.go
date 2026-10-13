package domain

import (
	"regexp"
	"strings"
	"testing"
)

// A UUIDv5 is defined as the truncated SHA-1 of namespace+name with the version
// and variant bits set, so the published vectors are the test. If this ever
// disagrees with another implementation, federated accounts stop finding their
// own rows.
func TestNewUUIDv5MatchesRFC4122Vector(t *testing.T) {
	got, err := NewUUIDv5(NamespaceDNS, "www.google.com")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	// uuid.uuid5(uuid.NAMESPACE_DNS, "www.google.com")
	const want = "488416f4-fcaf-5027-8c63-0105cfa213ea"
	if got != want {
		t.Errorf("derived %s, the published vector is %s", got, want)
	}

	// The federated name is two elements joined by 0x00: issuer then subject.
	multi, err := NewUUIDv5(NamespaceFederatedIdentity, "https://idp.example/", "8f2c1d0a-4b7e-4d33-9d1f-6d2c2f0a1b9e")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	const wantMulti = "91ad75f4-d9e2-5078-8cb8-977c7937495e"
	if multi != wantMulti {
		t.Errorf("derived %s, hashlib computes %s", multi, wantMulti)
	}
}

func TestNewUUIDv5IsStableAndWellFormed(t *testing.T) {
	issuer := "https://idp.example.com/"
	subject := "auth0|8f2c1d"

	first, err := NewUUIDv5(NamespaceFederatedIdentity, issuer, subject)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	second, err := NewUUIDv5(NamespaceFederatedIdentity, issuer, subject)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if first != second {
		t.Errorf("the same identity derived %s then %s", first, second)
	}
	shape := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !shape.MatchString(first) {
		t.Errorf("%s is not a version 5 UUID with the RFC 4122 variant", first)
	}

	other, err := NewUUIDv5(NamespaceFederatedIdentity, "https://other.example.com/", subject)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if other == first {
		t.Error("two issuers using the same subject string derived the same account id")
	}
}

// The separator between elements is what keeps ("a", "b|c") apart from ("a|b",
// "c"); without it one provider could move a subject across fields and inherit
// another account.
func TestNewUUIDv5ElementBoundaryIsInjective(t *testing.T) {
	left, err := NewUUIDv5(NamespaceFederatedIdentity, "issuer", "a", "b|c")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	right, err := NewUUIDv5(NamespaceFederatedIdentity, "issuer", "a|b", "c")
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if left == right {
		t.Errorf("element split collision: both derived %s", left)
	}
}

func TestNewUUIDv5RejectsMalformedNamespace(t *testing.T) {
	for _, namespace := range []string{"", "not-a-uuid", "6ba7b810-9dad-11d1-80b4-00c04fd430c"} {
		if _, err := NewUUIDv5(namespace, "subject"); err == nil {
			t.Errorf("namespace %q was accepted", namespace)
		}
	}
}

func TestNamespaceDNSIsThePublishedConstant(t *testing.T) {
	if !strings.EqualFold(NamespaceDNS, "6ba7b810-9dad-11d1-80b4-00c04fd430c8") {
		t.Errorf("NamespaceDNS is %q", NamespaceDNS)
	}
	if NamespaceDNS != NamespaceFederatedIdentity {
		t.Error("the federated namespace is expected to be the DNS namespace")
	}
}
