package ansible

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/slim/helios/agent/internal/gitops"
)

// Result is a normalized outcome of an ansible-playbook run.
type Result struct {
	Rc           int            `json:"rc"`
	Status       string         `json:"status"` // ok | drift | failed
	Summary      map[string]int `json:"summary"`
	ChangedTasks []ChangedTask  `json:"changed_tasks"`
	OutputTail   string         `json:"output_tail"`
	Raw          string         `json:"raw"`
	// OutputText is the readable rendering of the run (PLAY/TASK/recap).
	OutputText string `json:"output_text"`
	// OutputDiff keeps only the diff hunks (what a --check would change).
	OutputDiff string `json:"diff"`
	// Resources lists the deployed/managed endpoints of the run.
	Resources []Resource `json:"resources"`
	Err       string     `json:"error,omitempty"`
}

// ChangedTask identifies a task that drift detection flagged.
type ChangedTask struct {
	TaskName string `json:"task_name"`
	Host     string `json:"host"`
}

const (
	StatusOK     = "ok"
	StatusDrift  = "drift"
	StatusFailed = "failed"
)

// Target describes a remote host reached over SSH (agentless tunnel mode).
// Nil/zero Port keeps the local connection used by the installed agent.
type Target struct {
	Host       string
	Port       int
	User       string
	KeyFile    string
	CommonArgs string
}

// Runner executes ansible-playbook against a local mirror.
type Runner struct {
	Bin     string
	Timeout time.Duration
	// Target switches from local execution to SSH against a tunneled host.
	Target *Target
	// ExtraEnv allows injecting vars for CI-ish runs (unused in check/apply inline).
	ExtraEnv map[string]string
}

// Check runs the playbook in --check mode (dry-run): changed tasks == drift.
func (r *Runner) Check(ctx context.Context, repo *gitops.Repo, playbook, varsJSON string) (Result, error) {
	return r.run(ctx, repo, playbook, varsJSON, true)
}

// Apply runs the playbook for real.
func (r *Runner) Apply(ctx context.Context, repo *gitops.Repo, playbook, varsJSON string) (Result, error) {
	return r.run(ctx, repo, playbook, varsJSON, false)
}

