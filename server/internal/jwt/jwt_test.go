package jwt

import (
	"strings"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	secret := []byte("s3cret")
	now := time.Now()
	tok, err := Sign(map[string]any{"sub": "u1", "exp": now.Add(time.Hour).Unix()}, secret)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify(tok, secret, now)
	if err != nil || claims["sub"] != "u1" {
		t.Fatalf("verify: %v %v", claims, err)
	}
	if _, err := Verify(tok, []byte("other"), now); err != ErrInvalid {
		t.Fatalf("wrong secret accepted: %v", err)
	}
	if _, err := Verify(tok, secret, now.Add(2*time.Hour)); err != ErrExpired {
		t.Fatalf("expired accepted: %v", err)
	}
	parts := strings.Split(tok, ".")
	if _, err := Verify(parts[0]+"."+parts[1]+"x."+parts[2], secret, now); err == nil {
		t.Fatal("tampered body accepted")
	}
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + parts[1] + "."
	if _, err := Verify(none, secret, now); err == nil {
		t.Fatal("alg none accepted")
	}
}
