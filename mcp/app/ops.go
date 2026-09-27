package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/entities"
	gh "github.com/AndrewSkea/team-memory/mcp/github"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
	"github.com/AndrewSkea/team-memory/mcp/secrets"
	"github.com/AndrewSkea/team-memory/mcp/store"
	"github.com/AndrewSkea/team-memory/mcp/workspace"
)

// RememberInput is an explicit, user-initiated save. Because the user chose
// to share it, it is published directly regardless of capture mode (secrets
// are still redacted).
type RememberInput struct {
	Title    string   `json:"title"`
	Text     string   `json:"text"`
	Summary  string   `json:"summary"`
	Type     string   `json:"type"`
	About    []string `json:"about"`
	Tags     []string `json:"tags"`
	Scope    string   `json:"scope"` // team (default) | personal
	Source   string   `json:"source"`
	Cwd      string   `json:"cwd"`
	Classify bool     `json:"classify"` // let the LLM fill in title/summary/about/tags
}

// Remember publishes an explicit memory. Warnings describe redactions or
// classification fallbacks.
func (a *App) Remember(ctx context.Context, in RememberInput) (memory.Memory, []string, error) {
	return a.remember(ctx, in, nil)
}

// Supersede publishes a replacement for an existing memory.
func (a *App) Supersede(ctx context.Context, id string, in RememberInput) (memory.Memory, []string, error) {
	old, err := a.Get(id)
	if err != nil {
		return memory.Memory{}, nil, err
	}
	if old.Legacy {
		return memory.Memory{}, nil, fmt.Errorf("%s is a legacy entry in %s and is read-only; edit that file on GitHub", id, old.Path)
	}
	if len(in.About) == 0 {
		in.About = old.About
	}
	if in.Type == "" {
		in.Type = old.Type
	}
	return a.remember(ctx, in, []string{id})
}

func (a *App) remember(ctx context.Context, in RememberInput, supersedes []string) (memory.Memory, []string, error) {
	var warnings []string
	if !a.Configured() {
		return memory.Memory{}, nil, ErrNotConfigured
	}
	text := strings.TrimSpace(in.Text)
	if text == "" && strings.TrimSpace(in.Title) == "" {
		return memory.Memory{}, nil, errors.New("nothing to remember: text is empty")
	}
	if len(text) > 50_000 {
		return memory.Memory{}, nil, errors.New("text is too long (max 50,000 characters)")
	}
	v, err := a.View()
	if err != nil {
		return memory.Memory{}, nil, err
	}
	m := memory.Memory{
		ID:      memory.NewID(a.Now()),
		Title:   strings.TrimSpace(in.Title),
		Summary: strings.TrimSpace(in.Summary),
		Type:    normType(in.Type, memory.TypeNote),
		Body:    text,
		Tags:    cleanTags(in.Tags),
		Scope:   memory.ScopeTeam,
		Author:  a.Viewer(),
		Source:  in.Source,
		Created: a.Now().UTC(),
	}
	if strings.EqualFold(in.Scope, memory.ScopePersonal) {
		m.Scope = memory.ScopePersonal
	}
	if m.Source == "" {
		m.Source = "cli"
	}
	if in.Cwd != "" {
		if r, ok := workspace.FindRepo(in.Cwd); ok {
			m.LearnedIn = r.Slug()
		}
	}
	about := in.About
	var newEnts []entities.Entity
	if in.Classify && (m.Title == "" || m.Summary == "" || len(about) == 0) {
		c, err := a.classify(ctx, v, text, in.Type)
		if err != nil {
			warnings = append(warnings, "AI classification unavailable ("+shortErr(err)+"); saved as written")
		} else {
			if m.Title == "" {
				m.Title = c.Title
			}
			if m.Summary == "" {
				m.Summary = c.Summary
			}
			if len(about) == 0 {
				about = c.About
			}
			if len(m.Tags) == 0 {
				m.Tags = cleanTags(c.Tags)
			}
			if in.Type == "" {
				m.Type = normType(c.Type, memory.TypeNote)
			}
			newEnts = c.NewEntities
		}
	}
	if m.Title == "" {
		m.Title = heuristicTitle(text)
	}
	if m.Summary == "" && m.Body != "" {
		m.Summary = firstSentence(m.Body)
		if m.Summary == m.Title || m.Summary == m.Body {
			m.Summary = ""
		}
	}
	m.About = a.canonicalAbout(v, about)
	m.Supersedes = supersedes
	if redactMemory(&m) {
		warnings = append(warnings, "possible secrets were redacted before saving")
	}
	m.Path = memory.PathFor(m)
	extra := map[string]entities.Entity{}
	for _, e := range newEnts {
		extra[entities.CanonicalName(e.Name)] = e
	}
	m, err = a.publish(ctx, m, entitiesFor(m.About, extra))
	return m, warnings, err
}

