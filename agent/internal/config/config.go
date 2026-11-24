package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	// Server is the base URL of the central API, e.g. https://hub.example.com.
	Server string `yaml:"server"`
	// Token is the per-instance bearer token issued at bootstrap.
	Token string `yaml:"token"`
	// InstanceID is the canonical server-side instance ID. Empty = auto.
	// When empty on EC2, the agent registers the AWS instance id with the server.
	InstanceID string `yaml:"instance_id"`
	// StateDir is where the repo mirrors + runner files live.
	StateDir string `yaml:"state_dir"`
	// PollInterval controls both heartbeat and drift check frequency.
	PollInterval time.Duration `yaml:"poll_interval"`
	// JitterPercent spreads checks/heartbeats by +/- this percentage so a
	// fleet rolled out together does not hit the hub at the same instant.
	// 0 disables jitter; the default is 20.
	JitterPercent int `yaml:"jitter_percent"`
	// MaxConcurrentApps bounds how many applications are reconciled in
	// parallel. Keep this small on CPU-constrained hosts: every Ansible
	// invocation is its own process.
	MaxConcurrentApps int `yaml:"max_concurrent_apps"`
	// AutoHeal applies a playbook automatically when drift is detected (like
	// ArgoCD selfHeal). Sync commands still always apply.
	AutoHeal bool `yaml:"autoheal"`
	// InsecureSkipVerify disables TLS verification (testing only).
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
	// AwsRegion is used for IMDSv2 fallback; normally injected by env.
	AwsRegion string `yaml:"-"`
	// AnsibleBin overrides the ansible-playbook binary path.
	AnsibleBin string `yaml:"ansible_bin"`
	// InventoryMode switches Ansible from local execution to SSH against hosts
	// described by an inventory in the application repository. Used by the
	// runner (push) path; the installed agent leaves it false.
	InventoryMode bool `yaml:"inventory_mode"`
	// InventoryHost is the instance's host name in the inventory (--limit).
	InventoryHost string `yaml:"inventory_host"`
	SSHUser       string `yaml:"ssh_user"`
	SSHKey        string `yaml:"ssh_key"`
	SSHCommonArgs string `yaml:"ssh_common_args"`
	// Hostname/Platform let the runner report the managed instance rather than
	// the runner's own hostname.
	Hostname string `yaml:"hostname"`
	Platform string `yaml:"platform"`
}

func Default() Config {
	return Config{
		StateDir:          "/var/lib/helios/agent",
		PollInterval:      30 * time.Second,
		JitterPercent:     20,
		MaxConcurrentApps: 4,
		AnsibleBin:        "ansible-playbook",
		AwsRegion:         os.Getenv("AWS_REGION"),
	}
}

// Load reads a yaml config file and merges HELIOS_* env overrides.
// Env vars (e.g. HELIOS_SERVER, HELIOS_TOKEN, HELIOS_POLL_INTERVAL=2m, HELIOS_AUTOHEAL=1).
func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
	}

	env := func(name string) string {
		return os.Getenv("HELIOS_" + name)
	}

	applyEnv := func(name string) error {
		v := env(name)
		if v == "" {
			return nil
		}
		switch name {
		case "SERVER":
			cfg.Server = v
		case "TOKEN":
			cfg.Token = v
		case "INSTANCE_ID":
			cfg.InstanceID = v
		case "STATE_DIR":
			cfg.StateDir = v
		case "POLL_INTERVAL":
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid HELIOS_POLL_INTERVAL %q: %w", v, err)
			}
			cfg.PollInterval = d
		case "JITTER_PERCENT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 || n > 50 {
				return fmt.Errorf("invalid HELIOS_JITTER_PERCENT %q: must be 0-50", v)
			}
			cfg.JitterPercent = n
		case "MAX_CONCURRENT_APPS":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return fmt.Errorf("invalid HELIOS_MAX_CONCURRENT_APPS %q: must be >= 1", v)
			}
			cfg.MaxConcurrentApps = n
		case "AUTOHEAL":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid HELIOS_AUTOHEAL %q: %w", v, err)
			}
			cfg.AutoHeal = b
		case "INSECURE":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid HELIOS_INSECURE %q: %w", v, err)
			}
			cfg.InsecureSkipVerify = b
		case "ANSIBLE_BIN":
			cfg.AnsibleBin = v
		case "AWS_REGION":
			cfg.AwsRegion = v
		}
		return nil
	}
	for _, name := range []string{"SERVER", "TOKEN", "INSTANCE_ID", "STATE_DIR", "POLL_INTERVAL", "JITTER_PERCENT", "MAX_CONCURRENT_APPS", "AUTOHEAL", "INSECURE", "ANSIBLE_BIN", "AWS_REGION"} {
		if err := applyEnv(name); err != nil {
			return cfg, err
		}
	}

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if c.MaxConcurrentApps < 0 {
		return fmt.Errorf("max_concurrent_apps must be >= 0 (0 uses the default)")
	}
	if c.JitterPercent < 0 || c.JitterPercent > 50 {
		return fmt.Errorf("jitter_percent must be 0-50")
	}
	var missing []string
	if strings.TrimSpace(c.Server) == "" {
		missing = append(missing, "server")
	}
	if strings.TrimSpace(c.Token) == "" {
		missing = append(missing, "token")
	}
	if c.PollInterval <= 0 {
		missing = append(missing, "poll_interval")
	}
	if c.InventoryMode && strings.TrimSpace(c.InventoryHost) == "" {
		missing = append(missing, "inventory_host")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
