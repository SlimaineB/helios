package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestListDownloadsFiltersAndSorts(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	s.SetDownloadsDir(dir)
	for name, body := range map[string]string{
		"helios-agent-linux-amd64.tar.gz":       "amd64",
		"helios-agent-0.1.1-linux-arm64.tar.gz": "arm64",
		"checksums.txt":                         "sums",
		"notes.txt":                             "nope",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	s.handleListDownloads(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Files []downloadFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"checksums.txt",
		"helios-agent-0.1.1-linux-arm64.tar.gz",
		"helios-agent-linux-amd64.tar.gz",
	}
	if len(got.Files) != len(want) {
		t.Fatalf("files = %+v, want %v", got.Files, want)
	}
	for i, name := range want {
		if got.Files[i].Name != name {
			t.Fatalf("file %d = %q, want %q", i, got.Files[i].Name, name)
		}
	}
}

func TestDownloadServesKnownFile(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	s.SetDownloadsDir(dir)
	name := "helios-agent-linux-amd64.tar.gz"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("agent"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/downloads/"+name, nil)
	req.SetPathValue("name", name)
	rec := httptest.NewRecorder()
	s.handleDownload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "agent" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="`+name+`"` {
		t.Fatalf("content-disposition = %q", got)
	}
}

func TestDownloadRejectsUnknownAndTraversal(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	s.SetDownloadsDir(dir)
	if err := os.WriteFile(filepath.Join(dir, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"secret.txt", "../secret.txt", "missing.tar.gz"} {
		req := httptest.NewRequest(http.MethodGet, "/downloads/"+name, nil)
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		s.handleDownload(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%q: status %d, want 404", name, rec.Code)
		}
	}
}

func TestDownloadsDisabled(t *testing.T) {
	s, _ := newTestServer(t)

	rec := httptest.NewRecorder()
	s.handleListDownloads(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Files []downloadFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 0 {
		t.Fatalf("files = %+v, want empty", got.Files)
	}

	req := httptest.NewRequest(http.MethodGet, "/downloads/helios-agent-linux-amd64.tar.gz", nil)
	req.SetPathValue("name", "helios-agent-linux-amd64.tar.gz")
	rec = httptest.NewRecorder()
	s.handleDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestDownloadRejectsSymlink(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "helios-agent-linux-amd64.tar.gz"
	if err := os.Symlink(secret, filepath.Join(dir, name)); err != nil {
		t.Fatal(err)
	}
	s.SetDownloadsDir(dir)

	rec := httptest.NewRecorder()
	s.handleListDownloads(rec, httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Files []downloadFile `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Files) != 0 {
		t.Fatalf("files = %+v, want empty", got.Files)
	}

	req := httptest.NewRequest(http.MethodGet, "/downloads/"+name, nil)
	req.SetPathValue("name", name)
	rec = httptest.NewRecorder()
	s.handleDownload(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}
