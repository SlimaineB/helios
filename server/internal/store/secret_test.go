package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slim/helios/server/internal/model"
)

func TestRepositorySecretsEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secrets.db")
	st, err := Open(path, WithSecretKey("unit-test-secret-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	repo, err := st.CreateRepository(ctx, model.Repository{
		Name: "demo", URL: "https://example.com/demo.git",
		Username: "git", Password: "s3cr3t", SSHKey: "PRIVATE-KEY",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	var rawPassword, rawKey string
	if err := st.(*sqlstore).db.QueryRow(
		`SELECT password, ssh_key FROM repositories WHERE id=?`, repo.ID,
	).Scan(&rawPassword, &rawKey); err != nil {
		t.Fatalf("read raw repo: %v", err)
	}
	if rawPassword == "s3cr3t" || !strings.HasPrefix(rawPassword, "enc:v1:") {
		t.Fatalf("password not encrypted at rest: %q", rawPassword)
	}
	if rawKey == "PRIVATE-KEY" || !strings.HasPrefix(rawKey, "enc:v1:") {
		t.Fatalf("ssh key not encrypted at rest: %q", rawKey)
	}

	got, err := st.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if got.Password != "s3cr3t" || got.SSHKey != "PRIVATE-KEY" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestSecretVariablesEncryptedAtRest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vars.db")
	st, err := Open(path, WithSecretKey("unit-test-secret-key"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()

	app, err := st.CreateApplication(ctx, model.Application{
		Name: "demo", RepoURL: "https://example.com/demo.git", Branch: "main", Path: "site.yml",
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if err := st.SaveAppVariables(ctx, app.ID, []model.Variable{
		{Key: "token", Value: "abc123", IsSecret: true},
		{Key: "region", Value: "eu-west-1"},
	}); err != nil {
		t.Fatalf("save vars: %v", err)
	}

	var rawToken, rawRegion string
	if err := st.(*sqlstore).db.QueryRow(
		`SELECT value FROM variables WHERE app_id=? AND key='token'`, app.ID,
	).Scan(&rawToken); err != nil {
		t.Fatalf("read raw token: %v", err)
	}
	if rawToken == "abc123" || !strings.HasPrefix(rawToken, "enc:v1:") {
		t.Fatalf("secret variable not encrypted at rest: %q", rawToken)
	}
	if err := st.(*sqlstore).db.QueryRow(
		`SELECT value FROM variables WHERE app_id=? AND key='region'`, app.ID,
	).Scan(&rawRegion); err != nil {
		t.Fatalf("read raw region: %v", err)
	}
	if rawRegion != "eu-west-1" {
		t.Fatalf("non-secret variable should stay plaintext, got %q", rawRegion)
	}

	vars, err := st.AppVariables(ctx, app.ID)
	if err != nil {
		t.Fatalf("read vars: %v", err)
	}
	byKey := map[string]string{}
	for _, v := range vars {
		byKey[v.Key] = v.Value
	}
	if byKey["token"] != "abc123" || byKey["region"] != "eu-west-1" {
		t.Fatalf("variable roundtrip mismatch: %+v", byKey)
	}
}

func TestEncryptedValueRequiresMatchingKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secrets.db")

	writer, err := Open(path, WithSecretKey("right-key"))
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	repo, err := writer.CreateRepository(ctx, model.Repository{
		Name: "demo", URL: "https://example.com/demo.git", Password: "s3cr3t",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	_ = writer.Close()

	noKey, err := Open(path)
	if err != nil {
		t.Fatalf("reopen without key: %v", err)
	}
	defer noKey.Close()
	if _, err := noKey.GetRepository(ctx, repo.ID); err == nil {
		t.Fatal("reading ciphertext without a key should fail")
	}
}

func TestLegacyPlaintextReadableWithKey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	writer, err := Open(path) // no key: plaintext row
	if err != nil {
		t.Fatalf("open writer: %v", err)
	}
	repo, err := writer.CreateRepository(ctx, model.Repository{
		Name: "legacy", URL: "https://example.com/legacy.git", Password: "old-plain",
	})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	_ = writer.Close()

	reader, err := Open(path, WithSecretKey("new-key"))
	if err != nil {
		t.Fatalf("reopen with key: %v", err)
	}
	defer reader.Close()
	got, err := reader.GetRepository(ctx, repo.ID)
	if err != nil {
		t.Fatalf("read legacy repo: %v", err)
	}
	if got.Password != "old-plain" {
		t.Fatalf("legacy password = %q, want old-plain", got.Password)
	}
}
