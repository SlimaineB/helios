// Package secretbox encrypts values that must be stored at rest, such as
// repository credentials and secret playbook variables.
//
// Ciphertexts are self-describing: they are prefixed with "enc:v1:" so values
// written before encryption was enabled (plaintext) keep working and are never
// mistaken for ciphertext. A key is derived from HELIOS_SECRET_KEY; changing it
// makes existing ciphertexts unreadable, so keep it stable and backed up.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const prefix = "enc:v1:"

// Box encrypts and decrypts short secrets with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// New builds a Box from a key string. The key may be 32 raw bytes encoded as
// hex or base64; any other non-empty string is hashed with SHA-256 so a long
// random passphrase also works.
func New(key string) (*Box, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("secretbox: empty key")
	}
	k, err := parseKey(key)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("secretbox: cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: gcm: %w", err)
	}
	return &Box{aead: aead}, nil
}

func parseKey(s string) ([]byte, error) {
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	sum := sha256.Sum256([]byte(s))
	return sum[:], nil
}

// IsEncrypted reports whether v carries the ciphertext prefix.
func IsEncrypted(v string) bool { return strings.HasPrefix(v, prefix) }

// Encrypt returns the sealed, prefixed form of plain. Empty values and values
// that already look encrypted are returned unchanged.
func (b *Box) Encrypt(plain string) (string, error) {
	if plain == "" || IsEncrypted(plain) {
		return plain, nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt returns the plaintext for a value produced by Encrypt. Values without
// the ciphertext prefix are returned unchanged (legacy plaintext rows).
func (b *Box) Decrypt(stored string) (string, error) {
	if !IsEncrypted(stored) {
		return stored, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", fmt.Errorf("secretbox: decode: %w", err)
	}
	ns := b.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secretbox: ciphertext too short")
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return "", errors.New("secretbox: cannot decrypt (wrong HELIOS_SECRET_KEY?)")
	}
	return string(plain), nil
}