func (r *Runner) run(ctx context.Context, repo *gitops.Repo, playbook, varsJSON string, check bool) (Result, error) {
	// Keep run read-only on Runner: checks are executed concurrently and the
	// previous lazy defaults were a data race on Bin/Timeout.
	bin, timeout := r.Bin, r.Timeout
	if bin == "" {
		bin = "ansible-playbook"
	}
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-i", "localhost,", "--connection", "local"}
	if r.Target != nil && r.Target.Port > 0 {
		host := strings.TrimSpace(r.Target.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		args = []string{"-i", host + ",", "--connection", "ssh"}
		if r.Target.User != "" {
			args = append(args, "-u", r.Target.User)
		}
		if r.Target.KeyFile != "" {
			args = append(args, "--private-key", r.Target.KeyFile)
		}
		args = append(args, "-e", fmt.Sprintf("ansible_port=%d", r.Target.Port))
		if r.Target.CommonArgs != "" {
			args = append(args, "--ssh-common-args", r.Target.CommonArgs)
		}
	}
	if check {
		args = append(args, "--check")
	}
	args = append(args, "--diff")
	if varsJSON != "" && varsJSON != "{}" {
		args = append(args, "-e", varsJSON)
	}
	args = append(args, playbook)

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = repo.Dir
	cmd.WaitDelay = 15 * time.Second
	cmd.Env = append(os.Environ(),
		"ANSIBLE_STDOUT_CALLBACK=json",
		"ANSIBLE_HOST_KEY_CHECKING=false",
		"ANSIBLE_NOCOLOR=1",
		"ANSIBLE_FORCE_COLOR=0",
		"ANSIBLE_LOCALHOST_WARNING=false",
		"ANSIBLE_INVENTORY_UNPARSED_WARNING=false",
	)
	for k, v := range r.ExtraEnv {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := stdout.String()
	out += "\n" + stderr.String()

	res, doc := parseRun(out, cmd.ProcessState.ExitCode(), check)
	if doc != nil {
		if text := FormatText(doc); text != "" {
			res.OutputText = text
		}
		if diff := FormatDiff(doc); diff != "" {
			res.OutputDiff = diff
		}
		res.Resources = extractResources(doc)
	}
	if runErr != nil && res.Rc == 0 {
		res.Rc = 1
	}
	if res.Rc != 0 {
		res.Status = StatusFailed
		res.Err = runErrDetail(runErr, out)
	}
	res.Raw = truncate(out, 256<<10)
	res.OutputTail = truncate(out, 4096)
	return res, runErr
}

func parseResult(output string, rc int, check bool) Result {
	res, _ := parseRun(output, rc, check)
	return res
}

func parseRun(output string, rc int, check bool) (Result, *callbackDoc) {
	summary := map[string]int{}
	var changed []ChangedTask

	doc := findPlaysJSON(output)
	if doc != nil {
		agg := aggregate(doc)
		summary = agg.summary
		changed = agg.changed
	}

	status := StatusOK
	// Only a --check run is drift-sensitive: changes there mean the live state
	// diverges from the desired state. A successful apply that changed things
	// is simply "ok".
	if check && len(changed) > 0 {
		status = StatusDrift
	}
	if rc != 0 {
		status = StatusFailed
	}
	return Result{Rc: rc, Status: status, Summary: summary, ChangedTasks: changed}, doc
}

type callbackDoc struct {
	Plays []struct {
		Play struct {
			Name string `json:"name"`
		} `json:"play"`
		Tasks []struct {
			Task struct {
				Name string `json:"name"`
			} `json:"task"`
			Hosts map[string]*hostResult `json:"hosts"`
		} `json:"tasks"`
	} `json:"plays"`
	Stats map[string]*statLine `json:"stats"`
}

type agg struct {
	summary map[string]int
	changed []ChangedTask
}

func aggregate(doc *callbackDoc) agg {
	// Prefer the provider "stats" section when present.
	if len(doc.Stats) > 0 {
		summary := map[string]int{}
		for _, s := range doc.Stats {
			summary["ok"] += s.Ok
			summary["changed"] += s.Changed
			summary["failed"] += s.Failures
			summary["skipped"] += s.Skipped
			summary["rescued"] += s.Rescued
			summary["ignored"] += s.Ignored
		}
		// Fall back to tasks walk for changed-task names.
		return agg{summary: summary, changed: changedTasks(doc)}
	}

	summary := map[string]int{}
	var changed []ChangedTask
	for _, play := range doc.Plays {
		for _, t := range play.Tasks {
			for host, h := range t.Hosts {
				name := t.Task.Name
				if h.Changed {
					changed = append(changed, ChangedTask{TaskName: name, Host: host})
				}
				if h.Failed || h.Unreachable {
					summary["failed"]++
				}
				if h.Ok {
					summary["ok"]++
				}
				if h.Skipped {
					summary["skipped"]++
				}
				if h.Changed {
					summary["changed"]++
				}
			}
		}
	}
	return agg{summary: summary, changed: changed}
}

func changedTasks(doc *callbackDoc) []ChangedTask {
	var changed []ChangedTask
	for _, play := range doc.Plays {
		for _, t := range play.Tasks {
			for host, h := range t.Hosts {
				if h.Changed {
					changed = append(changed, ChangedTask{TaskName: t.Task.Name, Host: host})
				}
			}
		}
	}
	return changed
}

// findPlaysJSON scans raw output for the JSON callback document (a top-level
// object with a "plays" key), which may be surrounded by other log lines.
func findPlaysJSON(out string) *callbackDoc {
	for scan := 0; ; {
		start := strings.Index(out[scan:], "{")
		if start < 0 {
			return nil
		}
		start += scan
		if doc, restOK := tryParseObject(out, start, &callbackDoc{}); restOK && doc != nil && len(doc.Plays) >= 0 {
			// Accept only objects that actually look like the callback doc (or stats).
			if len(doc.Plays) > 0 || len(doc.Stats) > 0 {
				return doc
			}
		}
		scan = start + 1
	}
}

// tryParseObject balances braces starting at start and unmarshals.
func tryParseObject[T any](s string, start int, into *T) (*T, bool) {
	depth := 0
	inStr := false
	esc := false
	end := -1
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil, false
	}
	segment := s[start : end+1]
	if err := json.Unmarshal([]byte(segment), into); err != nil {
		return nil, false
	}
	return into, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func runErrDetail(err error, out string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if out != "" {
		msg = strings.TrimSpace(out) + "\n" + msg
	}
	return fmt.Sprintf("%s", msg)
}
