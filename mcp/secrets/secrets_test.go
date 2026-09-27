package secrets

import (
	"strings"
	"testing"
)

func TestRedactsKnownFormats(t *testing.T) {
	cases := map[string]string{
		"aws":        "key AKIAIOSFODNN7EXAMPLE in env",
		"github":     "token ghp_abcdefghijklmnopqrstuvwxyz0123456789 ok",
		"github-pat": "github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKL",
		"slack":      "xoxb-1234567890-abcdefghij",
		"stripe":     "sk_live_abcdefghijklmnop1234",
		"anthropic":  "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"jwt":        "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
		"url":        "postgres://admin:hunter2secret@db.internal:5432/app",
		"assign":     `password = "S3cretValue!"`,
		"pem":        "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		"header":     "Authorization: Bearer abcdef0123456789abcdef",
	}
	for name, in := range cases {
		out, f := Redact(in)
		if len(f) == 0 || !strings.Contains(out, Mask) {
			t.Errorf("%s: not redacted: %q", name, out)
		}
	}
	out, _ := Redact("postgres://admin:hunter2secret@db.internal:5432/app")
	if !strings.Contains(out, "admin:") || !strings.Contains(out, "@db.internal") || strings.Contains(out, "hunter2") {
		t.Errorf("url redaction should keep user/host: %q", out)
	}
}

func TestLeavesPlaceholdersAndProse(t *testing.T) {
	for _, in := range []string{
		"password = ${DB_PASSWORD}",
		"api_key: <your-key-here>",
		"secret = process.env.SECRET",
		"password: changeme123",
		"We rotated the password yesterday and the API key is in Vault.",
		"token = getToken()",
		"The payments API rate-limits at 100 rps.",
	} {
		out, f := Redact(in)
		if len(f) != 0 {
			t.Errorf("false positive on %q -> %q", in, out)
		}
	}
}

func TestOverlappingMatches(t *testing.T) {
	in := "api_key=sk-ant-api03-abcdefghijklmnopqrstuvwxyz and more"
	out, _ := Redact(in)
	if strings.Count(out, Mask) != 1 || strings.Contains(out, "sk-ant") {
		t.Fatalf("got %q", out)
	}
}
