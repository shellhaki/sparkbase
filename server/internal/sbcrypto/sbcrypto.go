// Package sbcrypto holds the encryption, hashing and token helpers.
package sbcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
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
