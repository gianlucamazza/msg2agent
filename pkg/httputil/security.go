// Package httputil provides shared HTTP middleware helpers.
package httputil

import (
	"net/http"
	"strings"
)

// SecurityHeaders wraps an http.Handler and sets security-related response headers.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// CSP wraps an http.Handler with a strict Content-Security-Policy header.
// extraScriptSrc appends additional script-src sources (e.g. SHA-256 hashes for
// Astro's inline hydration runtime: "'sha256-abc123...'").
func CSP(next http.Handler, extraScriptSrc ...string) http.Handler {
	scriptSrc := "'self'"
	if len(extraScriptSrc) > 0 {
		scriptSrc += " " + strings.Join(extraScriptSrc, " ")
	}
	policy := "default-src 'self'; script-src " + scriptSrc + "; style-src 'self'; " +
		"img-src 'self' data:; connect-src 'self'; object-src 'none'; " +
		"base-uri 'self'; frame-ancestors 'none'; form-action 'self'"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", policy)
		next.ServeHTTP(w, r)
	})
}
