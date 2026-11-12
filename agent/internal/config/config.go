package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ModeAgent  = "agent"
	ModeTunnel = "tunnel"
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
	// TargetHost/TargetPort switch Ansible from local execution to SSH (used by
	// the agentless runner). TargetPort 0 keeps the normal local mode.
	TargetHost    string `yaml:"target_host"`
	TargetPort    int    `yaml:"target_port"`
	SSHUser       string `yaml:"ssh_user"`
	SSHKey        string `yaml:"ssh_key"`
	SSHCommonArgs string `yaml:"ssh_common_args"`
	// Hostname/Platform let the runner report the tunnel instance rather than
	// the bastion's own hostname.
	Hostname string `yaml:"hostname"`
	Platform string `yaml:"platform"`

	// Mode selects the agent role: "agent" reconciles Ansible applications,
	// "tunnel" only maintains the reverse SSH tunnel and reports health.
	Mode string `yaml:"mode"`
	// TunnelSSHKey is the private key used for the bastion reverse tunnel.
	TunnelSSHKey string `yaml:"tunnel_ssh_key"`
	// TunnelSSHCommonArgs is inserted before the built-in tunnel ssh options.
	TunnelSSHCommonArgs string `yaml:"tunnel_ssh_common_args"`
	// TunnelLocalTarget is the local sshd address forwarded by the bastion.
	TunnelLocalTarget string `yaml:"tunnel_local_target"`
	// TunnelGateway/TunnelPort override hub-provided tunnel settings.
	TunnelGateway string `yaml:"tunnel_gateway"`
	TunnelPort    int    `yaml:"tunnel_port"`
	// TunnelHealthInterval is how often the tunnel agent reports liveness.
	TunnelHealthInterval time.Duration `yaml:"tunnel_health_interval"`
}

// IsTunnel reports whether the agent runs in tunnel-only mode.
func (c Config) IsTunnel() bool {
	return strings.EqualFold(strings.TrimSpace(c.Mode), ModeTunnel)
}

func Default() Config {
	return Config{
		Server:               "",
		StateDir:             "/var/lib/helios/agent",
		PollInterval:         5 * time.Minute,
		MaxConcurrentApps:    4,
		AnsibleBin:           "ansible-playbook",
		AwsRegion:            os.Getenv("AWS_REGION"),
		Mode:                 ModeAgent,
		TunnelLocalTarget:    "127.0.0.1:22",
		TunnelHealthInterval: 30 * time.Second,
	}
}

// Load reads a yaml config file and merges HELIOS_* env overrides. The legacy
// ANSIBLEHUB_* / ANH_* / AHUB_* names are accepted as fallbacks.
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
		for _, prefix := range []string{"HELIOS_", "ANSIBLEHUB_", "ANH_", "AHUB_"} {
			if v := os.Getenv(prefix + name); v != "" {
				return v
			}
		}
		return ""
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
		case "MODE":
			cfg.Mode = v
		case "TUNNEL_SSH_KEY":
			cfg.TunnelSSHKey = v
		case "TUNNEL_SSH_COMMON_ARGS":
			cfg.TunnelSSHCommonArgs = v
		case "TUNNEL_LOCAL_TARGET":
			cfg.TunnelLocalTarget = v
		case "TUNNEL_GATEWAY":
			cfg.TunnelGateway = v
		case "TUNNEL_PORT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 65535 {
				return fmt.Errorf("invalid HELIOS_TUNNEL_PORT %q: must be 1-65535", v)
			}
			cfg.TunnelPort = n
		case "TUNNEL_HEALTH_INTERVAL":
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("invalid HELIOS_TUNNEL_HEALTH_INTERVAL %q: %w", v, err)
			}
			cfg.TunnelHealthInterval = d
		}
		return nil
	}
	for _, name := range []string{"SERVER", "TOKEN", "INSTANCE_ID", "STATE_DIR", "POLL_INTERVAL", "MAX_CONCURRENT_APPS", "AUTOHEAL", "INSECURE", "ANSIBLE_BIN", "AWS_REGION", "MODE", "TUNNEL_SSH_KEY", "TUNNEL_SSH_COMMON_ARGS", "TUNNEL_LOCAL_TARGET", "TUNNEL_GATEWAY", "TUNNEL_PORT", "TUNNEL_HEALTH_INTERVAL"} {
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
	if c.Mode != "" && !strings.EqualFold(c.Mode, ModeAgent) && !strings.EqualFold(c.Mode, ModeTunnel) {
		return fmt.Errorf("mode must be %q or %q", ModeAgent, ModeTunnel)
	}
	if c.IsTunnel() && strings.TrimSpace(c.TunnelSSHKey) == "" {
		missing = append(missing, "tunnel_ssh_key")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	return nil
}
