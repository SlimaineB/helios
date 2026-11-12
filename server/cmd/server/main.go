package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/slim/helios/server/internal/api"
	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/envutil"
	"github.com/slim/helios/server/internal/metrics"
	"github.com/slim/helios/server/internal/notify"
	"github.com/slim/helios/server/internal/oidc"
	"github.com/slim/helios/server/internal/rbac"
	"github.com/slim/helios/server/internal/reconciler"
	"github.com/slim/helios/server/internal/store"
)

func main() {
	var (
		addr          = flag.String("addr", ":8080", "listen address")
		dbPath        = flag.String("db", "./helios.db", "database: sqlite file path or postgres:// URL")
		uiDir         = flag.String("ui", "", "path to built UI static files (optional)")
		baseURL       = flag.String("base-url", envutil.Or("HELIOS_BASE_URL", "http://localhost:8080"), "external base URL (OIDC redirect, cookies)")
		tlsCert       = flag.String("tls-cert", envutil.Get("HELIOS_TLS_CERT"), "TLS certificate file (enables HTTPS)")
		tlsKey        = flag.String("tls-key", envutil.Get("HELIOS_TLS_KEY"), "TLS private key file")
		rbacFile      = flag.String("rbac", envutil.Get("HELIOS_RBAC_FILE"), "RBAC policy file (ArgoCD-style p/g rules)")
		downloads     = flag.String("downloads", envutil.Get("HELIOS_DOWNLOADS_DIR"), "directory of agent tarballs served under /downloads (optional)")
		runnerToken   = flag.String("runner-token", envutil.Get("HELIOS_RUNNER_TOKEN"), "shared bearer token for bastion runners (empty disables agentless mode)")
		tunnelGateway = flag.String("tunnel-gateway", envutil.Get("HELIOS_TUNNEL_GATEWAY"), "SSH destination shown for tunnel instances, e.g. helios-tunnel@bastion.example.com")
		sessTTL       = flag.Duration("session-ttl", 12*time.Hour, "operator login session lifetime")
	)
	flag.Parse()
	tunnelPortMin := envInt("HELIOS_TUNNEL_PORT_MIN", 22000)
	tunnelPortMax := envInt("HELIOS_TUNNEL_PORT_MAX", 22999)

	if dsn := envutil.Get("HELIOS_DB"); dsn != "" {
		*dbPath = dsn
	}

	log := slog.Default()
	log.Info("helios server starting", "addr", *addr, "db", store.Kind(*dbPath))

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer st.Close()
	if store.Kind(*dbPath) == "sqlite" {
		log.Info("SQLite is single-writer: run one server replica; use PostgreSQL for multi-replica deployments")
	}
	_ = st.DeleteExpiredOperatorSessions(context.Background(), time.Now().UTC())

	// One-shot migration: register repo URLs already used by applications so
	// the Repositories page reflects them (ArgoCD order: repo then app).
	if err := st.BackfillRepositoryRegistry(context.Background()); err != nil {
		log.Warn("repository registry backfill failed", "error", err)
	} else if repos, err := st.ListRepositories(context.Background()); err == nil {
		log.Info("repository registry ready", "repositories", len(repos))
	}

	rb, err := rbac.FromFile(*rbacFile)
	if err != nil {
		log.Error("failed to load RBAC", "error", err)
		os.Exit(1)
	}

	secure := strings.HasPrefix(*baseURL, "https://")

	// Local admin (break-glass / bootstrap). Refuse trivially-weak defaults
	// unless explicitly acknowledged.
	opUser := envutil.Get("HELIOS_OPERATOR_USER")
	opPass := envutil.Get("HELIOS_OPERATOR_PASSWORD")
	if opUser != "" && opPass == "" {
		log.Error("refusing to start with an empty operator password; set HELIOS_OPERATOR_PASSWORD")
		os.Exit(1)
	}
	if opUser == "admin" && opPass == "admin" && envutil.Get("HELIOS_ALLOW_WEAK_DEFAULT") != "1" {
		log.Error("refusing to start with default operator credentials admin/admin; set HELIOS_OPERATOR_USER/PASSWORD (or HELIOS_ALLOW_WEAK_DEFAULT=1 for the lab)")
		os.Exit(1)
	}

	oa := &api.OpAuth{RBAC: rb, BaseURL: *baseURL, Sessions: auth.NewDBSessionStore(st, *sessTTL), BasicUser: opUser}
	if opUser != "" {
		oa.Basic = func(header string) bool { return auth.CheckBasic(header, opUser, opPass) }
		log.Info("operator basic admin enabled", "user", opUser)
	}
	if iss := envutil.Get("HELIOS_OIDC_ISSUER"); iss != "" && envutil.Get("HELIOS_OIDC_CLIENT_ID") != "" && envutil.Get("HELIOS_OIDC_CLIENT_SECRET") != "" {
		groupClaim := envutil.Or("HELIOS_OIDC_GROUP_CLAIM", "groups")
		prov, err := oidc.New(context.Background(), oidc.Config{
			Issuer:       iss,
			ClientID:     envutil.Get("HELIOS_OIDC_CLIENT_ID"),
			ClientSecret: envutil.Get("HELIOS_OIDC_CLIENT_SECRET"),
			RedirectURL:  *baseURL + "/api/v1/auth/callback",
			GroupClaim:   groupClaim,
			Scopes:       splitList(envutil.Get("HELIOS_OIDC_SCOPES")),
		})
		if err != nil {
			log.Error("failed to init OIDC", "error", err)
			os.Exit(1)
		}
		oa.OIDC = prov
		log.Info("OIDC enabled", "issuer", iss, "group_claim", groupClaim)
	}
	if oa.Basic == nil && oa.OIDC == nil {
		if envutil.Get("HELIOS_ALLOW_UNAUTHENTICATED") != "1" {
			log.Error("refusing to start without operator authentication; configure HELIOS_OPERATOR_USER/PASSWORD or OIDC (set HELIOS_ALLOW_UNAUTHENTICATED=1 only for local development)")
			os.Exit(1)
		}
		log.Warn("no operator authentication configured — UI/API is OPEN")
	}

	srv := api.New(st, log, oa, secure)
	srv.SetNotifier(notify.FromEnv(log))
	srv.SetDownloadsDir(*downloads)
	srv.SetRunnerToken(*runnerToken)
	srv.SetTunnelConfig(*tunnelGateway, tunnelPortMin, tunnelPortMax)
	if *runnerToken != "" {
		log.Info("runner API enabled for agentless tunnel instances", "gateway", *tunnelGateway, "port_range", fmt.Sprintf("%d-%d", tunnelPortMin, tunnelPortMax))
	}
	gitopsState := envutil.Or("HELIOS_GITOPS_STATE_DIR", "/tmp/helios-gitops")
	recon := reconciler.New(st, log, gitopsState)
	srv.SetReconciler(recon)
	metrics.SetInfo("dev")
	handler := srv.Handler()

	if *uiDir != "" {
		handler = uiMiddleware(handler, *uiDir, secure)
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Drain in-flight requests on SIGINT/SIGTERM (k8s pod termination, systemd
	// stop) instead of cutting them off mid-response.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	recon.Start(ctx)
	defer func() {
		srv.Close()
		cancel()
		recon.Wait()
	}()

	errCh := make(chan error, 1)
	go func() {
		if *tlsCert != "" && *tlsKey != "" {
			errCh <- httpServer.ListenAndServeTLS(*tlsCert, *tlsKey)
			return
		}
		errCh <- httpServer.ListenAndServe()
	}()

	log.Info("listening", "addr", *addr)
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown timed out; cancelling background work", "error", err)
		}
	}
}

func envInt(name string, def int) int {
	if v := envutil.Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func uiMiddleware(next http.Handler, uiDir string, secure bool) http.Handler {
	fileServer := http.FileServer(http.Dir(uiDir))
	index := filepath.Join(uiDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/downloads/") || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		api.SetSecurityHeaders(w, r, secure)
		clean := path.Clean("/" + r.URL.Path)
		if clean != r.URL.Path && (r.URL.Path != clean+"/" || strings.Contains(path.Base(clean), ".")) {
			http.NotFound(w, r)
			return
		}

		rel := strings.TrimPrefix(clean, "/")
		target := filepath.Join(uiDir, filepath.FromSlash(rel))
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			if strings.HasPrefix(clean, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if strings.Contains(path.Base(clean), ".") {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, index)
	})
}
