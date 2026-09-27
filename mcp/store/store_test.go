package store_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

func newStore(t *testing.T) (*fakegh.Server, *store.Store) {
	t.Helper()
	fake := fakegh.New("acme", "kb", "tok")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	c := gh.NewClient(config.Config{Token: "tok", Owner: "acme", Repo: "kb"}, srv.URL)
	return fake, store.New(t.TempDir(), "acme", "kb", c)
}

func TestSyncEmptyRepo(t *testing.T) {
	_, s := newStore(t)
	if _, err := s.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load()
	if err != nil || len(snap.Memories) != 0 || snap.Policy.DefaultMode != policy.AI {
		t.Fatalf("%+v %v", snap, err)
	}
}

func TestSyncLoadCreateUpdate(t *testing.T) {
	fake, s := newStore(t)
	ctx := context.Background()
	existing := memory.Memory{ID: "20260101-000000-aaaaaa", Title: "Old fact", Type: "fact", Created: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	existing.Path = memory.PathFor(existing)
	body, _ := memory.Render(existing)
	fake.Seed(map[string]string{
		existing.Path:               body,
		"GENERAL.md":                "# GENERAL\n\n### Entry: 2025-12-01T00:00:00Z — Legacy thing\n**Summary:** legacy summary\n",
		"README.md":                 "# not a memory\n### Heading\n",
		".team-memory/policy.yml":   "minimum_mode: review\n",
		".team-memory/entities.yml": "entities:\n  - name: kafka\n",
		"memories/2026/01/bad.md":   "no frontmatter",
		"assets/logo.png":           "binary",
	})
	changed, err := s.Sync(ctx)
	if err != nil || !changed {
		t.Fatalf("sync: %v %v", changed, err)
	}
	if changed, _ := s.Sync(ctx); changed {
		t.Fatal("second sync should be a no-op")
	}
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Memories) != 2 || !snap.HasLegacy || snap.Policy.MinimumMode != policy.Review ||
		len(snap.Entities.Entities) != 1 || len(snap.Problems) != 1 {
		t.Fatalf("snapshot: %+v", snap)
	}

	// Create is visible locally immediately (write-through) and remotely.
	m := memory.Memory{ID: memory.NewID(time.Now()), Title: "New gotcha", Type: "gotcha", Created: time.Now().UTC()}
	m, err = s.Create(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fake.File(m.Path); !ok {
		t.Fatal("not written remotely")
	}
	snap, _ = s.Load()
	if _, ok := snap.ByID(m.ID); !ok {
		t.Fatal("write-through missing")
	}
	if _, err := s.Create(ctx, m); err == nil {
		t.Fatal("duplicate create should fail")
	}

	// Update (supersede) round-trips.
	_, err = s.UpdateMemory(ctx, existing.Path, "supersede", func(mm *memory.Memory) error {
		mm.Status = memory.StatusSuperseded
		mm.SupersededBy = m.ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := fake.File(existing.Path)
	if !strings.Contains(got, "status: superseded") {
		t.Fatalf("remote not updated:\n%s", got)
	}
	// After a full re-sync the state is consistent.
	if _, err := s.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	snap, _ = s.Load()
	old, _ := snap.ByID(existing.ID)
	if old.Active() || old.SupersededBy != m.ID {
		t.Fatalf("%+v", old)
	}
}

func TestPutIfMissing(t *testing.T) {
	fake, s := newStore(t)
	ctx := context.Background()
	created, err := s.PutIfMissing(ctx, ".team-memory/policy.yml", "a", "seed")
	if err != nil || !created {
		t.Fatal(err)
	}
	created, err = s.PutIfMissing(ctx, ".team-memory/policy.yml", "b", "seed")
	if err != nil || created {
		t.Fatal("second put should be skipped")
	}
	if got, _ := fake.File(".team-memory/policy.yml"); got != "a" {
		t.Fatal(got)
	}
}
