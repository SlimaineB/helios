package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/slim/ansiblehub/agent/internal/client"
	"github.com/slim/ansiblehub/agent/internal/config"
	"github.com/slim/ansiblehub/agent/internal/loop"
)

func main() {
	var (
		configPath = flag.String("config", "/etc/ansiblehub/agent.yaml", "path to agent config (yaml)")
		debug      = flag.Bool("debug", false, "verbose logging")
	)
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*configPath)
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
