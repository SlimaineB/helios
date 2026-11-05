package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/slim/helios/agent/internal/client"
	"github.com/slim/helios/agent/internal/config"
	"github.com/slim/helios/agent/internal/loop"
)

const (
	defaultConfigPath = "/etc/helios/agent.yaml"
	legacyConfigPath  = "/etc/ansiblehub/agent.yaml"
)

func main() {
	var (
		configPath = flag.String("config", defaultConfigPath, "path to agent config (yaml)")
		debug      = flag.Bool("debug", false, "verbose logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	path := *configPath
	if path == defaultConfigPath {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, legacyErr := os.Stat(legacyConfigPath); legacyErr == nil {
				path = legacyConfigPath
				log.Warn("using legacy agent config; move it to the new path", "from", legacyConfigPath, "to", defaultConfigPath)
			}
		}
	}

	cfg, err := config.Load(path)
	if err != nil {
		log.Error("config load failed", "error", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		log.Error("state dir", "error", err)
		os.Exit(1)
	}

	cl := client.New(cfg.Server, cfg.Token, cfg.InsecureSkipVerify)
	ag := loop.New(cfg, cl, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := ag.ResolveInstanceID(ctx); err != nil {
		log.Error("instance id resolution failed", "error", err)
		os.Exit(1)
	}

	if err := ag.Run(ctx); err != nil {
		log.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}
