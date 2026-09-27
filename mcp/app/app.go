// Package app is team-memory's core: it ties together the config, the local
// mirror of the memory repo, search, the capture pipeline, recall and the
// review queue. The MCP server, hooks, HTTP API and CLI are thin layers over
// it.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/entities"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/store"
)

// ErrNotConfigured is returned by operations that need the memory repo.
var ErrNotConfigured = errors.New("team-memory is not set up yet — run `team-memory-mcp install` or open the web UI (http://127.0.0.1:7438) and complete Setup")

type Options struct {
	ConfigPath string // default config.DefaultPath()
	DataDir    string // default config.DataDir()
	Runner     llm.Runner
	APIBase    string // GitHub API override (tests)
	Now        func() time.Time
	LogWriter  io.Writer // default: data/logs/team-memory.log
}

type App struct {
	CfgPath string
	DataDir string
	Runner  llm.Runner
	Now     func() time.Time
	Log     *log.Logger

	apiBase string

	mu     sync.Mutex
	cfg    config.Config
	client *gh.Client
	store  *store.Store

	cacheMu   sync.Mutex
	snap      *store.Snapshot
	idx       *search.Index
	matcher   *entities.Matcher
	snapKey   string
	snapAt    time.Time
	snapDirty bool
}

// Open loads config (a missing config is fine: the app runs unconfigured).
func Open(o Options) (*App, error) {
	if o.ConfigPath == "" {
		o.ConfigPath = config.DefaultPath()
	}
	if o.DataDir == "" {
		o.DataDir = config.DataDir()
	}
	if o.Runner == nil {
		o.Runner = llm.Auto()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
		return nil, err
	}
	w := o.LogWriter
	if w == nil {
		w = openLog(filepath.Join(o.DataDir, "logs", "team-memory.log"))
	}
	a := &App{
		CfgPath: o.ConfigPath,
		DataDir: o.DataDir,
		Runner:  o.Runner,
		Now:     o.Now,
		Log:     log.New(w, "", log.LstdFlags),
		apiBase: o.APIBase,
	}
	if err := a.Reload(); err != nil {
		return nil, err
	}
	return a, nil
}

// Reload re-reads the config file.
func (a *App) Reload() error {
	cfg, err := config.Read(a.CfgPath)
	if err != nil && !config.IsMissing(err) {
		return fmt.Errorf("read config %s: %w", a.CfgPath, err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = cfg
	a.client, a.store = nil, nil
	if cfg.Configured() {
		a.client = gh.NewClient(cfg, a.apiBase)
		a.store = store.New(a.DataDir, cfg.Owner, cfg.Repo, a.client)
	}
	a.invalidate()
	return nil
}

// Config returns the current config.
func (a *App) Config() config.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

// Configured reports whether the memory repo is set up.
func (a *App) Configured() bool { return a.Config().Configured() }

func (a *App) parts() (*gh.Client, *store.Store, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.store == nil {
		return nil, nil, ErrNotConfigured
	}
	return a.client, a.store, nil
}

// Client returns the GitHub client.
func (a *App) Client() (*gh.Client, error) {
	c, _, err := a.parts()
	return c, err
}

func (a *App) invalidate() {
	a.cacheMu.Lock()
	a.snapDirty = true
	a.cacheMu.Unlock()
}

// Sync refreshes the local mirror from GitHub.
func (a *App) Sync(ctx context.Context) (bool, error) {
	_, st, err := a.parts()
	if err != nil {
		return false, err
	}
	changed, err := st.Sync(ctx)
	if changed {
		a.invalidate()
	}
	return changed, err
}

// EnsureFresh syncs if the mirror is older than maxAge. Network failures are
// logged, not returned: callers carry on with the local copy (offline mode).
func (a *App) EnsureFresh(ctx context.Context, maxAge time.Duration) {
	_, st, err := a.parts()
	if err != nil {
		return
	}
	if s := st.State(); !s.SyncedAt.IsZero() && a.Now().Sub(s.SyncedAt) < maxAge {
		return
	}
	if _, err := a.Sync(ctx); err != nil {
		a.Log.Printf("sync: %v (using local copy)", err)
	}
}

// SyncState reports the mirror's state.
func (a *App) SyncState() store.SyncState {
	_, st, err := a.parts()
	if err != nil {
		return store.SyncState{}
	}
	return st.State()
}

// View is a consistent, cached view of the memory repo.
type View struct {
	Snap    *store.Snapshot
	Index   *search.Index
	Matcher *entities.Matcher
}

// View returns the parsed mirror, reloading it when it changed on disk.
func (a *App) View() (*View, error) {
	_, st, err := a.parts()
	if err != nil {
		return nil, err
	}
	key := st.State().SHA
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	if a.snap != nil && !a.snapDirty && key == a.snapKey && a.Now().Sub(a.snapAt) < 30*time.Second {
		return &View{a.snap, a.idx, a.matcher}, nil
	}
	snap, err := st.Load()
	if err != nil {
		return nil, err
	}
	var about []string
	for _, m := range snap.Memories {
		about = append(about, m.About...)
	}
	a.snap, a.idx, a.matcher = snap, search.Build(snap.Memories), entities.NewMatcher(snap.Entities, about)
	a.snapKey, a.snapAt, a.snapDirty = key, a.Now(), false
	return &View{a.snap, a.idx, a.matcher}, nil
}

// Mode is the capture mode in force for this user.
func (a *App) Mode(pol policy.Policy) policy.Mode {
	return pol.Effective(a.Config().Mode)
}

// Viewer is the current user's GitHub login (for personal memories).
func (a *App) Viewer() string { return a.Config().Username }

// Get finds a memory by id.
func (a *App) Get(id string) (memory.Memory, error) {
	v, err := a.View()
	if err != nil {
		return memory.Memory{}, err
	}
	m, ok := v.Snap.ByID(id)
	if !ok {
		return memory.Memory{}, fmt.Errorf("no memory with id %q", id)
	}
	return m, nil
}

// SearchInput is a user-initiated search.
type SearchInput struct {
	Query      string
	About      []string
	Type       string
	Limit      int
	IncludeAll bool
}

// Search runs a user-initiated search (web UI, MCP recall, CLI).
func (a *App) Search(in SearchInput) ([]search.Result, error) {
	v, err := a.View()
	if err != nil {
		return nil, err
	}
	strong := v.Matcher.Match(in.Query)
	for _, x := range in.About {
		strong = append(strong, entities.CanonicalName(x))
	}
	q := search.Query{
		Text:           in.Query,
		StrongEntities: strong,
		Type:           in.Type,
		Viewer:         a.Viewer(),
		Limit:          in.Limit,
		IncludeAll:     in.IncludeAll,
		Now:            a.Now(),
	}
	if in.Query == "" && len(strong) == 0 {
		// Browse mode: newest first.
		var out []search.Result
		for _, m := range v.Snap.Memories {
			if (!in.IncludeAll && !m.Active()) || (in.Type != "" && m.Type != in.Type) {
				continue
			}
			if m.Scope == memory.ScopePersonal && m.Author != a.Viewer() {
				continue
			}
			out = append(out, search.Result{Memory: m})
			if q.Limit > 0 && len(out) >= q.Limit {
				break
			}
		}
		return out, nil
	}
	return v.Index.Search(q), nil
}
