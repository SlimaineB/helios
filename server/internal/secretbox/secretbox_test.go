package secretbox

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	box, err := New("a-long-random-production-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	plain := "hunter2-super-secret"
	sealed, err := box.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if sealed == plain || !IsEncrypted(sealed) {
		t.Fatalf("value was not encrypted: %q", sealed)
	}
	got, err := box.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Fatalf("roundtrip = %q, want %q", got, plain)
	}
}

func TestPlaintextPassThrough(t *testing.T) {
	box, err := New("key")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"", "legacy-plaintext"} {
		got, err := box.Decrypt(v)
		if err != nil || got != v {
			t.Fatalf("Decrypt(%q) = %q, %v", v, got, err)
		}
	}
}

func TestWrongKeyFails(t *testing.T) {
	a, _ := New("key-one")
	b, _ := New("key-two")
	sealed, err := a.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Decrypt(sealed); err == nil {
		t.Fatal("decrypt with a different key should fail")
	}
}

func TestTamperedCiphertextFails(t *testing.T) {
	box, _ := New("key")
	sealed, err := box.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, err := box.Decrypt(tampered); err == nil {
		t.Fatal("tampered ciphertext should fail authentication")
	}
}

func TestHexKeyAccepted(t *testing.T) {
	key := strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
	if _, err := New(key); err != nil {
		t.Fatalf("32-byte hex key rejected: %v", err)
	}
}
