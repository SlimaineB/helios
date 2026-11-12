package gitops

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractSnapshot replaces dir with the tree from a hub-produced tarball. It
// extracts into a sibling temp directory first, so a failed download or a
// truncated archive never leaves a half-applied playbook behind.
func ExtractSnapshot(r io.Reader, dir, commit string) error {
	if commit == "" {
		return fmt.Errorf("snapshot has no commit")
	}
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".helios-snapshot-")
	if err != nil {
		return fmt.Errorf("create snapshot temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := extractTarGz(r, tmp); err != nil {
		return err
	}

	backup := ""
	if _, err := os.Lstat(dir); err == nil {
		backup = dir + ".old"
		_ = os.RemoveAll(backup)
		if err := os.Rename(dir, backup); err != nil {
			return fmt.Errorf("move old repo dir: %w", err)
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dir)
		}
		return fmt.Errorf("install snapshot: %w", err)
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func extractTarGz(r io.Reader, root string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("snapshot gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("snapshot tar: %w", err)
		}
		clean, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		if clean == "" {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(clean))
		mode := os.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if mode == 0 {
				mode = 0o755
			}
			if err := os.MkdirAll(target, mode); err != nil {
				return fmt.Errorf("create dir %s: %w", clean, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create parent %s: %w", clean, err)
			}
			if mode == 0 {
				mode = 0o644
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return fmt.Errorf("create file %s: %w", clean, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %s: %w", clean, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close file %s: %w", clean, err)
			}
		case tar.TypeSymlink:
			link, err := cleanLinkTarget(clean, hdr.Linkname)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(link), target); err != nil {
				return fmt.Errorf("create symlink %s: %w", clean, err)
			}
		case tar.TypeLink:
			link, err := cleanArchivePath(hdr.Linkname)
			if err != nil || link == "" {
				return fmt.Errorf("invalid hardlink target %q", hdr.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Link(filepath.Join(root, filepath.FromSlash(link)), target); err != nil {
				return fmt.Errorf("create hardlink %s: %w", clean, err)
			}
		default:
			// Git archives only emit dirs, regular files, symlinks and (rarely)
			// hardlinks. Ignore extended metadata headers.
		}
	}
}

func cleanArchivePath(name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := path.Clean(strings.TrimPrefix(name, "/"))
	if clean == "." || clean == "/" {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("snapshot path escapes repo: %s", name)
	}
	return clean, nil
}

func cleanLinkTarget(entry, link string) (string, error) {
	if link == "" {
		return "", fmt.Errorf("empty symlink target for %s", entry)
	}
	if path.IsAbs(strings.ReplaceAll(link, "\\", "/")) {
		return "", fmt.Errorf("absolute symlink target for %s", entry)
	}
	resolved := path.Clean(path.Join(path.Dir(entry), strings.ReplaceAll(link, "\\", "/")))
	if resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("symlink %s escapes repo", entry)
	}
	return link, nil
}
