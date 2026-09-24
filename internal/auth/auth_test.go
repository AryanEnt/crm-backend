package auth

import (
	"testing"
)

func TestHashRefreshTokenDeterministic(t *testing.T) {
	t.Parallel()
	a := HashRefreshToken("abc")
	b := HashRefreshToken("abc")
	if a != b || a == "" {
		t.Fatalf("expected deterministic hash")
	}
}

func TestNewRefreshTokenUnique(t *testing.T) {
	t.Parallel()
	r1, h1, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	r2, h2, err := NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if r1 == r2 || h1 == h2 {
		t.Fatal("expected unique tokens")
	}
	if HashRefreshToken(r1) != h1 {
		t.Fatal("hash mismatch")
	}
}

func TestJWTRoundTrip(t *testing.T) {
	t.Parallel()
	svc := NewJWTService("test-secret-key-at-least-32-chars!!")
	token, _, err := svc.IssueAccessToken("uid-1", "a@b.com", RoleSuperAdmin)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ParseAccessToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "uid-1" || claims.Email != "a@b.com" || claims.RoleCode != RoleSuperAdmin {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestPasswordHashCompare(t *testing.T) {
	t.Parallel()
	h := NewBcryptHasher()
	hash, err := h.Hash("ChangeMe123!")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Compare(hash, "ChangeMe123!"); err != nil {
		t.Fatal(err)
	}
	if err := h.Compare(hash, "wrong"); err == nil {
		t.Fatal("expected mismatch")
	}
}
