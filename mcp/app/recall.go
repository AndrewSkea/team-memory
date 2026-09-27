package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/policy"
	"github.com/AndrewSkea/team-memory/mcp/search"
	"github.com/AndrewSkea/team-memory/mcp/workspace"
)

// HookInput is the JSON Claude Code passes to hooks (fields vary by event).
type HookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`
	Source         string `json:"source"`
}

// HookOutput is what recall hooks return to Claude Code.
type HookOutput struct {
	Context       string // injected into Claude's context
	SystemMessage string // shown to the user
}

const (
	sessionStartLimit = 5
	promptLimit       = 3
)

// SessionStart refreshes the mirror, applies review timeouts and returns
// memories about the systems this workspace touches — wherever they were
// learned — plus a nudge if items are waiting for review.
func (a *App) SessionStart(ctx context.Context, in HookInput) HookOutput {
	var out HookOutput
	if !a.Configured() {
		return out
	}
	syncCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	a.EnsureFresh(syncCtx, time.Minute)
	cancel()
	a.CleanSessions()
	a.ExpirePending(ctx)

	v, err := a.View()
	if err != nil {
		a.Log.Printf("session-start: %v", err)
		return out
	}
	repo, _ := workspace.FindRepo(in.Cwd)
	signals := workspace.Signals(in.Cwd)
	sess := a.LoadSession(in.SessionID)
	sess.Signals, sess.Repo = signals, repo.Slug()

	weak := v.Matcher.Match(strings.Join(signals, " | "))
	var picked []search.Result
	if len(weak) > 0 {
		for _, r := range v.Index.Search(search.Query{WeakEntities: weak, Repo: repo.Slug(), Viewer: a.Viewer(),
			Limit: sessionStartLimit, Exclude: set(sess.Injected), Now: a.Now()}) {
			if len(r.EntityHits) > 0 {
				picked = append(picked, r)
			}
		}
	}
	for _, r := range picked {
		sess.Injected = append(sess.Injected, r.Memory.ID)
	}
	a.SaveSession(in.SessionID, sess)
	if len(picked) > 0 {
		out.Context = a.formatContext(picked, "Team knowledge about systems this workspace uses")
	}
	if n, _ := a.ListPending(); len(n) > 0 {
		noun := "memories"
		if len(n) == 1 {
			noun = "memory"
		}
		out.SystemMessage = fmt.Sprintf("team-memory: %d %s waiting for your review — run /memory-review", len(n), noun)
	}
	return out
}

// PromptSubmit returns memories relevant to the user's prompt. It never
// touches the network so it adds no noticeable latency.
func (a *App) PromptSubmit(in HookInput) HookOutput {
	var out HookOutput
	prompt := strings.TrimSpace(in.Prompt)
	if !a.Configured() || prompt == "" || strings.HasPrefix(prompt, "/") {
		return out
	}
	v, err := a.View()
	if err != nil || v.Index.Len() == 0 {
		return out
	}
	sess := a.LoadSession(in.SessionID)
	if sess.Signals == nil && in.Cwd != "" {
		sess.Signals = workspace.Signals(in.Cwd)
		if r, ok := workspace.FindRepo(in.Cwd); ok {
			sess.Repo = r.Slug()
		}
	}
	strong := v.Matcher.Match(prompt)
	if len(prompt) < 15 && len(strong) == 0 {
		return out
	}
	weak := v.Matcher.Match(strings.Join(sess.Signals, " | "))
	var picked []search.Result
	for _, r := range v.Index.Search(search.Query{Text: prompt, StrongEntities: strong, WeakEntities: weak,
		Repo: sess.Repo, Viewer: a.Viewer(), Limit: promptLimit * 2, Exclude: set(sess.Injected), Now: a.Now()}) {
		if r.Relevant() && len(picked) < promptLimit {
			picked = append(picked, r)
		}
	}
	if len(picked) == 0 {
		return out
	}
	for _, r := range picked {
		sess.Injected = append(sess.Injected, r.Memory.ID)
	}
	a.SaveSession(in.SessionID, sess)
	out.Context = a.formatContext(picked, "Possibly relevant team knowledge")
	return out
}

func (a *App) formatContext(rs []search.Result, heading string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<team-memory>\n%s (from the shared memory repo %s). It may be out of date: verify before relying on it, and mention it when you use it.\n",
		heading, a.Config().Slug())
	for _, r := range rs {
		m := r.Memory
		var meta []string
		if m.Type != "" {
			meta = append(meta, m.Type)
		}
		if len(m.About) > 0 {
			meta = append(meta, "about "+strings.Join(m.About, ", "))
		}
		prov := ""
		if m.LearnedIn != "" {
			prov = "learned in " + m.LearnedIn
		}
		if m.Author != "" {
			prov = strings.TrimSpace(prov + " by " + m.Author)
		}
		if !m.Created.IsZero() {
			prov = strings.TrimSpace(prov + ", " + Ago(a.Now(), m.Created))
		}
		if prov != "" {
			meta = append(meta, prov)
		}
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", m.ID, m.Title, strings.Join(meta, "; "))
		if m.Summary != "" && m.Summary != m.Title {
			fmt.Fprintf(&b, "  %s\n", m.Summary)
		}
	}
	b.WriteString("Use the team-memory tools: get_memory for details, recall to search, supersede or forget if one is wrong.\n</team-memory>")
	return b.String()
}

// ExpirePending retries items whose publish failed, and publishes
// review-timeout items older than the team's review window.
func (a *App) ExpirePending(ctx context.Context) {
	v, err := a.View()
	if err != nil {
		return
	}
	timeout := a.Mode(v.Snap.Policy) == policy.ReviewTimeout
	items, _ := a.listPendingAll()
	limit := time.Duration(v.Snap.Policy.ReviewTimeoutDays) * 24 * time.Hour
	for _, p := range items {
		if !p.AutoRetry && (!timeout || a.Now().Sub(p.QueuedAt) < limit) {
			continue
		}
		if _, err := a.Approve(ctx, p.ID(), nil); err != nil {
			a.Log.Printf("auto-publish %s after timeout: %v", p.ID(), err)
		} else {
			a.Log.Printf("auto-published %s after %d-day review window", p.ID(), v.Snap.Policy.ReviewTimeoutDays)
		}
	}
}

// Ago renders a coarse relative time.
func Ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 2*time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d weeks ago", int(d.Hours()/24/7))
	case d < 730*24*time.Hour:
		return fmt.Sprintf("%d months ago", int(d.Hours()/24/30))
	}
	return fmt.Sprintf("%d years ago", int(d.Hours()/24/365))
}

func set(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
