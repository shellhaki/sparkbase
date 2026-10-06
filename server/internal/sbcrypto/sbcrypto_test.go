package sbcrypto

import (
	"strings"
	"testing"
)

func TestBoxRoundTripAndTamper(t *testing.T) {
	box, err := NewBox(RandomBytes(32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := box.SealString("s3cret")
	got, err := box.OpenString(sealed)
	if err != nil || got != "s3cret" {
		t.Fatalf("round trip: %q %v", got, err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := box.Open(sealed); err == nil {
		t.Fatal("tampered ciphertext decrypted")
	}
	other, _ := NewBox(RandomBytes(32))
	if _, err := other.Open(box.SealString("x")); err == nil {
		t.Fatal("wrong key decrypted")
	}
	if _, err := NewBox([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestKeys(t *testing.T) {
	full, display := NewKey(PrefixSecret)
	if !strings.HasPrefix(full, "sbs_") || len(full) != 44 || display != full[:12] {
		t.Fatalf("bad key %s %s", full, display)
	}
	other, _ := NewKey(PrefixSecret)
	if SHA256Hex(full) == SHA256Hex(other) || SHA256Hex(full) != SHA256Hex(full) {
		t.Fatal("hash mismatch")
	}
}
