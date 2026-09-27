package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/entities"
	"github.com/AndrewSkea/team-memory/mcp/llm"
	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/prompts"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/secrets"
	"github.com/AndrewSkea/team-memory/mcp/transcript"
	"github.com/AndrewSkea/team-memory/mcp/workspace"
)

const (
	transcriptBudget = 60_000 // characters of conversation sent to the extractor
	minTranscript    = 150    // below this ("hi", "thanks") skip the LLM call; the extractor filters the rest
)

// extraction is the extractor's JSON output.
type extraction struct {
	Memories []struct {
		Title       string   `json:"title"`
		Summary     string   `json:"summary"`
		Body        string   `json:"body"`
		About       []string `json:"about"`
		Tags        []string `json:"tags"`
		Type        string   `json:"type"`
		Sensitivity string   `json:"sensitivity"`
		Confidence  float64  `json:"confidence"`
		Supersedes  []string `json:"supersedes"`
	} `json:"memories"`
	NewEntities []entities.Entity `json:"new_entities"`
	Corrections []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"corrections"`
}

// CaptureResult summarises one capture run.
type CaptureResult struct {
	Skipped   string   `json:"skipped,omitempty"`
	Published []string `json:"published,omitempty"`
	Pending   []string `json:"pending,omitempty"`
	Dropped   []string `json:"dropped,omitempty"`
}

func (r CaptureResult) String() string {
	if r.Skipped != "" {
		return "skipped: " + r.Skipped
	}
	return fmt.Sprintf("published %d, pending review %d, dropped %d", len(r.Published), len(r.Pending), len(r.Dropped))
}

// ProcessQueue runs all queued capture jobs. Failed jobs are retried by a
// later run (up to 3 attempts), so being offline loses nothing.
//
// Only one worker runs at a time: two workers could otherwise capture the
// same session concurrently (e.g. PreCompact and SessionEnd firing close
// together) and both extract the same transcript lines.
func (a *App) ProcessQueue(ctx context.Context) {
	// Each job is attempted at most once per run; failures wait for a later
	// run (next session start or the background service) to retry.
	seen := map[string]bool{}
	for round := 0; round < 5; round++ {
		unlock, ok := a.lockWorker()
		if !ok {
			return // another worker is running and will drain the queue
		}
		for ctx.Err() == nil {
			var todo []string
			for _, j := range a.claimJobs() {
				base := jobBase(j)
				if seen[base] {
					_ = os.Rename(j, filepath.Join(a.queueDir(), base)) // unclaim
					continue
				}
				seen[base] = true
				todo = append(todo, j)
			}
			if len(todo) == 0 {
				break
			}
			a.processJobs(ctx, todo)
		}
		unlock()
		// A job enqueued between our last check and unlock would find the
		// lock held and exit; pick it up here.
		if !a.hasUnseenJobs(seen) || ctx.Err() != nil {
			return
		}
	}
}

// jobBase maps a claimed job path back to its queue file name.
func jobBase(path string) string {
	return filepath.Base(strings.SplitN(path, ".working-", 2)[0])
}

func (a *App) hasUnseenJobs(seen map[string]bool) bool {
	entries, _ := os.ReadDir(a.queueDir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") && !seen[e.Name()] {
			return true
		}
	}
	return false
}

// lockWorker takes the queue lock, reclaiming it if its holder died.
func (a *App) lockWorker() (func(), bool) {
	path := filepath.Join(a.queueDir(), ".worker.lock")
	_ = os.MkdirAll(a.queueDir(), 0o700)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(path) }, true
		}
		info, statErr := os.Stat(path)
		if statErr != nil || a.Now().Sub(info.ModTime()) < 15*time.Minute {
			return nil, false
		}
		os.Remove(path) // stale: the previous worker crashed
	}
	return nil, false
}

func (a *App) processJobs(ctx context.Context, jobs []string) {
	for _, path := range jobs {
		var j Job
		if err := readJSONFile(path, &j); err != nil {
			a.Log.Printf("queue: drop unreadable job %s: %v", path, err)
			os.Remove(path)
			continue
		}
		res, err := a.Capture(ctx, j)
		if err != nil {
			j.Attempts++
			a.Log.Printf("capture %s (%s) attempt %d failed: %v", j.SessionID, j.Event, j.Attempts, err)
			if j.Attempts < 3 && !errors.Is(err, ErrNotConfigured) {
				_ = writeJSONFile(strings.SplitN(path, ".working-", 2)[0], j)
			}
			os.Remove(path)
			continue
		}
		a.Log.Printf("capture %s (%s): %s", j.SessionID, j.Event, res)
		os.Remove(path)
	}
}

// Capture extracts knowledge from the unprocessed part of a transcript and
// publishes, queues or drops each item according to the capture mode.
func (a *App) Capture(ctx context.Context, j Job) (CaptureResult, error) {
	var res CaptureResult
	if !a.Configured() {
		return res, ErrNotConfigured
	}
	syncCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	a.EnsureFresh(syncCtx, time.Minute)
	cancel()
	v, err := a.View()
	if err != nil {
		return res, err
	}
	mode := a.Mode(v.Snap.Policy)
	if mode == policy.Manual {
		res.Skipped = "capture mode is manual"
		return res, nil
	}

	sess := a.LoadSession(j.SessionID)
	msgs, offset, err := transcript.ReadFrom(j.TranscriptPath, sess.Offset)
	if err != nil {
		return res, fmt.Errorf("read transcript: %w", err)
	}
	users, assistants, chars := transcript.Stats(msgs)
	if users == 0 || assistants == 0 || chars < minTranscript {
		sess.Offset = offset
		a.SaveSession(j.SessionID, sess)
		res.Skipped = "nothing substantial since last capture"
		return res, nil
	}

	repo, _ := workspace.FindRepo(j.Cwd)
	// Related memories let the extractor dedupe and spot superseded facts.
	var userText strings.Builder
	for _, m := range msgs {
		if m.Role == "user" && userText.Len() < 4000 {
			userText.WriteString(m.Content + "\n")
		}
	}
	related := v.Index.Search(search.Query{Text: userText.String(), StrongEntities: v.Matcher.Match(userText.String()),
		Viewer: a.Viewer(), Limit: 8, Now: a.Now()})

	out, err := a.extract(ctx, v, repo.Slug(), related, sess.Injected, msgs)
	if err != nil {
		return res, err
	}

	inOrg := v.Snap.Policy.InOrg(a.Config().Owner, repo.Owner)
	// Entities: normalise the extractor's names against the registry.
	newEnts := map[string]entities.Entity{}
	for _, e := range out.NewEntities {
		newEnts[entities.CanonicalName(e.Name)] = e
	}
	for _, x := range out.Memories {
		m := memory.Memory{
			ID:         memory.NewID(a.Now()),
			Title:      strings.TrimSpace(x.Title),
			Type:       normType(x.Type, memory.TypeFact),
			Summary:    strings.TrimSpace(x.Summary),
			Body:       strings.TrimSpace(x.Body),
			Tags:       cleanTags(x.Tags),
			Author:     a.Viewer(),
			LearnedIn:  repo.Slug(),
			Source:     sourceFor(j.Event),
			Created:    a.Now().UTC(),
			Confidence: clamp01(x.Confidence),
			Scope:      memory.ScopeTeam,
		}
		if m.Title == "" {
			continue
		}
		m.About = a.canonicalAbout(v, x.About)
		m.Supersedes = validTargets(v, x.Supersedes)
		redacted := redactMemory(&m)
		m.Path = memory.PathFor(m)
		d := v.Snap.Policy.Decide(mode, policy.Candidate{
			Type:        m.Type,
			Sensitivity: strings.ToLower(x.Sensitivity),
			Confidence:  m.Confidence,
			Contradicts: len(m.Supersedes) > 0,
			Redacted:    redacted,
		}, inOrg)
		ents := entitiesFor(m.About, newEnts)
		switch d.Action {
		case policy.Publish:
			if _, err := a.publish(ctx, m, ents); err != nil {
				// Don't lose it: park it and retry automatically later.
				a.Log.Printf("publish %q failed, will retry: %v", m.Title, err)
				if err := a.AddPending(PendingItem{Kind: KindMemory, Memory: m, Reason: "retrying publish: " + shortErr(err),
					QueuedAt: a.Now().UTC(), NewEntities: ents, AutoRetry: true}); err != nil {
					return res, err
				}
				res.Pending = append(res.Pending, m.ID)
				continue
			}
			res.Published = append(res.Published, m.ID)
		case policy.Hold:
			if err := a.AddPending(PendingItem{Kind: KindMemory, Memory: m, Reason: d.Reason, QueuedAt: a.Now().UTC(), NewEntities: ents}); err != nil {
				return res, err
			}
			res.Pending = append(res.Pending, m.ID)
		default:
			a.Log.Printf("dropped %q: %s", m.Title, d.Reason)
			res.Dropped = append(res.Dropped, m.Title)
		}
	}

	for _, c := range out.Corrections {
		target, ok := v.Snap.ByID(c.ID)
		if !ok || target.Legacy || !target.Active() || strings.TrimSpace(c.Reason) == "" {
			continue
		}
		reason, _ := secrets.Redact(strings.TrimSpace(c.Reason))
		if mode == policy.Auto && inOrg {
			if err := a.retract(ctx, target, reason); err == nil {
				res.Published = append(res.Published, "correction:"+target.ID)
				continue
			}
		}
		item := PendingItem{Kind: KindCorrection, TargetID: target.ID, Reason: "the assistant thinks this memory is wrong: " + reason,
			QueuedAt: a.Now().UTC(), Memory: memory.Memory{ID: memory.NewID(a.Now()), Title: "Retract: " + target.Title,
				Summary: reason, Type: target.Type, Created: a.Now().UTC()}}
		if err := a.AddPending(item); err == nil {
			res.Pending = append(res.Pending, item.ID())
		}
	}

	sess.Offset = offset
	a.SaveSession(j.SessionID, sess)
	return res, nil
}

func (a *App) extract(ctx context.Context, v *View, repo string, related []search.Result, recalled []string, msgs []transcript.Message) (extraction, error) {
	type relatedMem struct {
		ID      string   `json:"id"`
		Title   string   `json:"title"`
		Summary string   `json:"summary,omitempty"`
		About   []string `json:"about,omitempty"`
	}
	var rel []relatedMem
	for _, r := range related {
		if !r.Memory.Legacy {
			rel = append(rel, relatedMem{r.Memory.ID, r.Memory.Title, r.Memory.Summary, r.Memory.About})
		}
	}
	ents := v.Snap.Entities.Entities
	if len(ents) > 300 {
		ents = ents[:300]
	}
	input, _ := json.Marshal(map[string]any{
		"repo":                repo,
		"entities":            ents,
		"related_memories":    rel,
		"recalled_memory_ids": recalled,
		"transcript":          transcript.Render(msgs, transcriptBudget),
	})
	llmCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	raw, err := a.Runner.Run(llmCtx, prompts.Extract+"\n\nINPUT:\n"+string(input))
	if err != nil {
		return extraction{}, err
	}
	var out extraction
	if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &out); err != nil {
		return extraction{}, fmt.Errorf("extractor returned invalid JSON: %w (%.200s)", err, raw)
	}
	return out, nil
}

// publish commits a memory, marks what it supersedes, and records any new
// entities it refers to.
func (a *App) publish(ctx context.Context, m memory.Memory, ents []entities.Entity) (memory.Memory, error) {
	_, st, err := a.parts()
	if err != nil {
		return m, err
	}
	m, err = st.Create(ctx, m)
	if err != nil {
		return m, err
	}
	defer a.invalidate()
	if v, err := a.View(); err == nil {
		for _, id := range m.Supersedes {
			old, ok := v.Snap.ByID(id)
			if !ok || old.Legacy {
				continue
			}
			if _, err := st.UpdateMemory(ctx, old.Path, "memory: superseded by "+m.ID, func(o *memory.Memory) error {
				o.Status, o.SupersededBy, o.Updated = memory.StatusSuperseded, m.ID, a.Now().UTC()
				return nil
			}); err != nil {
				a.Log.Printf("mark %s superseded: %v", id, err)
			}
		}
	}
	if len(ents) > 0 {
		if err := a.mergeEntities(ctx, ents); err != nil {
			a.Log.Printf("update entities: %v", err)
		}
	}
	return m, nil
}

func (a *App) mergeEntities(ctx context.Context, ents []entities.Entity) error {
	v, err := a.View()
	if err == nil {
		probe := v.Snap.Entities
		probe.Entities = append([]entities.Entity(nil), probe.Entities...)
		if !probe.Merge(ents) {
			return nil // nothing new; skip the network round trip
		}
	}
	_, st, err := a.parts()
	if err != nil {
		return err
	}
	return st.UpdateMeta(ctx, entities.Path, "memory: update entity catalogue", func(cur string, exists bool) (string, error) {
		reg, err := entities.Parse([]byte(cur))
		if err != nil {
			return "", fmt.Errorf("%s is not valid YAML; fix it on GitHub: %w", entities.Path, err)
		}
		if !reg.Merge(ents) && exists {
			return cur, nil
		}
		return reg.Render(), nil
	})
}

func (a *App) retract(ctx context.Context, m memory.Memory, reason string) error {
	_, st, err := a.parts()
	if err != nil {
		return err
	}
	_, err = st.UpdateMemory(ctx, m.Path, "memory: retract "+m.ID, func(o *memory.Memory) error {
		o.Status, o.StatusReason, o.Updated = memory.StatusRetracted, reason, a.Now().UTC()
		return nil
	})
	a.invalidate()
	return err
}

// canonicalAbout maps names/aliases to registry names, deduplicated.
func (a *App) canonicalAbout(v *View, names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range names {
		c, ok := v.Snap.Entities.Canonical(n)
		if !ok {
			c = entities.CanonicalName(n)
		}
		if c != "" && !seen[c] && len(out) < 8 {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// entitiesFor returns registry entries for every name a memory is about,
// enriched with the extractor's aliases/descriptions where available.
func entitiesFor(about []string, extra map[string]entities.Entity) []entities.Entity {
	var out []entities.Entity
	for _, n := range about {
		e, ok := extra[n]
		if !ok {
			e = entities.Entity{Name: n}
		}
		e.Name = n
		out = append(out, e)
	}
	return out
}

func validTargets(v *View, ids []string) []string {
	var out []string
	for _, id := range ids {
		if m, ok := v.Snap.ByID(id); ok && !m.Legacy && m.Active() {
			out = append(out, id)
		}
	}
	return out
}

// redactMemory strips secrets from all text fields; reports if any were found.
func redactMemory(m *memory.Memory) bool {
	found := false
	for _, f := range []*string{&m.Title, &m.Summary, &m.Body} {
		out, fs := secrets.Redact(*f)
		if len(fs) > 0 {
			*f, found = out, true
		}
	}
	for i := range m.Tags {
		if out, fs := secrets.Redact(m.Tags[i]); len(fs) > 0 {
			m.Tags[i], found = out, true
		}
	}
	return found
}

func normType(t, def string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for _, x := range memory.Types {
		if t == x {
			return t
		}
	}
	switch t {
	case "how-to", "how to", "procedure":
		return memory.TypeHowTo
	case "general", "":
		return def
	case "meeting notes":
		return memory.TypeMeeting
	case "ideas":
		return memory.TypeIdea
	case "programming":
		return memory.TypeNote
	}
	return def
}

func cleanTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] && len(out) < 8 {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func sourceFor(event string) string {
	switch strings.ToLower(event) {
	case "precompact":
		return "precompact"
	default:
		return "session-end"
	}
}
