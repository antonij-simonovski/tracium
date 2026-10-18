package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/tracium/api/internal/model"
)

const tokenTTL = 24 * time.Hour

// TokenIssuer signs and parses the bearer tokens returned by login/register.
type TokenIssuer struct {
	secret []byte
}

func NewTokenIssuer(secret string) *TokenIssuer {
	return &TokenIssuer{secret: []byte(secret)}
}

// Issue returns a signed JWT for user. The pwd claim binds the token to the
// user's current password hash, so changing the password ends the session.
func (t *TokenIssuer) Issue(user model.User) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":       user.ID,
		"tenant_id": user.TenantID,
		"role":      user.Role,
		"pwd":       t.passwordStamp(user.PasswordHash),
		"iat":       now.Unix(),
		"exp":       now.Add(tokenTTL).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
}

type tokenClaims struct {
	userID        string
	passwordStamp string
}

func (t *TokenIssuer) parse(tokenString string) (tokenClaims, error) {
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		return t.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil {
		return tokenClaims{}, fmt.Errorf("invalid token: %w", err)
	}
	userID, _ := claims["sub"].(string)
	if userID == "" {
		return tokenClaims{}, errors.New("invalid token: missing sub")
	}
	stamp, _ := claims["pwd"].(string)
	return tokenClaims{userID: userID, passwordStamp: stamp}, nil
}

// passwordStamp is keyed with the signing secret so the token, which the client
// can read, reveals nothing about the bcrypt hash.
func (t *TokenIssuer) passwordStamp(passwordHash string) string {
	mac := hmac.New(sha256.New, t.secret)
	mac.Write([]byte(passwordHash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}
