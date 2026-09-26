package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/slim/ansiblehub/server/internal/api"
	"github.com/slim/ansiblehub/server/internal/auth"
	"github.com/slim/ansiblehub/server/internal/oidc"
	"github.com/slim/ansiblehub/server/internal/rbac"
	"github.com/slim/ansiblehub/server/internal/store"
)

func main() {
	var (
		addr     = flag.String("addr", ":8080", "listen address")
		dbPath   = flag.String("db", "./ansiblehub.db", "database: sqlite file path or postgres:// URL")
		uiDir    = flag.String("ui", "", "path to built UI static files (optional)")
		baseURL  = flag.String("base-url", envOrDefault("ANSIBLEHUB_BASE_URL", "http://localhost:8080"), "external base URL (OIDC redirect, cookies)")
		tlsCert  = flag.String("tls-cert", os.Getenv("ANSIBLEHUB_TLS_CERT"), "TLS certificate file (enables HTTPS)")
		tlsKey   = flag.String("tls-key", os.Getenv("ANSIBLEHUB_TLS_KEY"), "TLS private key file")
		rbacFile = flag.String("rbac", os.Getenv("ANSIBLEHUB_RBAC_FILE"), "RBAC policy file (ArgoCD-style p/g rules)")
		sessTTL  = flag.Duration("session-ttl", 12*time.Hour, "operator login session lifetime")
	)
	flag.Parse()

	if dsn := os.Getenv("ANSIBLEHUB_DB"); dsn != "" {
		*dbPath = dsn
	}

	log := slog.Default()
	log.Info("ansiblehub server starting", "addr", *addr, "db", store.Kind(*dbPath))

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Error("failed to open store", "error", err)
		os.Exit(1)
	}
	defer st.Close()

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
	opUser := os.Getenv("ANSIBLEHUB_OPERATOR_USER")
	opPass := os.Getenv("ANSIBLEHUB_OPERATOR_PASSWORD")
	if opUser == "admin" && opPass == "admin" && os.Getenv("ANH_ALLOW_WEAK_DEFAULT") != "1" {
		log.Error("refusing to start with default operator credentials admin/admin; set ANSIBLEHUB_OPERATOR_USER/PASSWORD (or ANH_ALLOW_WEAK_DEFAULT=1 for the lab)")
		os.Exit(1)
	}

	oa := &api.OpAuth{RBAC: rb, BaseURL: *baseURL, Sessions: auth.NewSessionStore(*sessTTL)}
	if opUser != "" {
		oa.Basic = func(header string) bool { return auth.CheckBasic(header, opUser, opPass) }
		log.Info("operator basic admin enabled", "user", opUser)
	}
	if iss := os.Getenv("ANSIBLEHUB_OIDC_ISSUER"); iss != "" && os.Getenv("ANSIBLEHUB_OIDC_CLIENT_ID") != "" && os.Getenv("ANSIBLEHUB_OIDC_CLIENT_SECRET") != "" {
		prov, err := oidc.New(context.Background(), oidc.Config{
			Issuer:       iss,
			ClientID:     os.Getenv("ANSIBLEHUB_OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("ANSIBLEHUB_OIDC_CLIENT_SECRET"),
			RedirectURL:  *baseURL + "/api/v1/auth/callback",
			GroupClaim:   envOrDefault("ANSIBLEHUB_OIDC_GROUP_CLAIM", "groups"),
			Scopes:       splitList(os.Getenv("ANSIBLEHUB_OIDC_SCOPES")),
		})
		if err != nil {
			log.Error("failed to init OIDC", "error", err)
			os.Exit(1)
		}
		oa.OIDC = prov
		log.Info("OIDC enabled", "issuer", iss, "group_claim", os.Getenv("ANSIBLEHUB_OIDC_GROUP_CLAIM"))
	}
	if oa.Basic == nil && oa.OIDC == nil {
		log.Warn("no operator authentication configured — UI/API is OPEN (dev mode)")
	}

	srv := api.New(st, log, oa, secure)
	handler := srv.Handler()

	if *uiDir != "" {
		handler = uiMiddleware(handler, *uiDir)
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Info("listening")
	if *tlsCert != "" && *tlsKey != "" {
		if err := httpServer.ListenAndServeTLS(*tlsCert, *tlsKey); err != nil && err != http.ErrServerClosed {
			log.Error("server failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func envOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
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

func uiMiddleware(next http.Handler, uiDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(uiDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")

		// Real asset that exists on disk (js/css/…) → serve it normally.
		target := filepath.Join(uiDir, filepath.FromSlash(strings.TrimPrefix(r.URL.Path, "/")))
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		// A missing asset (stale hashed bundle, etc.) is a genuine 404.
		if strings.Contains(filepath.Base(r.URL.Path), ".") {
			http.NotFound(w, r)
			return
		}
		// SPA fallback: any other path (client route) renders the app.
		http.ServeFile(w, r, filepath.Join(uiDir, "index.html"))
	})
}
