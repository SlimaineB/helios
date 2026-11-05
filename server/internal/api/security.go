package api

import (
	"net/http"
	"strings"
)

const contentSecurityPolicy = "default-src 'self'; base-uri 'self'; frame-ancestors 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'"

// SetSecurityHeaders applies the browser hardening headers shared by the API
// and the embedded UI. hsts should be true when the public base URL is HTTPS.
func SetSecurityHeaders(w http.ResponseWriter, r *http.Request, hsts bool) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	if hsts {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
}
