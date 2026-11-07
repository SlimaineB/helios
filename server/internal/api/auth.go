package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/slim/helios/server/internal/auth"
	"github.com/slim/helios/server/internal/model"
	"github.com/slim/helios/server/internal/oidc"
	"github.com/slim/helios/server/internal/rbac"
)

const (
	cookieName          = "helios_sess"
	stateCookie         = "helios_oauth_state"
	ctxPrincipal ctxKey = iota + 100
)

// OpAuth is the operator authentication bundle handed to the API.
// All fields optional — with nil Basic and nil OIDC the operator surface is
// open (dev mode) and only agents are authenticated.
type OpAuth struct {
	// Basic validates the Authorization header against the local admin user.
	Basic func(header string) bool
	// BasicUser is the display name for the local admin principal.
	BasicUser string
	// RBAC is the policy set evaluated on every operator action.
	RBAC *rbac.RBAC
	// Sessions is the browser session store for OIDC logins.
	Sessions auth.SessionStore
	// OIDC is the provider; nil disables SSO login.
	OIDC *oidc.Provider
	// BaseURL is the external base URL used to build the OIDC callback URL.
	BaseURL string
}

// principal is the authenticated operator identity.
type principal struct {
	Sub    string
	Email  string
	Name   string
	Groups []string
	Roles  []string
}

func (p *principal) label() string {
	if p.Name != "" {
		return p.Name
	}
	if p.Email != "" {
		return p.Email
	}
	return p.Sub
}

// op guards an operator route: resolve the caller (basic admin or OIDC
// session) and enforce the RBAC operation "resource.action".
func (s *Server) op(operation string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := s.resolvePrincipal(r)
		if !ok {
			// No WWW-Authenticate header on purpose: it would raise the
			// browser's native basic-auth dialog instead of our login screen.
			writeErr(w, http.StatusUnauthorized, "authentication required")
			return
		}
		res, act, _ := strings.Cut(operation, ".")
		if s.opAuth != nil && s.opAuth.RBAC != nil && !s.opAuth.RBAC.AuthorizeAny(pr.Groups, pr.Roles, res, act) {
			writeErr(w, http.StatusForbidden, fmt.Sprintf("forbidden: missing %s", operation))
			return
		}
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next(sw, r.WithContext(context.WithValue(r.Context(), ctxPrincipal, pr)))
		if mutating(r.Method) {
			entry := model.AuditEntry{
				User:     pr.label(),
				Method:   r.Method,
				Path:     r.URL.Path,
				Status:   sw.status,
				RemoteIP: remoteIP(r),
			}
			go func() {
				if err := s.store.AppendAudit(context.Background(), entry); err != nil {
					s.log.Warn("audit write failed", "error", err)
				}
			}()
		}
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	return w.ResponseWriter.Write(b)
}

func mutating(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// resolvePrincipal authenticates the request via an OIDC session cookie or the
// local admin Basic credentials, resolving roles from RBAC group bindings.
// The session cookie is checked first so a deliberately-started SSO session
// wins over a stale cached Basic header (e.g. leftover from the native dialog).
func (s *Server) resolvePrincipal(r *http.Request) (*principal, bool) {
	oa := s.opAuth
	if oa == nil {
		return &principal{Name: "dev", Roles: []string{"role:admin"}}, true
	}
	if oa.Sessions != nil {
		if c, err := r.Cookie(cookieName); err == nil && c.Value != "" {
			if sess, ok := oa.Sessions.Get(r.Context(), c.Value); ok {
				roles := []string(nil)
				if oa.RBAC != nil {
					roles = oa.RBAC.RolesForGroups(sess.Groups)
				}
				return &principal{
					Sub: sess.Sub, Email: sess.Email, Name: sess.Name,
					Groups: sess.Groups, Roles: roles,
				}, true
			}
		}
	}
	if oa.Basic != nil && oa.Basic(r.Header.Get("Authorization")) {
		name := oa.BasicUser
		if name == "" {
			name = "admin"
		}
		return &principal{Name: name, Roles: []string{"role:admin"}}, true
	}
	return nil, false
}

// handleRealm reports the auth surface for the UI (is there an SSO login?).
func (s *Server) handleRealm(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"oidc": s.opAuth != nil && s.opAuth.OIDC != nil}
	if s.opAuth != nil && s.opAuth.Basic != nil {
		resp["basic_admin"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleLogin starts the OIDC authorization-code + PKCE flow.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.opAuth == nil || s.opAuth.OIDC == nil {
		writeErr(w, http.StatusNotFound, "OIDC login is not configured")
		return
	}
	state, err := auth.NewToken()
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	url, verifier, nonce, err := s.opAuth.OIDC.AuthURL(state)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	http.SetCookie(w, s.cookie(stateCookie, state+"."+verifier+"."+nonce, 10*60))
	http.Redirect(w, r, url, http.StatusFound)
}

// handleCallback exchanges the code, creates a session and logs the user in.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if s.opAuth == nil || s.opAuth.OIDC == nil {
		writeErr(w, http.StatusNotFound, "OIDC login is not configured")
		return
	}
	code, state := r.URL.Query().Get("code"), r.URL.Query().Get("state")
	c, err := r.Cookie(stateCookie)
	if err != nil || state == "" || code == "" {
		writeErr(w, http.StatusBadRequest, "missing oauth state or code")
		return
	}
	sts := strings.SplitN(c.Value, ".", 3)
	if len(sts) != 3 || sts[0] != state {
		writeErr(w, http.StatusBadRequest, "oauth state mismatch")
		return
	}
	pr, err := s.opAuth.OIDC.Exchange(r.Context(), code, sts[1], sts[2])
	if err != nil {
		s.log.Warn("oidc token exchange failed", "error", err)
		writeErr(w, http.StatusBadRequest, "oidc login failed")
		return
	}
	tok, err := s.opAuth.Sessions.Create(r.Context(), pr.Sub, pr.Email, pr.Name, pr.Groups, nil)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	ttl := int(s.opAuth.Sessions.TTL() / time.Second)
	http.SetCookie(w, s.cookie(cookieName, tok, ttl))
	http.SetCookie(w, s.cookie(stateCookie, "", -1))
	http.Redirect(w, r, s.opAuth.BaseURL+"/", http.StatusFound)
}

// handleLogout clears the browser session. Returns JSON so the UI can call it
// with fetch() without tripping over a redirect.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil && s.opAuth != nil && s.opAuth.Sessions != nil {
		s.opAuth.Sessions.Delete(r.Context(), c.Value)
	}
	http.SetCookie(w, s.cookie(cookieName, "", -1))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe reports the current operator identity + effective permissions.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	pr, ok := s.resolvePrincipal(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	resp := map[string]any{
		"name":        pr.label(),
		"email":       pr.Email,
		"groups":      pr.Groups,
		"roles":       pr.Roles,
		"permissions": []string{},
	}
	if s.opAuth != nil && s.opAuth.RBAC != nil {
		resp["permissions"] = s.opAuth.RBAC.Permissions(pr.Groups, pr.Roles)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies,
	}
}
