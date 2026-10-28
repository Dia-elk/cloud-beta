// Package auth: bcrypt passwords, short-lived JWT access tokens and
// opaque rotating refresh tokens (generic primitives, no storage).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	AccessTTL  = time.Hour
	RefreshTTL = 7 * 24 * time.Hour
	MinPassLen = 8
)

var (
	ErrPasswordShort = errors.New("password too short")
	ErrTokenExpired  = errors.New("token expired")
	ErrTokenInvalid  = errors.New("token invalid")
)

func HashPassword(pw string) (string, error) {
	if len(pw) < MinPassLen {
		return "", ErrPasswordShort
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

type Claims struct {
	UserID    string `json:"sub"`
	Email     string `json:"email"`
	CompanyID string `json:"company_id"`
	jwt.RegisteredClaims
}

// SignAccess issues an access token with a unique ID (jti) so a single
// token can be revoked on logout before it expires.
func SignAccess(secret, userID, email, companyID string) (string, error) {
	now := time.Now()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: userID, Email: email, CompanyID: companyID,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        NewUUID(),
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(AccessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	})
	return t.SignedString([]byte(secret))
}

// VerifyAccess returns ErrTokenExpired for an expired but otherwise
// valid token, so clients can refresh instead of signing in again.
func VerifyAccess(secret, token string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if errors.Is(err, jwt.ErrTokenExpired) {
		return nil, ErrTokenExpired
	}
	if err != nil || !t.Valid {
		return nil, ErrTokenInvalid
	}
	c, ok := t.Claims.(*Claims)
	if !ok || c.UserID == "" {
		return nil, ErrTokenInvalid
	}
	return c, nil
}

// RandomToken is an opaque 256-bit secret, used for share links and
// refresh tokens. Only its HashToken digest is ever stored.
func RandomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// NewUUID returns a random RFC 4122 version 4 UUID.
func NewUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
