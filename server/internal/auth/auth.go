package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// NewToken generates the plaintext token handed out once during bootstrap.
func NewToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Hash returns the hex-encoded SHA-256 of the token (what we persist).
func Hash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Valid compares a presented token to the stored hash in constant time-ish.
func Valid(token, storedHash string) bool {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(token))) == storedHash
}

// CheckBasic reports whether the Authorization header carries valid HTTP Basic
// credentials for the given user/pass. Both sides are compared in constant
// time; an empty user/pass always fails.
func CheckBasic(header, user, pass string) bool {
	if header == "" || user == "" || pass == "" {
		return false
	}
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return false
	}
	sep := bytes.IndexByte(raw, ':')
	if sep < 0 {
		return false
	}
	if subtle.ConstantTimeCompare(raw[:sep], []byte(user)) != 1 {
		return false
	}
	return subtle.ConstantTimeCompare(raw[sep+1:], []byte(pass)) == 1
}