type classification struct {
	Title       string            `json:"title"`
	Summary     string            `json:"summary"`
	About       []string          `json:"about"`
	Tags        []string          `json:"tags"`
	Type        string            `json:"type"`
	NewEntities []entities.Entity `json:"new_entities"`
}

func (a *App) classify(ctx context.Context, v *View, text, typeHint string) (classification, error) {
	var c classification
	ents := v.Snap.Entities.Entities
	if len(ents) > 300 {
		ents = ents[:300]
	}
	redacted, _ := secrets.Redact(text)
	input, _ := json.Marshal(map[string]any{"text": redacted, "type_hint": typeHint, "entities": ents})
	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	raw, err := a.Runner.Run(cctx, prompts.Classify+"\n\nINPUT:\n"+string(input))
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &c); err != nil {
		return c, fmt.Errorf("invalid classifier output: %w", err)
	}
	c.Title = strings.TrimSpace(c.Title)
	if len([]rune(c.Title)) > 100 {
		c.Title = string([]rune(c.Title)[:100])
	}
	return c, nil
}

// Forget retracts a memory. It stays in git history but is no longer recalled.
func (a *App) Forget(ctx context.Context, id, reason string) (memory.Memory, error) {
	m, err := a.Get(id)
	if err != nil {
		return m, err
	}
	if m.Legacy {
		return m, fmt.Errorf("%s is a legacy entry in %s and is read-only; delete it from that file on GitHub", id, m.Path)
	}
	if reason == "" {
		reason = "retracted"
	}
	reason, _ = secrets.Redact(reason)
	if err := a.retract(ctx, m, reason); err != nil {
		return m, err
	}
	m.Status, m.StatusReason = memory.StatusRetracted, reason
	return m, nil
}

// Edits are optional changes applied when approving a pending item.
type Edits struct {
	Title   *string   `json:"title,omitempty"`
	Summary *string   `json:"summary,omitempty"`
	Body    *string   `json:"body,omitempty"`
	About   *[]string `json:"about,omitempty"`
	Tags    *[]string `json:"tags,omitempty"`
	Type    *string   `json:"type,omitempty"`
	Scope   *string   `json:"scope,omitempty"`
}

