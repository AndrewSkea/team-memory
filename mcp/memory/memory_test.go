package memory

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRenderParseRoundTrip(t *testing.T) {
	created := time.Date(2026, 9, 27, 15, 30, 12, 0, time.UTC)
	m := Memory{
		ID:         "20260927-153012-a1b2c3",
		Title:      "Payments API rate limits at 100 rps",
		Type:       TypeGotcha,
		Summary:    "Retry 429s with jittered backoff.",
		About:      []string{"payments-api"},
		Tags:       []string{"rate-limit", "retries"},
		Author:     "alice",
		LearnedIn:  "acme/billing-service",
		Source:     "session-end",
		Created:    created,
		Confidence: 0.9,
		Body:       "Details: the limit is per API key.\n\n- use jitter",
	}
	m.Path = PathFor(m)
	if m.Path != "memories/2026/09/20260927-153012-a1b2c3-payments-api-rate-limits-at-100-rps.md" {
		t.Fatalf("path = %s", m.Path)
	}
	out, err := Render(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# Payments API rate limits at 100 rps") {
		t.Fatalf("missing heading:\n%s", out)
	}
	got, err := Parse(m.Path, out)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("round trip mismatch\n got %+v\nwant %+v", got, m)
	}
}

func TestParseRejectsNoFrontmatter(t *testing.T) {
	if _, err := Parse("x.md", "# hi"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Parse("x.md", "---\ntitle: x\n---\nbody"); err == nil {
		t.Fatal("expected missing id error")
	}
}

func TestParseCRLFAndHandEdited(t *testing.T) {
	src := "---\r\nid: abc\r\ntitle: T\r\n---\r\nSome body\r\n"
	m, err := Parse("m.md", src)
	if err != nil {
		t.Fatal(err)
	}
	if m.Body != "Some body" || m.Type != TypeNote || !m.Active() {
		t.Fatalf("%+v", m)
	}
}

func TestSlugAndID(t *testing.T) {
	if s := Slug("  Hello, World! — ünïcode  "); s != "hello-world-n-code" {
		t.Fatalf("slug %q", s)
	}
	if Slug("!!!") != "memory" {
		t.Fatal("empty slug fallback")
	}
	id := NewID(time.Now())
	if !ValidID(id) {
		t.Fatalf("invalid id %s", id)
	}
	if ValidID("../etc") {
		t.Fatal("accepted bad id")
	}
}

func TestParseLegacy(t *testing.T) {
	src := `# GENERAL

### Entry: 2026-01-15T10:00:00Z — Use JWT for auth
**Scope:** Team
**Type:** General
**Tags:** auth;jwt
**Source:** UI
**Summary:** We use JWTs signed with RS256.
**Bullets:** - a ; - b
**Full:**
Longer text here.

### Decision: Adopt Postgres
**Date:** 2026-02-01
**Status:** Accepted

**Decision:**
We will use Postgres.
---
`
	got := ParseLegacy("GENERAL.md", src)
	if len(got) != 2 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	a := got[0]
	if a.Title != "Use JWT for auth" || a.Summary != "We use JWTs signed with RS256." ||
		!reflect.DeepEqual(a.Tags, []string{"auth", "jwt"}) || a.Created.IsZero() || !a.Legacy {
		t.Fatalf("entry 0: %+v", a)
	}
	if !strings.Contains(a.Body, "Longer text here.") {
		t.Fatalf("body: %q", a.Body)
	}
	b := got[1]
	if b.Title != "Adopt Postgres" || b.Type != TypeDecision || b.Created.IsZero() {
		t.Fatalf("entry 1: %+v", b)
	}
	if a.ID == b.ID {
		t.Fatal("ids must differ")
	}
}
