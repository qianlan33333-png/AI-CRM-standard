package credential

import (
	"errors"
	"strings"
	"testing"
)

func TestArgon2idPHCRoundTrip(t *testing.T) {
	hasher := PasswordHasher{}
	encoded, err := hasher.Hash("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$") {
		t.Fatalf("hash = %q", encoded)
	}
	if !hasher.Verify("correct horse battery staple", encoded) || hasher.Verify("wrong password", encoded) {
		t.Fatal("password verification mismatch")
	}
}

func TestOpaqueCredentialStoresOnlyDigest(t *testing.T) {
	value, digest, err := IssueOpaque("as_")
	if err != nil {
		t.Fatal(err)
	}
	if value == "" || string(digest[:]) == value || !Matches(value, digest) || Matches(value+"x", digest) {
		t.Fatal("opaque credential contract failed")
	}
}

func TestPasswordEightByteMinimumAndLongPasswordCompatibility(t *testing.T) {
	h := PasswordHasher{}
	if _, err := h.Hash("Seven77"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("seven-byte password accepted")
	}
	encoded, err := h.Hash("EightPw8")
	if err != nil || !h.Verify("EightPw8", encoded) || h.Verify("wrong888", encoded) {
		t.Fatal("eight-byte password hashing or verification failed")
	}
	long := strings.Repeat("p", 1024)
	encoded, err = h.Hash(long)
	if err != nil || !h.Verify(long, encoded) {
		t.Fatal("existing maximum changed")
	}
	if _, err := h.Hash(long + "p"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatal("maximum length protection lost")
	}
}