// Approve publishes a pending item (optionally edited).
func (a *App) Approve(ctx context.Context, id string, e *Edits) (memory.Memory, error) {
	p, err := a.GetPending(id)
	if err != nil {
		return memory.Memory{}, err
	}
	if p.Kind == KindCorrection {
		target, err := a.Get(p.TargetID)
		if err != nil {
			_ = a.removePending(id)
			return memory.Memory{}, err
		}
		if err := a.retract(ctx, target, p.Memory.Summary); err != nil {
			return memory.Memory{}, err
		}
		_ = a.removePending(id)
		target.Status = memory.StatusRetracted
		return target, nil
	}
	m := p.Memory
	if e != nil {
		if e.Title != nil {
			m.Title = strings.TrimSpace(*e.Title)
		}
		if e.Summary != nil {
			m.Summary = strings.TrimSpace(*e.Summary)
		}
		if e.Body != nil {
			m.Body = strings.TrimSpace(*e.Body)
		}
		if e.Tags != nil {
			m.Tags = cleanTags(*e.Tags)
		}
		if e.Type != nil {
			m.Type = normType(*e.Type, m.Type)
		}
		if e.Scope != nil && (*e.Scope == memory.ScopePersonal || *e.Scope == memory.ScopeTeam) {
			m.Scope = *e.Scope
		}
		if e.About != nil {
			if v, err := a.View(); err == nil {
				m.About = a.canonicalAbout(v, *e.About)
			}
		}
		redactMemory(&m)
	}
	if m.Title == "" {
		return m, errors.New("title is required")
	}
	m.Path = memory.PathFor(m)
	ents := p.NewEntities
	if e != nil && e.About != nil {
		ents = entitiesFor(m.About, nil)
	}
	m, err = a.publish(ctx, m, ents)
	if err != nil {
		if errors.Is(err, gh.ErrConflict) {
			// Already published (e.g. a previous approve timed out after
			// the write landed).
			_ = a.removePending(id)
		}
		return m, err
	}
	_ = a.removePending(id)
	return m, nil
}

// Reject discards a pending item.
func (a *App) Reject(id string) error {
	if _, err := a.GetPending(id); err != nil {
		return err
	}
	return a.removePending(id)
}

// Settings is the user-facing configuration summary (no secrets).
type Settings struct {
	Configured      bool                   `json:"configured"`
	Owner           string                 `json:"owner"`
	Repo            string                 `json:"repo"`
	Username        string                 `json:"username"`
	TokenSource     string                 `json:"token_source"`   // "config" | "gh" | ""
	Mode            string                 `json:"mode"`           // user's choice ("" = team default)
	EffectiveMode   policy.Mode            `json:"effective_mode"` // what's in force
	TeamDefault     policy.Mode            `json:"team_default"`
	TeamMinimum     policy.Mode            `json:"team_minimum"`
	AllowedModes    []policy.Mode          `json:"allowed_modes"`
	ModeHelp        map[policy.Mode]string `json:"mode_help"`
	ReviewTimeout   int                    `json:"review_timeout_days"`
	LLM             string                 `json:"llm"`
	LastSync        time.Time              `json:"last_sync"`
	ConfigPath      string                 `json:"config_path"`
	DataDir         string                 `json:"data_dir"`
	PendingCount    int                    `json:"pending_count"`
	QueueLength     int                    `json:"queue_length"`
	SnapshotProblem []string               `json:"snapshot_problems,omitempty"`
}

// Settings describes the current configuration.
func (a *App) Settings() Settings {
	cfg := a.Config()
	s := Settings{
		Configured: cfg.Configured(), Owner: cfg.Owner, Repo: cfg.Repo, Username: cfg.Username,
		Mode: cfg.Mode, ModeHelp: policy.Descriptions, ConfigPath: a.CfgPath, DataDir: a.DataDir,
		LLM: runnerName(a.Runner), QueueLength: a.QueueLen(),
	}
	switch {
	case cfg.Token != "":
		s.TokenSource = "config"
	case cfg.TokenSource == "gh":
		s.TokenSource = "gh"
	}
	pol := policy.Default()
	if v, err := a.View(); err == nil {
		pol = v.Snap.Policy
		s.SnapshotProblem = v.Snap.Problems
	}
	s.EffectiveMode, s.TeamDefault, s.TeamMinimum = pol.Effective(cfg.Mode), pol.DefaultMode, pol.MinimumMode
	s.AllowedModes, s.ReviewTimeout = pol.Allowed(), pol.ReviewTimeoutDays
	s.LastSync = a.SyncState().SyncedAt
	if p, err := a.ListPending(); err == nil {
		s.PendingCount = len(p)
	}
	return s
}

