package search

import (
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
)

var now = time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)

func mems() []memory.Memory {
	return []memory.Memory{
		{ID: "1", Title: "Payments API rate limits at 100 rps", Summary: "Retry 429 responses with jittered backoff", About: []string{"payments-api"}, LearnedIn: "acme/billing", Created: now.AddDate(0, 0, -20)},
		{ID: "2", Title: "Kafka consumer lag alerts", Summary: "Lag over 10k pages on-call", About: []string{"kafka"}, Created: now.AddDate(0, 0, -5)},
		{ID: "3", Title: "Old payments retry advice", Summary: "Retry immediately", About: []string{"payments-api"}, Status: memory.StatusSuperseded, Created: now.AddDate(-1, 0, 0)},
		{ID: "4", Title: "My personal reminder about payments", Scope: memory.ScopePersonal, Author: "bob", Created: now},
		{ID: "5", Title: "Deploy checklist", Body: "run migrations before deploy", Type: "howto", Created: now.AddDate(0, -2, 0)},
	}
}

func ids(rs []Result) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Memory.ID)
	}
	return out
}

func TestTextSearch(t *testing.T) {
	idx := Build(mems())
	rs := idx.Search(Query{Text: "how do we handle payments rate limits?", Now: now})
	if len(rs) == 0 || rs[0].Memory.ID != "1" {
		t.Fatalf("got %v", ids(rs))
	}
	if !rs[0].Relevant() {
		t.Fatalf("top result should be relevant: %+v", rs[0])
	}
	for _, r := range rs {
		if r.Memory.ID == "3" || r.Memory.ID == "4" {
			t.Fatalf("inactive/personal leaked: %v", ids(rs))
		}
	}
}

func TestEntityRecallAcrossRepos(t *testing.T) {
	idx := Build(mems())
	// Different repo, no keyword overlap, but the prompt names the entity.
	rs := idx.Search(Query{Text: "checkout is timing out", StrongEntities: []string{"Payments API"}, Repo: "acme/checkout-web", Now: now})
	if len(rs) != 1 || rs[0].Memory.ID != "1" || !rs[0].Relevant() {
		t.Fatalf("got %+v", rs)
	}
	// Ambient (weak) entity alone does not make it auto-inject-worthy.
	rs = idx.Search(Query{WeakEntities: []string{"payments-api"}, Now: now})
	if len(rs) != 1 || rs[0].Relevant() {
		t.Fatalf("weak: %+v", rs)
	}
}

func TestFilters(t *testing.T) {
	idx := Build(mems())
	if rs := idx.Search(Query{Text: "personal reminder payments", Viewer: "bob", Now: now}); len(rs) == 0 || rs[0].Memory.ID != "4" {
		t.Fatalf("author should see personal: %v", ids(rs))
	}
	if rs := idx.Search(Query{Text: "deploy", Type: "howto", Now: now}); len(rs) != 1 || rs[0].Memory.ID != "5" {
		t.Fatalf("type filter: %v", ids(rs))
	}
	if rs := idx.Search(Query{Text: "payments retry", IncludeAll: true, Now: now}); len(rs) < 2 {
		t.Fatalf("include all: %v", ids(rs))
	}
	if rs := idx.Search(Query{Text: "payments", Exclude: map[string]bool{"1": true}, Now: now}); len(rs) != 0 {
		t.Fatalf("exclude: %v", ids(rs))
	}
	if rs := idx.Search(Query{Text: "zebra unicorn", Now: now}); len(rs) != 0 {
		t.Fatalf("no match should be empty: %v", ids(rs))
	}
}

func TestTokenize(t *testing.T) {
	got := Tokenize("The Payments-API retries 429s; policies!")
	want := []string{"payment", "api", "retry", "429", "policy"}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%v", got)
		}
	}
}
