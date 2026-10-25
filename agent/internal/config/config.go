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
	// MaxConcurrentApps bounds how many applications the agent reconciles in
	// parallel during one pass. Keep this small on CPU-constrained hosts:
	// every Ansible invocation is its own process.
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
}

func Default() Config {
	return Config{
		Server:            "",
		StateDir:          "/var/lib/ansiblehub/agent",
		PollInterval:      5 * time.Minute,
		MaxConcurrentApps: 4,
		AnsibleBin:        "ansible-playbook",
		AwsRegion:         os.Getenv("AWS_REGION"),
	}
}

// Load reads a yaml config file and merges ANSIBLEHUB_* / ANH_* env overrides.
// Env vars (e.g. ANH_SERVER, ANH_TOKEN, ANH_POLL_INTERVAL=2m, ANH_AUTOHEAL=1).
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
		if v := os.Getenv("ANSIBLEHUB_" + name); v != "" {
			return v
		}
		return os.Getenv("ANH_" + name)
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
				return fmt.Errorf("invalid ANH_POLL_INTERVAL %q: %w", v, err)
			}
			cfg.PollInterval = d
		case "MAX_CONCURRENT_APPS":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return fmt.Errorf("invalid ANH_MAX_CONCURRENT_APPS %q: must be >= 1", v)
			}
			cfg.MaxConcurrentApps = n
		case "AUTOHEAL":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid ANH_AUTOHEAL %q: %w", v, err)
			}
			cfg.AutoHeal = b
		case "INSECURE":
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("invalid ANH_INSECURE %q: %w", v, err)
			}
			cfg.InsecureSkipVerify = b
		case "ANSIBLE_BIN":
			cfg.AnsibleBin = v
		case "AWS_REGION":
			cfg.AwsRegion = v
		}
		return nil
	}
	for _, name := range []string{"SERVER", "TOKEN", "INSTANCE_ID", "STATE_DIR", "POLL_INTERVAL", "MAX_CONCURRENT_APPS", "AUTOHEAL", "INSECURE", "ANSIBLE_BIN", "AWS_REGION"} {
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
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