func runnerName(r llm.Runner) string {
	switch r.(type) {
	case *llm.Claude:
		return "claude-cli"
	case *llm.API:
		return "anthropic-api"
	case llm.Unavailable:
		return "unavailable"
	}
	return "custom"
}

// SetMode changes this user's capture mode ("" = team default).
func (a *App) SetMode(mode string) (policy.Mode, error) {
	if mode != "" {
		m, err := policy.ParseMode(mode)
		if err != nil {
			return "", err
		}
		mode = string(m)
	}
	cfg := a.Config()
	cfg.Mode = mode
	if err := config.Write(a.CfgPath, cfg); err != nil {
		return "", err
	}
	if err := a.Reload(); err != nil {
		return "", err
	}
	pol := policy.Default()
	if v, err := a.View(); err == nil {
		pol = v.Snap.Policy
	}
	eff := pol.Effective(mode)
	if mode != "" && string(eff) != mode {
		return eff, fmt.Errorf("your team requires at least %q; using %q", pol.MinimumMode, eff)
	}
	return eff, nil
}

// SetupInput configures the memory repo.
type SetupInput struct {
	Token       string `json:"token"`
	UseGH       bool   `json:"use_gh"` // use `gh auth token` instead of storing a token
	Repo        string `json:"repo"`   // owner/name
	Mode        string `json:"mode"`
	APIURL      string `json:"api_url,omitempty"`
	KeepToken   bool   `json:"keep_token"` // reuse the currently configured token
	SkipSeeding bool   `json:"-"`
}

// SetupResult reports what setup did.
type SetupResult struct {
	Username string   `json:"username"`
	Repo     string   `json:"repo"`
	Seeded   []string `json:"seeded,omitempty"`
	Memories int      `json:"memories"`
}

// Setup validates access to the memory repo, seeds it if needed and writes
// the config file.
func (a *App) Setup(ctx context.Context, in SetupInput) (SetupResult, error) {
	var res SetupResult
	owner, repo, ok := strings.Cut(strings.Trim(strings.TrimSpace(in.Repo), "/"), "/")
	owner, repo = strings.TrimSpace(owner), strings.TrimSuffix(strings.TrimSpace(repo), ".git")
	if !ok || owner == "" || repo == "" || strings.Contains(repo, "/") {
		return res, errors.New("repository must be in the form owner/name")
	}
	cfg := config.Config{Owner: owner, Repo: repo, Mode: in.Mode, APIURL: in.APIURL}
	old := a.Config()
	switch {
	case in.UseGH:
		cfg.TokenSource = "gh"
	case strings.TrimSpace(in.Token) != "":
		cfg.Token = strings.TrimSpace(in.Token)
	case in.KeepToken && (old.Token != "" || old.TokenSource == "gh"):
		cfg.Token, cfg.TokenSource = old.Token, old.TokenSource
	default:
		return res, errors.New("a GitHub token is required (or choose to use your GitHub CLI login)")
	}
	if cfg.Mode != "" {
		if _, err := policy.ParseMode(cfg.Mode); err != nil {
			return res, err
		}
	}
	client := gh.NewClient(cfg, a.apiBase)
	user, err := client.GetUser(ctx)
	if err != nil {
		return res, fmt.Errorf("checking token: %w", err)
	}
	info, err := client.GetRepo(ctx)
	if err != nil {
		return res, fmt.Errorf("checking %s/%s: %w", owner, repo, err)
	}
	cfg.Username = user.Login
	res.Username, res.Repo = user.Login, info.FullName

	st := store.New(a.DataDir, owner, repo, client)
	if !in.SkipSeeding {
		seeds := []struct{ path, content string }{
			{policy.Path, policy.Default().Render()},
			{entities.Path, entities.Registry{Entities: []entities.Entity{}}.Render()},
			{"README.md", seedReadme},
		}
		for _, s := range seeds {
			created, err := st.PutIfMissing(ctx, s.path, s.content, "team-memory: initialise "+s.path)
			if err != nil {
				if gh.IsStatus(err, 403) || gh.IsStatus(err, 404) {
					return res, fmt.Errorf("the token can read %s but cannot write to it — it needs Contents: read & write: %w", info.FullName, err)
				}
				return res, fmt.Errorf("initialising %s: %w", s.path, err)
			}
			if created {
				res.Seeded = append(res.Seeded, s.path)
			}
		}
	}
	if err := config.Write(a.CfgPath, cfg); err != nil {
		return res, fmt.Errorf("write config: %w", err)
	}
	if err := a.Reload(); err != nil {
		return res, err
	}
	if _, err := a.Sync(ctx); err != nil {
		return res, fmt.Errorf("first sync: %w", err)
	}
	if v, err := a.View(); err == nil {
		res.Memories = len(v.Snap.Memories)
	}
	return res, nil
}

