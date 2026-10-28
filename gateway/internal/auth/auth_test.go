package auth

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordAndJWT(t *testing.T) {
	h, err := HashPassword("beta-password-1")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "beta-password-1") || CheckPassword(h, "wrong") {
		t.Fatal("password check broken")
	}
	if _, err := HashPassword("short"); !errors.Is(err, ErrPasswordShort) {
		t.Fatalf("short password must return ErrPasswordShort, got %v", err)
	}
	tok, err := SignAccess("test-secret", "u-1", "a@example.ma", "c-1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := VerifyAccess("test-secret", tok)
	if err != nil || c.CompanyID != "c-1" || c.ID == "" {
		t.Fatalf("jwt verify broken (want company and jti): %v %+v", err, c)
	}
	if _, err := VerifyAccess("other", tok); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("wrong secret must be ErrTokenInvalid, got %v", err)
	}
	if HashToken("abc") == "abc" {
		t.Fatal("tokens must be stored hashed, not raw")
	}
}

func TestExpiredTokenIsDistinguished(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour)
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: "u-1",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(past.Add(time.Minute)), IssuedAt: jwt.NewNumericDate(past),
		},
	}).SignedString([]byte("s"))
	if _, err := VerifyAccess("s", tok); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("expired token must return ErrTokenExpired, got %v", err)
	}
}

func TestNewUUIDIsV4(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewUUID()
		if !re.MatchString(id) {
			t.Fatalf("not a v4 UUID: %s", id)
		}
		if seen[id] {
			t.Fatalf("duplicate UUID %s", id)
		}
		seen[id] = true
	}
}
