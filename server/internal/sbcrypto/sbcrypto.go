// Package sbcrypto holds the encryption, hashing and token helpers.
package sbcrypto

import (
	"crypto/subtle"
	"fmt"

	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"golang.org/x/crypto/argon2"
	"math/big"
	"strings"
)

// Box encrypts and decrypts stored secrets with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

func NewBox(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal returns nonce || ciphertext.
func (b *Box) Seal(plain []byte) []byte {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return b.aead.Seal(nonce, nonce, plain, nil)
}

func (b *Box) Open(sealed []byte) ([]byte, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("ciphertext too short")
	}
	return b.aead.Open(nil, sealed[:n], sealed[n:], nil)
}

func (b *Box) SealString(s string) []byte { return b.Seal([]byte(s)) }

func (b *Box) OpenString(sealed []byte) (string, error) {
	p, err := b.Open(sealed)
	return string(p), err
}

func RandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

// Token returns a URL-safe random token with 32 bytes of entropy.
func Token() string { return base64.RawURLEncoding.EncodeToString(RandomBytes(32)) }

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// RandomString returns n characters from [a-zA-Z0-9].
func RandomString(n int) string { return randomFrom(alphabet, n) }

// RandomLower returns n characters from [a-z0-9], for identifiers.
func RandomLower(n int) string { return randomFrom("abcdefghijklmnopqrstuvwxyz0123456789", n) }

func randomFrom(chars string, n int) string {
	var sb strings.Builder
	max := big.NewInt(int64(len(chars)))
	for i := 0; i < n; i++ {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		sb.WriteByte(chars[v.Int64()])
	}
	return sb.String()
}

// SHA256Hex hashes high-entropy secrets (keys, session tokens) for lookup.
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func HMAC(key []byte, msg string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return m.Sum(nil)
}

// Key kinds and their prefixes.
const (
	PrefixAccount     = "sb_"
	PrefixPublishable = "sbp_"
	PrefixSecret      = "sbs_"
)

// NewKey returns a full key (prefix + 40 random characters) and the display
// prefix stored next to its hash.
func NewKey(prefix string) (full, display string) {
	full = prefix + RandomString(40)
	return full, full[:len(prefix)+8]
}

// Argon2id parameters (OWASP baseline).
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
)

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) string {
	salt := RandomBytes(16)
	sum := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(sum))
}

// VerifyPassword checks a password against an encoded argon2id hash.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

var dummyHash = HashPassword("sparkbase-dummy-password")

// DummyVerify spends the same time as a real check, so unknown emails cannot
// be told apart by timing.
func DummyVerify(password string) { VerifyPassword(password, dummyHash) }

// ValidPassword reports whether a password meets the minimum policy.
func ValidPassword(p string) bool { return len(p) >= 8 && len(p) <= 256 }