const seedReadme = `# Team memory

This repository is the shared memory for [team-memory](https://github.com/AndrewSkea/team-memory).

- ` + "`memories/`" + ` — one Markdown file per memory, with YAML frontmatter. Edit or delete freely; changes sync to everyone.
- ` + "`.team-memory/entities.yml`" + ` — the systems, services and libraries memories can be *about*. Add aliases (env var names, package names) to improve recall.
- ` + "`.team-memory/policy.yml`" + ` — team capture policy (default and minimum capture modes, review rules).

Memories are captured automatically from Claude Code sessions and recalled when anyone — in any repository — works with the same systems.
`

// Stats summarises the memory repo.
type Stats struct {
	Total        int            `json:"total"`
	Active       int            `json:"active"`
	ThisWeek     int            `json:"this_week"`
	Legacy       int            `json:"legacy"`
	ByType       map[string]int `json:"by_type"`
	BySource     map[string]int `json:"by_source"`
	TopEntities  []Count        `json:"top_entities"`
	Contributors []Count        `json:"contributors"`
	Pending      int            `json:"pending"`
	Entities     int            `json:"entities"`
}

type Count struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (a *App) Stats() (Stats, error) {
	v, err := a.View()
	if err != nil {
		return Stats{}, err
	}
	s := Stats{ByType: map[string]int{}, BySource: map[string]int{}, Entities: len(v.Snap.Entities.Entities)}
	ents, people := map[string]int{}, map[string]int{}
	for _, m := range v.Snap.Memories {
		s.Total++
		if m.Legacy {
			s.Legacy++
		}
		if !m.Active() {
			continue
		}
		s.Active++
		if a.Now().Sub(m.Created) < 7*24*time.Hour {
			s.ThisWeek++
		}
		s.ByType[m.Type]++
		src := m.Source
		if src == "" {
			src = "unknown"
		}
		s.BySource[src]++
		for _, e := range m.About {
			ents[e]++
		}
		if m.Author != "" {
			people[m.Author]++
		}
	}
	s.TopEntities, s.Contributors = top(ents, 10), top(people, 10)
	if s.TopEntities == nil {
		s.TopEntities = []Count{}
	}
	if s.Contributors == nil {
		s.Contributors = []Count{}
	}
	if p, err := a.ListPending(); err == nil {
		s.Pending = len(p)
	}
	return s, nil
}

func top(m map[string]int, n int) []Count {
	var out []Count
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func heuristicTitle(text string) string {
	t := strings.TrimSpace(text)
	if i := strings.IndexAny(t, "\n"); i > 0 {
		t = t[:i]
	}
	if s := firstSentence(t); s != "" {
		t = s
	}
	t = strings.TrimRight(strings.TrimLeft(t, "#-* "), ".")
	r := []rune(t)
	if len(r) > 80 {
		t = strings.TrimSpace(string(r[:79])) + "…"
	}
	if t == "" {
		t = "Untitled note"
	}
	return t
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\n') {
			return strings.TrimSpace(s[:i+1])
		}
		if r == '\n' {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func shortErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
