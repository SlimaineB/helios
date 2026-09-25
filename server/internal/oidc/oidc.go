// Package oidc implements the OpenID Connect "Authorization Code + PKCE"
// flow used to sign operators in (Okta, Keycloak, Dex-compatible issuers).
// Group membership is read from a configurable claim (Okta's `groups`).
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config describes the OIDC client. All fields except GroupClaim/Scopes are
// required.
type Config struct {
	Issuer       string // issuer discovery URL, e.g. https://acme.okta.com/oauth2/default
	ClientID     string
	ClientSecret string
	RedirectURL  string   // must be registered with the provider, e.g. https://hub/api/v1/auth/callback
	GroupClaim   string   // JWT/userinfo claim holding the group list (default "groups")
	Scopes       []string // default: openid profile email groups
	Timeout      time.Duration
}

// Principal is the authenticated identity handed to the RBAC layer.
type Principal struct {
	Sub    string
	Email  string
	Name   string
	Groups []string
}

// Provider is a ready-to-use OIDC client after discovery.
type Provider struct {
	cfg      Config
	oidc     *oidc.Provider
	oauth    *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

// New performs provider discovery and builds the token/idtoken verifiers.
func New(ctx context.Context, cfg Config) (*Provider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, fmt.Errorf("oidc: issuer, client_id and redirect_url are required")
	}
	if cfg.GroupClaim == "" {
		cfg.GroupClaim = "groups"
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email", "groups"}
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	prov, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery %s: %w", cfg.Issuer, err)
	}
	p := &Provider{
		cfg:  cfg,
		oidc: prov,
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     prov.Endpoint(),
			RedirectURL:  cfg.RedirectURL,
			Scopes:       cfg.Scopes,
		},
		verifier: prov.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
	}
	return p, nil
}

// AuthURL builds the provider login URL with PKCE. It returns the generated
// verifier the server must stash until the callback arrives.
func (p *Provider) AuthURL(state string) (url, verifier string, err error) {
	verifier, err = randomString()
	if err != nil {
		return "", "", err
	}
	challenge := base64.RawURLEncoding.EncodeToString(sha256Sum([]byte(verifier)))
	u := p.oauth.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	return u, verifier, nil
}

// Exchange trades the authorization code (with its PKCE verifier) for tokens
// and returns the verified principal, reading groups from GroupClaim.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (*Principal, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()

	tok, err := p.oauth.Exchange(ctx, code, oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		return nil, fmt.Errorf("oidc: token exchange: %w", err)
	}
	// Prefer the ID token (short-lived, signed); fall back to userinfo.
	idtok, ok := tok.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("oidc: no id_token in token response")
	}
	idt, err := p.verifier.Verify(ctx, idtok)
	if err != nil {
		return nil, fmt.Errorf("oidc: invalid id_token: %w", err)
	}

	var claims map[string]json.RawMessage
	if err := idt.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: parse claims: %w", err)
	}
	pr := &Principal{Sub: idt.Subject}
	if v, ok := rawString(claims["email"]); ok {
		pr.Email = v
	}
	if v, ok := rawString(claims["name"]); !ok {
		if v, ok := rawString(claims["preferred_username"]); ok {
			pr.Name = v
		}
	} else {
		pr.Name = v
	}
	pr.Groups = rawList(claims[p.cfg.GroupClaim])
	// Some providers put groups only in userinfo; fetch it when the ID token
	// lacks the claim.
	if len(pr.Groups) == 0 {
		if ui, err := p.oidc.UserInfo(ctx, p.oauth.TokenSource(ctx, tok)); err == nil {
			var uc map[string]json.RawMessage
			if err := ui.Claims(&uc); err == nil {
				pr.Groups = rawList(uc[p.cfg.GroupClaim])
				if pr.Email == "" {
					pr.Email = ui.Email
				}
			}
		}
	}
	return pr, nil
}

func rawString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, s != ""
}

func rawList(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	var out []string
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, g := range list {
			if g != "" && !strings.HasPrefix(g, "user/") {
				out = append(out, g)
			}
		}
		return out
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil && s != "" {
		out = append(out, s)
	}
	return out
}

func sha256Sum(b []byte) []byte {
	h := sha256.New()
	h.Write(b)
	return h.Sum(nil)
}

func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
