package auth

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestValidRejectsBadHashes(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if !Valid(token, Hash(token)) {
		t.Fatal("valid token rejected")
	}
	if Valid(token, "not-hex") {
		t.Fatal("malformed hash accepted")
	}
	if Valid(token+"x", Hash(token)) {
		t.Fatal("different token accepted")
	}
}

func TestCheckBasic(t *testing.T) {
	header := "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:secret"))
	if !CheckBasic(header, "admin", "secret") {
		t.Fatal("valid credentials rejected")
	}
	for _, tc := range []struct{ name, header, user, pass string }{
		{"empty header", "", "admin", "secret"},
		{"empty user", header, "", "secret"},
		{"empty password", header, "admin", ""},
		{"wrong scheme", strings.Replace(header, "Basic", "Bearer", 1), "admin", "secret"},
		{"wrong user", header, "other", "secret"},
		{"wrong password", header, "admin", "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if CheckBasic(tc.header, tc.user, tc.pass) {
				t.Fatal("invalid credentials accepted")
			}
		})
	}
}
