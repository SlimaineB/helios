package gitops

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Supported manifest kinds.
const (
	KindProject     = "Project"
	KindRepository  = "Repository"
	KindApplication = "Application"
	KindAssignment  = "Assignment"
)

// Metadata is the common manifest header. Project scopes an object; it is
// optional for Project (its own name is the scope) and defaults to global.
type Metadata struct {
	Name        string            `yaml:"name" json:"name"`
	Project     string            `yaml:"project" json:"project"`
	Annotations map[string]string `yaml:"annotations" json:"annotations,omitempty"`
}

// Document is one YAML document from a config repository.
type Document struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   Metadata       `yaml:"metadata" json:"metadata"`
	Spec       map[string]any `yaml:"spec" json:"spec"`
	Source     string         `yaml:"-" json:"-"`
}

// ParseDir walks path inside root and parses every .yaml/.yml document.
// A plain file path is accepted too. Empty documents are ignored.
func ParseDir(root, path string) ([]Document, error) {
	target := filepath.Join(root, path)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return nil, fmt.Errorf("config path escapes repository: %s", path)
	}

	var files []string
	info, err := os.Stat(absTarget)
	if err != nil {
		return nil, fmt.Errorf("config path not found: %s", path)
	}
	if info.IsDir() {
		err = filepath.WalkDir(absTarget, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if isYAML(p) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else if isYAML(absTarget) {
		files = append(files, absTarget)
	}

	sort.Strings(files)
	out := []Document{}
	for _, f := range files {
		docs, err := parseFile(f)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", relTo(absRoot, f), err)
		}
		for i := range docs {
			docs[i].Source = relTo(absRoot, f)
		}
		out = append(out, docs...)
	}
	return out, nil
}

func isYAML(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".yaml", ".yml":
		return true
	}
	return false
}

func relTo(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return filepath.ToSlash(r)
}

func parseFile(path string) ([]Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	out := []Document{}
	for {
		var doc Document
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(doc.Kind) == "" && strings.TrimSpace(doc.Metadata.Name) == "" && len(doc.Spec) == 0 {
			continue // blank document / trailing ---
		}
		out = append(out, doc)
	}
	return out, nil
}

// Validate checks the common header and known kinds.
func (d Document) Validate() error {
	switch d.Kind {
	case KindProject, KindRepository, KindApplication, KindAssignment:
	default:
		return fmt.Errorf("unsupported kind %q", d.Kind)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return errors.New("metadata.name is required")
	}
	if d.APIVersion != "" && d.APIVersion != "helios.dev/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", d.APIVersion)
	}
	return nil
}

// String returns a stable key for duplicate detection.
func (d Document) String() string { return d.Kind + "/" + d.Metadata.Name }

func (d Document) project() string {
	if p := strings.TrimSpace(d.Metadata.Project); p != "" {
		return p
	}
	return strings.TrimSpace(str(d.Spec, "project"))
}

// ProjectSpec is the typed spec of a Project manifest.
type ProjectSpec struct {
	RepoPatterns string
}

func (d Document) Project() (ProjectSpec, error) {
	patterns := str(d.Spec, "repoPatterns")
	if patterns == "" {
		patterns = str(d.Spec, "repo_patterns")
	}
	if list, ok := d.Spec["repoPatterns"].([]any); ok {
		parts := make([]string, 0, len(list))
		for _, v := range list {
			parts = append(parts, fmt.Sprint(v))
		}
		patterns = strings.Join(parts, "\n")
	}
	return ProjectSpec{RepoPatterns: patterns}, nil
}

// RepositorySpec is the typed spec of a Repository manifest.
type RepositorySpec struct {
	Name               string
	URL                string
	Username           string
	Password           string
	SSHKey             string
	InsecureSkipVerify bool
}

