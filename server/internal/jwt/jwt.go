// Package jwt signs and verifies HS256 JSON Web Tokens.
package jwt

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var (
	ErrInvalid = errors.New("invalid token")
	ErrExpired = errors.New("token expired")
)

var header = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Sign encodes claims and signs them with secret.
func Sign(claims map[string]any, secret []byte) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(body)
	return unsigned + "." + sign(unsigned, secret), nil
}

func sign(s string, secret []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(s))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Verify checks the signature, the algorithm and exp, and returns the claims.
func Verify(token string, secret []byte, now time.Time) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, ErrInvalid
	}
	h, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalid
	}
	var hdr struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(h, &hdr) != nil || hdr.Alg != "HS256" {
		return nil, ErrInvalid
	}
	if !hmac.Equal([]byte(sign(parts[0]+"."+parts[1], secret)), []byte(parts[2])) {
		return nil, ErrInvalid
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalid
	}
	var claims map[string]any
	if err := json.Unmarshal(body, &claims); err != nil {
		return nil, ErrInvalid
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, ErrInvalid
	}
	if now.Unix() >= int64(exp) {
		return nil, ErrExpired
	}
	return claims, nil
}
