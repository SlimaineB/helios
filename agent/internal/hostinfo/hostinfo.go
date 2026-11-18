// Package hostinfo collects a small, best-effort inventory of the local host.
package hostinfo

import (
	"context"
	"os"
	"os/exec"
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
