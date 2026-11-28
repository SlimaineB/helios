// Package hostinfo collects a small, best-effort inventory of the local host.
package hostinfo

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/slim/helios/agent/internal/client"
)

// Collect returns the basic host inventory. Missing tools or files are simply
// omitted; collection must never block or fail an agent checkin.
func Collect(ansibleBin string) client.HostInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	info := client.HostInfo{
		CPUCount: runtime.NumCPU(),
		Arch:     runtime.GOARCH,
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		info.MemoryMB = memTotalMB(string(b))
	}
	if b, err := os.ReadFile("/etc/os-release"); err == nil {
		info.OS = prettyName(string(b))
	}
	if out, err := exec.CommandContext(ctx, "python3", "--version").CombinedOutput(); err == nil {
		info.Python = firstLine(string(out))
	}
	if bin := strings.TrimSpace(ansibleBin); bin != "" {
		if out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput(); err == nil {
			info.Ansible = firstLine(string(out))
		}
	}
	return info
}

func memTotalMB(meminfo string) int {
	for _, line := range strings.Split(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "MemTotal:" {
			continue
		}
		kb, err := strconv.Atoi(fields[1])
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

func prettyName(osRelease string) string {
	for _, line := range strings.Split(osRelease, "\n") {
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// CollectRuntime returns the Ansible/Python toolchain installed on this host.
// It never blocks or fails a runner start: missing tools are simply omitted.
func CollectRuntime(ansibleBin string) client.RunnerRuntime {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rt := client.RunnerRuntime{}
	if bin := strings.TrimSpace(ansibleBin); bin != "" {
		if out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput(); err == nil {
			rt.Ansible = firstLine(string(out))
		}
	}
	if out, err := exec.CommandContext(ctx, "python3", "--version").CombinedOutput(); err == nil {
		rt.Python = firstLine(string(out))
	}
	if gal := galaxyBin(ansibleBin); gal != "" {
		rt.Collections = listCollections(ctx, gal)
	}
	return rt
}

// galaxyBin locates ansible-galaxy next to the ansible binary, falling back to
// PATH. Empty when unavailable.
func galaxyBin(ansibleBin string) string {
	if ansibleBin != "" {
		if cand := strings.TrimSpace(filepath.Join(filepath.Dir(ansibleBin), "ansible-galaxy")); cand != "" {
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}
	}
	if _, err := exec.LookPath("ansible-galaxy"); err == nil {
		return "ansible-galaxy"
	}
	return ""
}

// listCollections returns installed collections, best-effort. It tries the JSON
// output first (ansible-core >= 2.15) and falls back to the table format.
func listCollections(ctx context.Context, gal string) []client.RunnerCollection {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, gal, "collection", "list", "--format", "json").CombinedOutput(); err == nil {
		if colls := parseCollectionsJSON(string(out)); len(colls) > 0 {
			return colls
		}
	}
	out, err := exec.CommandContext(ctx, gal, "collection", "list").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseCollectionsText(string(out))
}

func parseCollectionsJSON(raw string) []client.RunnerCollection {
	var perPath map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &perPath); err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []client.RunnerCollection{}
	for _, byName := range perPath {
		for name, version := range byName {
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, client.RunnerCollection{Name: name, Version: version})
			if len(out) >= 200 {
				return out
			}
		}
	}
	return out
}

func parseCollectionsText(raw string) []client.RunnerCollection {
	seen := map[string]bool{}
	out := []client.RunnerCollection{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "Collection") || strings.HasPrefix(line, "---") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		out = append(out, client.RunnerCollection{Name: fields[0], Version: fields[1]})
		if len(out) >= 200 {
			return out
		}
	}
	return out
}
