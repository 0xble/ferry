package share

import "testing"

func TestShareTokenDeterministic(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890123456789012")
	id := "abc123"

	a := ShareToken(secret, id, DefaultTokenBytes)
	b := ShareToken(secret, id, DefaultTokenBytes)
	if a != b {
		t.Fatalf("ShareToken should be deterministic, got %q and %q", a, b)
	}
	if !ValidateShareToken(secret, id, a, DefaultTokenBytes) {
		t.Fatal("expected token validation to pass")
	}
	if ValidateShareToken(secret, id+"x", a, DefaultTokenBytes) {
		t.Fatal("expected token validation to fail for a different share id")
	}
}

func TestShareTokenCustomBytes(t *testing.T) {
	t.Parallel()

	secret := []byte("12345678901234567890123456789012")
	id := "abc123"

	short := ShareToken(secret, id, 4)
	long := ShareToken(secret, id, 16)
	if len(short) >= len(long) {
		t.Fatalf("expected shorter token with fewer bytes: short=%q long=%q", short, long)
	}
	if !ValidateShareToken(secret, id, short, 4) {
		t.Fatal("expected short token validation to pass")
	}
	if ValidateShareToken(secret, id, short, 16) {
		t.Fatal("expected short token to fail validation with different byte count")
	}
}