func (d Document) Repository() (RepositorySpec, error) {
	url := str(d.Spec, "url")
	if url == "" {
		url = str(d.Spec, "repoURL")
	}
	if url == "" {
		return RepositorySpec{}, errors.New("spec.url is required")
	}
	return RepositorySpec{
		Name:               d.Metadata.Name,
		URL:                url,
		Username:           str(d.Spec, "username"),
		Password:           str(d.Spec, "password"),
		SSHKey:             str(d.Spec, "sshKey"),
		InsecureSkipVerify: boolean(d.Spec["insecureSkipVerify"], boolean(d.Spec["insecure_skip_verify"], false)),
	}, nil
}

// ApplicationSpec is the typed spec of an Application manifest.
type ApplicationSpec struct {
	Name         string
	Project      string
	RepoURL      string
	RepoRef      string
	Branch       string
	Path         string
	Vars         string
	AutoHeal     bool
	SyncInterval int
	IgnorePaths  string
}

func (d Document) Application() (ApplicationSpec, error) {
	repoURL := str(d.Spec, "repoURL")
	if repoURL == "" {
		repoURL = str(d.Spec, "repo_url")
	}
	repoRef := str(d.Spec, "repoRef")
	if repoURL == "" && repoRef == "" {
		return ApplicationSpec{}, errors.New("spec.repoURL or spec.repoRef is required")
	}
	vars := "{}"
	if v, ok := d.Spec["vars"]; ok && v != nil {
		b, err := json.Marshal(v)
		if err != nil {
			return ApplicationSpec{}, fmt.Errorf("spec.vars: %w", err)
		}
		vars = string(b)
	}
	ignore := str(d.Spec, "ignorePaths")
	if ignore == "" {
		ignore = str(d.Spec, "ignore_paths")
	}
	return ApplicationSpec{
		Name:         d.Metadata.Name,
		Project:      d.project(),
		RepoURL:      repoURL,
		RepoRef:      repoRef,
		Branch:       strDefault(d.Spec, "branch", "main"),
		Path:         strDefault(d.Spec, "path", "site.yml"),
		Vars:         vars,
		AutoHeal:     boolean(d.Spec["autoHeal"], boolean(d.Spec["autoheal"], false)),
		SyncInterval: integer(d.Spec["syncInterval"], integer(d.Spec["sync_interval"], 0)),
		IgnorePaths:  ignore,
	}, nil
}

// AssignmentSpec is the typed spec of an Assignment manifest.
type AssignmentSpec struct {
	Name        string
	Project     string
	Application string
	Instance    string
	AutoHeal    *bool
}

func (d Document) Assignment() (AssignmentSpec, error) {
	app := str(d.Spec, "application")
	if app == "" {
		app = str(d.Spec, "app")
	}
	instance := str(d.Spec, "instance")
	if app == "" || instance == "" {
		return AssignmentSpec{}, errors.New("spec.application and spec.instance are required")
	}
	var ah *bool
	if v, ok := d.Spec["autoHeal"]; ok && v != nil {
		b := boolean(v, false)
		ah = &b
	} else if v, ok := d.Spec["autoheal"]; ok && v != nil {
		b := boolean(v, false)
		ah = &b
	}
	return AssignmentSpec{
		Name:        d.Metadata.Name,
		Project:     d.project(),
		Application: app,
		Instance:    instance,
		AutoHeal:    ah,
	}, nil
}

func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func strDefault(m map[string]any, key, def string) string {
	if s := str(m, key); s != "" {
		return s
	}
	return def
}

func boolean(v any, def bool) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		b, err := strconv.ParseBool(t)
		if err == nil {
			return b
		}
	case int:
		return t != 0
	case int64:
		return t != 0
	case float64:
		return t != 0
	}
	return def
}

func integer(v any, def int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, err := strconv.Atoi(t)
		if err == nil {
			return n
		}
	}
	return def
}

// DecodeDocuments is a convenience for tests: parse YAML bytes.
func DecodeDocuments(raw []byte) ([]Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	out := []Document{}
	for {
		var doc Document
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(doc.Kind) == "" && strings.TrimSpace(doc.Metadata.Name) == "" && len(doc.Spec) == 0 {
			continue
		}
		out = append(out, doc)
	}
	return out, nil
}
