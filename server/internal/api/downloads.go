package api

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// downloadNameRe matches the flat files the hub is allowed to serve. The
// builder and installers normalise tarballs to helios-agent-linux-<arch>;
// versioned release asset names are accepted as well.
var downloadNameRe = regexp.MustCompile(`^helios-agent(?:-[0-9A-Za-z._-]+)?-linux-(amd64|arm64)\.tar\.gz$`)

type downloadFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// SetDownloadsDir enables the public /downloads/ endpoint. An empty dir
// disables it (the API still reports an empty file list).
func (s *Server) SetDownloadsDir(dir string) { s.downloadsDir = dir }

func validDownloadName(name string) bool {
	return name == "checksums.txt" || downloadNameRe.MatchString(name)
}

func (s *Server) handleListDownloads(w http.ResponseWriter, r *http.Request) {
	files := make([]downloadFile, 0)
	if s.downloadsDir != "" {
		entries, err := os.ReadDir(s.downloadsDir)
		if err != nil && !os.IsNotExist(err) {
			s.internalError(w, r, err)
			return
		}
		for _, entry := range entries {
			if !validDownloadName(entry.Name()) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			files = append(files, downloadFile{Name: entry.Name(), Size: info.Size()})
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.downloadsDir == "" || !validDownloadName(name) || filepath.Base(name) != name {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.downloadsDir, name)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.ServeFile(w, r, path)
}
