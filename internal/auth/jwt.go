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

// Claims is a minimal JWT claim set we use for demo auth.
type Claims struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`          // viewer|operator|admin
	Exp  int64  `json:"exp,omitempty"` // unix seconds
}

var (
	ErrBadToken = errors.New("invalid token")
	ErrExpired  = errors.New("token expired")
	ErrAlg      = errors.New("unsupported alg")
)

// ParseAndValidateHS256 validates a JWT signed with HS256 and returns Claims.
func ParseAndValidateHS256(token string, secret []byte) (*Claims, error) {
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

func b64urlDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// RoleOrder defines role precedence (viewer < operator < admin)
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
