package auth

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correcthorse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !checkPassword(hash, "correcthorse") {
		t.Fatal("checkPassword: expected match")
	}
	if checkPassword(hash, "wrong-password") {
		t.Fatal("checkPassword: expected mismatch")
	}
}

// Accounts created for an external identity provider store an empty hash; no
// password, including the empty one, may match it.
func TestEmptyHashNeverMatches(t *testing.T) {
	for _, pw := range []string{"", "correcthorse"} {
		if checkPassword("", pw) {
			t.Fatalf("empty hash matched %q", pw)
		}
	}
}

func TestDummyHashCostsAsMuchAsARealOne(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyHash))
	if err != nil || cost != bcrypt.DefaultCost {
		t.Fatalf("dummy hash cost = %d, %v; want %d", cost, err, bcrypt.DefaultCost)
	}
}
