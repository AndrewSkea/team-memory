package github_test

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/config"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
)

func setup(t *testing.T) (*fakegh.Server, *gh.Client) {
	t.Helper()
	fake := fakegh.New("o", "r", "tok")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return fake, gh.NewClient(config.Config{Token: "tok", Owner: "o", Repo: "r"}, srv.URL)
}

func TestGetFile(t *testing.T) {
	fake, c := setup(t)
	f, err := c.GetFile(context.Background(), "INDEX.md")
	if err != nil || f.Exists {
		t.Fatalf("missing file: %+v %v", f, err)
	}
	long := strings.Repeat("hello world ", 50)
	fake.Seed(map[string]string{"INDEX.md": long})
	f, err = c.GetFile(context.Background(), "INDEX.md")
	if err != nil || !f.Exists || f.Content != long || f.SHA == "" {
		t.Fatalf("unexpected: %+v %v", f, err)
	}
}

func TestEmptyRepoAndHead(t *testing.T) {
	fake, c := setup(t)
	if _, err := c.HeadSHA(context.Background()); !errors.Is(err, gh.ErrEmptyRepo) {
		t.Fatalf("want ErrEmptyRepo, got %v", err)
	}
	fake.Seed(map[string]string{"a.md": "x"})
	sha, err := c.HeadSHA(context.Background())
	if err != nil || len(sha) != 40 {
		t.Fatalf("sha %q %v", sha, err)
	}
	rc, err := c.Tarball(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if len(b) < 20 {
		t.Fatal("tarball too small")
	}
}

func TestPutFileCreateAndConflict(t *testing.T) {
	fake, c := setup(t)
	ctx := context.Background()
	if err := c.PutFile(ctx, "memories/2026/09/a.md", "one", "", "msg"); err != nil {
		t.Fatal(err)
	}
	// Creating again without sha is a conflict.
	if err := c.PutFile(ctx, "memories/2026/09/a.md", "two", "", "msg"); !errors.Is(err, gh.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	// Stale sha is a conflict.
	if err := c.PutFile(ctx, "memories/2026/09/a.md", "two", "deadbeef", "msg"); !errors.Is(err, gh.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	if got, _ := fake.File("memories/2026/09/a.md"); got != "one" {
		t.Fatalf("content %q", got)
	}
}

func TestUpdateRetriesAndAppends(t *testing.T) {
	fake, c := setup(t)
	fake.Seed(map[string]string{".team-memory/entities.yml": "a\n"})
	calls := 0
	err := c.Update(context.Background(), ".team-memory/entities.yml", "m", func(cur string, exists bool) (string, error) {
		calls++
		if calls == 1 {
			// Simulate a teammate writing between our read and write.
			fake.Seed(map[string]string{".team-memory/entities.yml": cur + "b\n"})
		}
		return cur + "c\n", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := fake.File(".team-memory/entities.yml")
	if got != "a\nb\nc\n" || calls != 2 {
		t.Fatalf("got %q after %d calls", got, calls)
	}
}

func TestPathValidation(t *testing.T) {
	_, c := setup(t)
	for _, p := range []string{"../x.md", "/abs.md", "a b.md", "x.txt", ".team-memory/other.yml"} {
		if err := c.PutFile(context.Background(), p, "x", "", "m"); err == nil {
			t.Errorf("accepted %q", p)
		}
	}
	if err := gh.ValidatePath(".team-memory/policy.yml"); err != nil {
		t.Error(err)
	}
}

func TestAuthErrorsAreDescriptive(t *testing.T) {
	fake := fakegh.New("o", "r", "right")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := gh.NewClient(config.Config{Token: "wrong", Owner: "o", Repo: "r"}, srv.URL)
	_, err := c.GetRepo(context.Background())
	if !gh.IsStatus(err, 401) || !strings.Contains(err.Error(), "invalid or expired") {
		t.Fatalf("got %v", err)
	}
	c2 := gh.NewClient(config.Config{Owner: "o", Repo: "r"}, srv.URL)
	if _, err := c2.GetUser(context.Background()); err == nil || !strings.Contains(err.Error(), "no GitHub token") {
		t.Fatalf("got %v", err)
	}
}
