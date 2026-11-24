package repocache

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

const (
	maxSnapshotFileBytes  = 4 << 20
	maxSnapshotTotalBytes = 16 << 20
	maxSnapshotFiles      = 512
)

// ReadTree returns the regular files at or below subdir from a snapshot
// tarball. Keys are repo-relative slash paths. It refuses paths that escape the
// repository root and caps how much it reads.
func ReadTree(snap Snapshot, subdir string) (map[string][]byte, error) {
	if snap.Path == "" || snap.NotModified {
		return nil, errors.New("snapshot has no tree")
	}
	subdir = path.Clean(strings.TrimPrefix(strings.ReplaceAll(subdir, "\\", "/"), "./"))
	if subdir == "." {
		subdir = ""
	}
	if subdir == ".." || strings.HasPrefix(subdir, "../") || strings.HasPrefix(subdir, "/") {
		return nil, fmt.Errorf("invalid inventory path %q", subdir)
	}

	f, err := os.Open(snap.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	files := map[string][]byte{}
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			continue
		}
		if subdir != "" && name != subdir && !strings.HasPrefix(name, subdir+"/") {
			continue
		}
		if len(files) >= maxSnapshotFiles {
			return files, fmt.Errorf("too many files under %q", subdir)
		}
		if hdr.Size > maxSnapshotFileBytes || total+hdr.Size > maxSnapshotTotalBytes {
			return files, fmt.Errorf("inventory tree under %q is too large", subdir)
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxSnapshotFileBytes+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxSnapshotFileBytes {
			return files, fmt.Errorf("%s is too large", name)
		}
		files[name] = data
		total += int64(len(data))
	}
	return files, nil
}
