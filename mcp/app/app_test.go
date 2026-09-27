package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/config"
	"github.com/AndrewSkea/team-memory/mcp/internal/fakegh"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
)

// fakeRunner answers extract/classify prompts with canned JSON.
type fakeRunner struct {
	mu       sync.Mutex
	extract  string
	classify string
	err      error
	prompts  []string
}

func (f *fakeRunner) Run(_ context.Context, prompt string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	if f.err != nil {
		return "", f.err
	}
	if strings.Contains(prompt, "You extract durable team knowledge") {
		return "```json\n" + f.extract + "\n```", nil
	}
	return f.classify, nil
}

type env struct {
	t      *testing.T
	fake   *fakegh.Server
	app    *App
	runner *fakeRunner
	now    time.Time
	work   string // a workspace inside acme/checkout-web
}

func newEnv(t *testing.T) *env {
	t.Helper()
	fake := fakegh.New("acme", "kb", "tok")
	fake.Login = "alice"
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	e := &env{t: t, fake: fake, runner: &fakeRunner{}, now: time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)}
	a, err := Open(Options{
		ConfigPath: filepath.Join(dir, "config.json"),
		DataDir:    filepath.Join(dir, "data"),
		Runner:     e.runner,
		APIBase:    srv.URL,
		Now:        func() time.Time { return e.now },
		LogWriter:  io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	e.work = filepath.Join(dir, "checkout-web")
	mustWrite(t, filepath.Join(e.work, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:acme/checkout-web.git\n")
	mustWrite(t, filepath.Join(e.work, ".env.example"), "PAYMENTS_API_URL=\n")
	return e
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) setup(mode string) {
	e.t.Helper()
	res, err := e.app.Setup(context.Background(), SetupInput{Token: "tok", Repo: "acme/kb", Mode: mode})
	if err != nil {
		e.t.Fatal(err)
	}
	if res.Username != "alice" || len(res.Seeded) != 3 {
		e.t.Fatalf("setup: %+v", res)
	}
}

// transcriptFile writes a Claude Code-style transcript.
func (e *env) transcriptFile(lines ...[2]string) string {
	var b strings.Builder
	for _, l := range lines {
		content, _ := json.Marshal(l[1])
		if l[0] == "user" {
			b.WriteString(`{"type":"user","message":{"role":"user","content":` + string(content) + "}}\n")
		} else {
			b.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` + string(content) + "}]}}\n")
		}
	}
	p := filepath.Join(e.t.TempDir(), "session.jsonl")
	mustWrite(e.t, p, b.String())
	return p
}

var longChat = [][2]string{
	{"user", "Checkout keeps failing with 429s when we call the payments API during the sale. Can you figure out why? " + strings.Repeat("context ", 30)},
	{"assistant", "The payments API enforces 100 requests per second per API key. Our retry loop retried immediately which amplified the problem. " + strings.Repeat("detail ", 30)},
	{"user", "Great, add jittered exponential backoff then."},
	{"assistant", "Done — retries now use full jitter starting at 200ms."},
}

const extraction1 = `{
  "memories": [
    {"title": "Payments API rate-limits at 100 rps per key", "summary": "Retry 429s with jittered exponential backoff.",
     "body": "Immediate retries amplify throttling.", "about": ["Payments API"], "tags": ["rate-limit","Retries"],
     "type": "gotcha", "sensitivity": "shareable", "confidence": 0.92, "supersedes": []},
    {"title": "Maybe the sale traffic doubles on Fridays", "summary": "Unverified hunch.", "body": "", "about": ["checkout-web"],
     "tags": [], "type": "fact", "sensitivity": "shareable", "confidence": 0.4, "supersedes": []},
    {"title": "Alice is moving house next week", "summary": "", "body": "", "about": [], "tags": [],
     "type": "fact", "sensitivity": "personal", "confidence": 0.99, "supersedes": []},
    {"title": "Staging DB access", "summary": "Use the shared account.", "body": "postgres://svc:SuperSecret99@staging-db:5432/app",
     "about": ["staging-db"], "tags": [], "type": "howto", "sensitivity": "shareable", "confidence": 0.95, "supersedes": []}
  ],
  "new_entities": [{"name": "payments-api", "aliases": ["PAYMENTS_API_URL", "payments service"], "description": "Card payments"}],
  "corrections": []
}`

func TestSetupRequiresValidToken(t *testing.T) {
	e := newEnv(t)
	_, err := e.app.Setup(context.Background(), SetupInput{Token: "wrong", Repo: "acme/kb"})
	if err == nil || !strings.Contains(err.Error(), "invalid or expired") {
		t.Fatalf("got %v", err)
	}
	if _, err := e.app.Setup(context.Background(), SetupInput{Token: "tok", Repo: "nonsense"}); err == nil {
		t.Fatal("bad repo accepted")
	}
	if e.app.Configured() {
		t.Fatal("should not be configured")
	}
	if _, _, err := e.app.Remember(context.Background(), RememberInput{Text: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v", err)
	}
}

func TestCaptureAIModeEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.setup("")
	ctx := context.Background()
	e.runner.extract = extraction1
	tp := e.transcriptFile(longChat...)
	job := Job{SessionID: "s1", TranscriptPath: tp, Cwd: e.work, Event: "session-end"}
	res, err := e.app.Capture(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	// Confident item publishes; low confidence and redacted items wait; personal is dropped.
	if len(res.Published) != 1 || len(res.Pending) != 2 || len(res.Dropped) != 1 {
		t.Fatalf("result %+v", res)
	}
	var memPath string
	for _, p := range e.fake.Paths() {
		if strings.HasPrefix(p, "memories/") {
			memPath = p
		}
	}
	content, _ := e.fake.File(memPath)
	for _, want := range []string{"about:\n  - payments-api", "learned_in: acme/checkout-web", "author: alice", "source: session-end", "- rate-limit"} {
		if !strings.Contains(content, want) {
			t.Errorf("memory file missing %q:\n%s", want, content)
		}
	}
	ents, _ := e.fake.File(".team-memory/entities.yml")
	if !strings.Contains(ents, "PAYMENTS_API_URL") {
		t.Errorf("entities not updated:\n%s", ents)
	}
	// Secrets never reach the pending queue or GitHub.
	pending, _ := e.app.ListPending()
	for _, p := range pending {
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), "SuperSecret99") {
			t.Fatal("secret stored in pending queue")
		}
	}
	for _, p := range e.fake.Paths() {
		c, _ := e.fake.File(p)
		if strings.Contains(c, "SuperSecret99") || strings.Contains(c, "moving house") {
			t.Fatalf("leak in %s", p)
		}
	}
	// The transcript sent to the LLM included the conversation.
	if !strings.Contains(e.runner.prompts[0], "100 requests per second") {
		t.Fatal("transcript not in prompt")
	}
	// Re-running on the same transcript does nothing new.
	res, err = e.app.Capture(ctx, job)
	if err != nil || res.Skipped == "" {
		t.Fatalf("second capture should skip: %+v %v", res, err)
	}
}

func TestCrossRepoRecall(t *testing.T) {
	e := newEnv(t)
	e.setup("auto")
	ctx := context.Background()
	e.runner.extract = extraction1
	// Learned in a different repo (billing-service).
	billing := filepath.Join(t.TempDir(), "billing")
	mustWrite(t, filepath.Join(billing, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/acme/billing-service\n")
	if _, err := e.app.Capture(ctx, Job{SessionID: "b1", TranscriptPath: e.transcriptFile(longChat...), Cwd: billing}); err != nil {
		t.Fatal(err)
	}
	// Session start in checkout-web, whose .env.example references PAYMENTS_API_URL.
	out := e.app.SessionStart(ctx, HookInput{SessionID: "c1", Cwd: e.work})
	if !strings.Contains(out.Context, "Payments API rate-limits") || !strings.Contains(out.Context, "learned in acme/billing-service") {
		t.Fatalf("session start context:\n%s", out.Context)
	}
	// Already injected this session: not repeated on prompts.
	if o := e.app.PromptSubmit(HookInput{SessionID: "c1", Cwd: e.work, Prompt: "why does the payments api return 429?"}); o.Context != "" {
		t.Fatalf("duplicate injection:\n%s", o.Context)
	}
	// A new session without ambient signals gets it from the prompt alone.
	o := e.app.PromptSubmit(HookInput{SessionID: "c2", Cwd: t.TempDir(), Prompt: "why does the payments service return 429 errors?"})
	if !strings.Contains(o.Context, "Payments API rate-limits") {
		t.Fatalf("prompt recall:\n%s", o.Context)
	}
	// Unrelated prompts inject nothing.
	if o := e.app.PromptSubmit(HookInput{SessionID: "c3", Prompt: "write a haiku about autumn leaves falling"}); o.Context != "" {
		t.Fatalf("irrelevant injection:\n%s", o.Context)
	}
	// Slash commands and nested sessions are ignored.
	if o := e.app.PromptSubmit(HookInput{SessionID: "c4", Prompt: "/memory-review"}); o.Context != "" {
		t.Fatal("slash command")
	}
}

func TestModes(t *testing.T) {
	ctx := context.Background()
	t.Run("review holds everything", func(t *testing.T) {
		e := newEnv(t)
		e.setup("review")
		e.runner.extract = extraction1
		res, err := e.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e.transcriptFile(longChat...), Cwd: e.work})
		if err != nil || len(res.Published) != 0 || len(res.Pending) != 3 {
			t.Fatalf("%+v %v", res, err)
		}
		out := e.app.SessionStart(ctx, HookInput{SessionID: "x", Cwd: e.work})
		if !strings.Contains(out.SystemMessage, "3 memories waiting") {
			t.Fatalf("nudge: %q", out.SystemMessage)
		}
	})
	t.Run("manual captures nothing", func(t *testing.T) {
		e := newEnv(t)
		e.setup("manual")
		e.runner.extract = extraction1
		res, err := e.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e.transcriptFile(longChat...), Cwd: e.work})
		if err != nil || res.Skipped == "" || len(e.runner.prompts) != 0 {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("team minimum clamps", func(t *testing.T) {
		e := newEnv(t)
		e.setup("auto")
		e.fake.Seed(map[string]string{policy.Path: "minimum_mode: review\n"})
		if _, err := e.app.Sync(ctx); err != nil {
			t.Fatal(err)
		}
		s := e.app.Settings()
		if s.EffectiveMode != policy.Review || len(s.AllowedModes) != 2 {
			t.Fatalf("%+v", s)
		}
		if _, err := e.app.SetMode("ai"); err == nil {
			t.Fatal("expected clamp warning")
		}
	})
	t.Run("outside org is held", func(t *testing.T) {
		e := newEnv(t)
		e.setup("auto")
		e.runner.extract = extraction1
		personal := filepath.Join(t.TempDir(), "side")
		mustWrite(t, filepath.Join(personal, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:alice/wedding-site.git\n")
		res, err := e.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e.transcriptFile(longChat...), Cwd: personal})
		if err != nil || len(res.Published) != 0 || len(res.Pending) != 3 {
			t.Fatalf("%+v %v", res, err)
		}
	})
	t.Run("review-timeout publishes after window", func(t *testing.T) {
		e := newEnv(t)
		e.setup("review-timeout")
		e.runner.extract = extraction1
		if _, err := e.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e.transcriptFile(longChat...), Cwd: e.work}); err != nil {
			t.Fatal(err)
		}
		e.now = e.now.Add(2 * 24 * time.Hour)
		e.app.ExpirePending(ctx)
		if p, _ := e.app.ListPending(); len(p) != 3 {
			t.Fatalf("published too early: %d left", len(p))
		}
		e.now = e.now.Add(2 * 24 * time.Hour)
		e.app.ExpirePending(ctx)
		if p, _ := e.app.ListPending(); len(p) != 0 {
			t.Fatalf("not published after window: %d left", len(p))
		}
	})
}

func TestApproveRejectAndRetry(t *testing.T) {
	e := newEnv(t)
	e.setup("review")
	ctx := context.Background()
	e.runner.extract = extraction1
	if _, err := e.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e.transcriptFile(longChat...), Cwd: e.work}); err != nil {
		t.Fatal(err)
	}
	items, _ := e.app.ListPending()
	title := "Edited title"
	m, err := e.app.Approve(ctx, items[0].ID(), &Edits{Title: &title, About: &[]string{"PAYMENTS_API_URL"}})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != title || len(m.About) != 1 {
		t.Fatalf("%+v", m)
	}
	if err := e.app.Reject(items[1].ID()); err != nil {
		t.Fatal(err)
	}
	if err := e.app.Reject("nope"); err == nil {
		t.Fatal("reject unknown should fail")
	}
	if p, _ := e.app.ListPending(); len(p) != 1 {
		t.Fatalf("left %d", len(p))
	}

	// Publishing failures are retried automatically, never shown for review.
	e2 := newEnv(t)
	e2.setup("auto")
	e2.runner.extract = extraction1
	e2.fake.FailPuts = 1000
	res, err := e2.app.Capture(ctx, Job{SessionID: "s", TranscriptPath: e2.transcriptFile(longChat...), Cwd: e2.work})
	if err != nil || len(res.Published) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	if p, _ := e2.app.ListPending(); len(p) != 0 {
		t.Fatalf("retry items shown for review: %d", len(p))
	}
	e2.fake.FailPuts = 0
	e2.app.ExpirePending(ctx)
	if all, _ := e2.app.listPendingAll(); len(all) != 0 {
		t.Fatalf("retry not published: %d", len(all))
	}
}

func TestRememberForgetSupersede(t *testing.T) {
	e := newEnv(t)
	e.setup("")
	ctx := context.Background()
	e.runner.classify = `{"title":"Deploys need a migration first","summary":"Run migrations before deploying.","about":["deploy-pipeline"],"tags":["deploy"],"type":"howto","new_entities":[{"name":"deploy-pipeline","aliases":["deployer"]}]}`
	m, warns, err := e.app.Remember(ctx, RememberInput{Text: "always run db migrations before deploy. token ghp_abcdefghijklmnopqrstuvwxyz0123456789", Classify: true, Source: "ui"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Deploys need a migration first" || m.Type != "howto" || len(warns) != 1 || strings.Contains(m.Body, "ghp_") {
		t.Fatalf("%+v %v", m, warns)
	}
	// The classifier never sees the secret either.
	if strings.Contains(e.runner.prompts[len(e.runner.prompts)-1], "ghp_") {
		t.Fatal("secret sent to classifier")
	}
	rs, _ := e.app.Search(SearchInput{Query: "deployer migrations"})
	if len(rs) == 0 || rs[0].Memory.ID != m.ID {
		t.Fatalf("search: %+v", rs)
	}
	// Classifier down: still saves with heuristics.
	e.runner.err = errors.New("offline")
	m2, warns, err := e.app.Remember(ctx, RememberInput{Text: "Kafka lag over 10k pages on-call. More detail here.", Classify: true})
	if err != nil || m2.Title != "Kafka lag over 10k pages on-call" || len(warns) != 1 {
		t.Fatalf("%+v %v %v", m2, warns, err)
	}
	e.runner.err = nil
	// Supersede marks the old one.
	m3, _, err := e.app.Supersede(ctx, m.ID, RememberInput{Title: "Deploys run migrations automatically now", Text: "The pipeline runs them."})
	if err != nil {
		t.Fatal(err)
	}
	old, _ := e.app.Get(m.ID)
	if old.Active() || old.SupersededBy != m3.ID || m3.About[0] != "deploy-pipeline" {
		t.Fatalf("old %+v new %+v", old, m3)
	}
	// Forget hides it from search but keeps it for IncludeAll.
	if _, err := e.app.Forget(ctx, m2.ID, "wrong threshold"); err != nil {
		t.Fatal(err)
	}
	if rs, _ := e.app.Search(SearchInput{Query: "kafka lag"}); len(rs) != 0 {
		t.Fatalf("forgotten memory recalled: %+v", rs)
	}
	if rs, _ := e.app.Search(SearchInput{Query: "kafka lag", IncludeAll: true}); len(rs) != 1 {
		t.Fatal("include all")
	}
	st, _ := e.app.Stats()
	if st.Active != 1 || st.Total != 3 || st.ByType["howto"] != 1 || st.Contributors[0].Name != "alice" {
		t.Fatalf("stats %+v", st)
	}
}

func TestPersonalScope(t *testing.T) {
	e := newEnv(t)
	e.setup("")
	ctx := context.Background()
	m, _, err := e.app.Remember(ctx, RememberInput{Title: "My reminder about invoices", Text: "Submit the invoices report", Scope: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if rs, _ := e.app.Search(SearchInput{Query: "invoices report"}); len(rs) != 1 {
		t.Fatal("author should see it")
	}
	// Another user shares the same repo.
	cfg := e.app.Config()
	cfg.Username = "bob"
	if err := config.Write(e.app.CfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	_ = e.app.Reload()
	if rs, _ := e.app.Search(SearchInput{Query: "invoices report"}); len(rs) != 0 {
		t.Fatalf("bob sees alice's personal memory %s", m.ID)
	}
}

func TestLegacyReadOnly(t *testing.T) {
	e := newEnv(t)
	e.fake.Seed(map[string]string{"GENERAL.md": "### Entry: 2025-01-01T00:00:00Z — Legacy payments note\n**Summary:** old\n"})
	e.setup("")
	rs, _ := e.app.Search(SearchInput{Query: "legacy payments"})
	if len(rs) != 1 || !rs[0].Memory.Legacy {
		t.Fatalf("%+v", rs)
	}
	if _, err := e.app.Forget(context.Background(), rs[0].Memory.ID, ""); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("got %v", err)
	}
}

func TestQueueRetriesAndCorrections(t *testing.T) {
	e := newEnv(t)
	e.setup("auto")
	ctx := context.Background()
	e.runner.extract = extraction1
	tp := e.transcriptFile(longChat...)
	e.runner.err = errors.New("llm down")
	if err := e.app.Enqueue(Job{SessionID: "q", TranscriptPath: tp, Cwd: e.work}); err != nil {
		t.Fatal(err)
	}
	e.app.ProcessQueue(ctx)
	if e.app.QueueLen() != 1 {
		t.Fatal("failed job should be re-queued")
	}
	e.runner.err = nil
	e.app.ProcessQueue(ctx)
	if e.app.QueueLen() != 0 {
		t.Fatal("job should be done")
	}
	rs, _ := e.app.Search(SearchInput{Query: "payments rate"})
	if len(rs) == 0 {
		t.Fatal("capture from queue missing")
	}
	target := rs[0].Memory
	// A later session proves it wrong: auto mode applies the correction.
	e.runner.extract = `{"memories":[],"new_entities":[],"corrections":[{"id":"` + target.ID + `","reason":"limit is 500 rps since the v3 upgrade"}]}`
	f, _ := os.OpenFile(tp, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"user","message":{"role":"user","content":"` + strings.Repeat("the limit changed ", 20) + `"}}` + "\n")
	f.WriteString(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + strings.Repeat("confirmed 500 ", 20) + `"}]}}` + "\n")
	f.Close()
	if _, err := e.app.Capture(ctx, Job{SessionID: "q", TranscriptPath: tp, Cwd: e.work}); err != nil {
		t.Fatal(err)
	}
	got, _ := e.app.Get(target.ID)
	if got.Status != memory.StatusRetracted || !strings.Contains(got.StatusReason, "500 rps") {
		t.Fatalf("%+v", got)
	}
}

func TestSingleWorker(t *testing.T) {
	e := newEnv(t)
	e.setup("auto")
	e.runner.extract = extraction1
	ctx := context.Background()
	tp := e.transcriptFile(longChat...)
	// Two jobs for the same session (PreCompact then SessionEnd).
	e.app.Enqueue(Job{SessionID: "dup", TranscriptPath: tp, Cwd: e.work, Event: "precompact"})
	e.app.Enqueue(Job{SessionID: "dup", TranscriptPath: tp, Cwd: e.work, Event: "session-end"})
	// A second worker while one holds the lock does nothing.
	unlock, ok := e.app.lockWorker()
	if !ok {
		t.Fatal("lock")
	}
	e.app.ProcessQueue(ctx)
	if e.app.QueueLen() != 2 {
		t.Fatal("second worker should not run")
	}
	unlock()
	e.app.ProcessQueue(ctx)
	if e.app.QueueLen() != 0 {
		t.Fatal("queue not drained")
	}
	// The transcript was extracted once, not twice.
	extracts := 0
	for _, p := range e.runner.prompts {
		if strings.Contains(p, "You extract durable team knowledge") {
			extracts++
		}
	}
	if extracts != 1 {
		t.Fatalf("extracted %d times", extracts)
	}
	// A stale lock from a crashed worker is reclaimed.
	lock := filepath.Join(e.app.queueDir(), ".worker.lock")
	os.WriteFile(lock, []byte("1"), 0o600)
	old := e.now.Add(-time.Hour)
	os.Chtimes(lock, old, old)
	if _, ok := e.app.lockWorker(); !ok {
		t.Fatal("stale lock not reclaimed")
	}
}
