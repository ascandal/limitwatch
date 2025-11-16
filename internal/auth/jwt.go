// Package auth provides minimal JWT parsing and role helpers for protecting
// the HTTP API.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Minimal JWT claim set used by the API layer. It intentionally
// models only the fields needed by this service.
type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`          // viewer|operator|admin
	Exp  int64  `json:"exp,omitempty"` // unix seconds
}

// Common errors returned by ParseAndValidateHS256.
var (
	ErrBadToken = errors.New("invalid token")
	ErrExpired  = errors.New("token expired")
	ErrAlg      = errors.New("unsupported alg")
)

// Validates a compact JWT signed with HS256 using the
// provided secret and returns the parsed Claims.
// It verifies the signature, algorithm, and expiration time (if present).
func ParseAndValidateHS256(token string, secret []byte) (*Claims, error) {
	if len(secret) == 0 {
		return nil, ErrBadToken
	}

	parts := strings.Split(token, ".")

	if len(parts) != 3 {
		return nil, ErrBadToken
	}

	headerB, err := b64urlDecode(parts[0])

	if err != nil {
		return nil, ErrBadToken
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}

	if err := json.Unmarshal(headerB, &header); err != nil {
		return nil, ErrBadToken
	}

	if header.Alg != "HS256" {
		return nil, ErrAlg
	}

	payloadB, err := b64urlDecode(parts[1])

	if err != nil {
		return nil, ErrBadToken
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(parts[0]))
	mac.Write([]byte("."))
	mac.Write([]byte(parts[1]))
	expected := mac.Sum(nil)
	sig, err := b64urlDecode(parts[2])

	if err != nil {
		return nil, ErrBadToken
	}

	if !hmac.Equal(expected, sig) {
		return nil, ErrBadToken
	}

	var c Claims

	if err := json.Unmarshal(payloadB, &c); err != nil {
		return nil, ErrBadToken
	}

	if c.Exp > 0 && time.Now().Unix() > c.Exp {
		return nil, ErrExpired
	}

	return &c, nil
}

// Decodes a base64url-encoded string without padding.
func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// Defines role precedence (viewer < operator < admin). Unknown roles
// return 0 and are treated as having the lowest precedence.
func RoleOrder(r string) int {
	switch strings.ToLower(r) {
	case "admin":
		return 3
	case "operator":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}
