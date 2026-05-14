package server

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// WithCORS restricts cross-origin access to same-origin only.
//
// The binary serves both the API and the web UI on the same host:port, so
// legitimate browser requests share that origin. Any other origin — including
// other localhost ports and file:// pages — is rejected. Non-browser callers
// (no Origin header) are allowed: they already need filesystem access to read
// the config file, so the API gates nothing extra for them.
func WithCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostIsLoopback(r.Host) {
			// DNS rebinding defence: refuse requests whose Host header
			// does not resolve to a loopback address.
			http.Error(w, "non-loopback host blocked", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if !sameOrigin(origin, r.Host) {
			http.Error(w, "cross-origin request blocked", http.StatusForbidden)
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hostIsLoopback(host string) bool {
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	h = strings.ToLower(h)
	if h == "localhost" || h == "team-mem" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	return u.Host == host
}
