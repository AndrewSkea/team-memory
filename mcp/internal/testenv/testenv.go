// Package testenv builds a configured App backed by a fake GitHub for tests.
package testenv

import (
	"context"
	"io"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
	"github.com/AndrewSkea/team-memory/mcp/llm"
)

type Env struct {
	Fake   *fakegh.Server
	App    *app.App
	APIURL string
}

// New returns an App. If configured, setup has run against a fresh fake
// repo acme/kb as user alice.
func New(t *testing.T, runner llm.Runner, configured bool) *Env {
	t.Helper()
	t.Setenv("TEAM_MEMORY_NO_SPAWN", "1")
	fake := fakegh.New("acme", "kb", "tok")
	fake.Login = "alice"
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	if runner == nil {
		runner = llm.Unavailable{}
	}
	dir := t.TempDir()
	a, err := app.Open(app.Options{
		ConfigPath: filepath.Join(dir, "config.json"),
		DataDir:    filepath.Join(dir, "data"),
		Runner:     runner,
		APIBase:    srv.URL,
		LogWriter:  io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured {
		if _, err := a.Setup(context.Background(), app.SetupInput{Token: "tok", Repo: "acme/kb"}); err != nil {
			t.Fatal(err)
		}
	}
	return &Env{Fake: fake, App: a, APIURL: srv.URL}
}
