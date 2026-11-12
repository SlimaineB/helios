package gitops

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

type tarEntry struct {
	name string
	body string
	mode int64
	typ  byte
	link string
}

func makeTarGz(t *testing.T, entries []tarEntry) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: e.mode, Typeflag: e.typ, Linkname: e.link}
		if e.typ == tar.TypeReg || e.typ == 0 {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if hdr.Size > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func TestExtractSnapshotReplacesTree(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := makeTarGz(t, []tarEntry{
		{name: "site.yml", body: "- hosts: all\n", mode: 0o644, typ: tar.TypeReg},
		{name: "bin/run.sh", body: "#!/bin/sh\n", mode: 0o755, typ: tar.TypeReg},
		{name: "link", typ: tar.TypeSymlink, link: "site.yml", mode: 0o777},
	})
	if err := ExtractSnapshot(archive, dir, "abc123"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "site.yml")); err != nil || string(b) != "- hosts: all\n" {
		t.Fatalf("site.yml = %q, %v", b, err)
	}
	info, err := os.Stat(filepath.Join(dir, "bin/run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("run.sh is not executable: %v", info.Mode())
	}
	if link, err := os.Readlink(filepath.Join(dir, "link")); err != nil || link != "site.yml" {
		t.Fatalf("link = %q, %v", link, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old file survived extraction: %v", err)
	}
}

func TestExtractSnapshotRejectsTraversal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	archive := makeTarGz(t, []tarEntry{
		{name: "../escape", body: "bad", mode: 0o644, typ: tar.TypeReg},
	})
	if err := ExtractSnapshot(archive, dir, "abc123"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape")); !os.IsNotExist(err) {
		t.Fatalf("escaped file exists: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("target dir should not be installed: %v", err)
	}
}

func TestExtractSnapshotRejectsEscapingSymlink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "repo")
	archive := makeTarGz(t, []tarEntry{
		{name: "link", typ: tar.TypeSymlink, link: "../../etc", mode: 0o777},
	})
	if err := ExtractSnapshot(archive, dir, "abc123"); err == nil {
		t.Fatal("expected symlink escape error")
	}
}
