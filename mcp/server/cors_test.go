package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOrigin(t *testing.T) {
	cases := []struct {
		origin string
		host   string
		want   bool
	}{
		{"http://127.0.0.1:7438", "127.0.0.1:7438", true},
		{"https://127.0.0.1:7438", "127.0.0.1:7438", true},
		{"http://localhost:7438", "localhost:7438", true},
		{"http://127.0.0.1:3000", "127.0.0.1:7438", false},
		{"http://localhost:3000", "127.0.0.1:7438", false},
		{"http://evil.com", "127.0.0.1:7438", false},
		{"null", "127.0.0.1:7438", false},
		{"not a url", "127.0.0.1:7438", false},
	}
	for _, c := range cases {
		got := sameOrigin(c.origin, c.host)
		if got != c.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v", c.origin, c.host, got, c.want)
		}
	}
}

func TestWithCORS_BlocksCrossOrigin(t *testing.T) {
	h := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7438/v1/config", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for cross-origin, got %d", rr.Code)
	}
}

func TestWithCORS_AllowsSameOrigin(t *testing.T) {
	h := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7438/v1/config", nil)
	req.Header.Set("Origin", "http://127.0.0.1:7438")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for same-origin, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:7438" {
		t.Errorf("missing CORS allow-origin header")
	}
}

func TestWithCORS_AllowsNoOrigin(t *testing.T) {
	h := WithCORS(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7438/health", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 for no Origin, got %d", rr.Code)
	}
}
