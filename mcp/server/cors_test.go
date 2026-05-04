package server

import "testing"

func TestOriginAllowed(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"", true},
		{"null", true},
		{"http://localhost", true},
		{"http://localhost:8080", true},
		{"http://127.0.0.1", true},
		{"http://127.0.0.1:7438", true},
		{"http://localhostevil.com", false},
		{"http://127.0.0.10:8080", false},
		{"https://localhost", false},
		{"http://example.com", false},
		{"not a url", false},
	}
	for _, c := range cases {
		got := originAllowed(c.origin)
		if got != c.want {
			t.Errorf("originAllowed(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}
