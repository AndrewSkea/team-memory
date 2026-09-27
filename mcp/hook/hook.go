// Package hook implements team-memory's Claude Code hook handlers.
//
//	SessionStart     → sync, inject memories about systems this workspace uses
//	UserPromptSubmit → inject memories relevant to the prompt (local only, fast)
//	PreCompact       → queue a capture of the conversation so far
//	SessionEnd       → queue a capture of the rest of the session
//
// Capture runs in a detached background worker so Claude Code never waits on
// the LLM or GitHub. Hooks never fail loudly: errors go to the log file.
package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/app"
	"github.com/AndrewSkea/team-memory/mcp/llm"
)

// Events accepted by Run (and the legacy --once names).
const (
	SessionStart = "session-start"
	PromptSubmit = "prompt-submit"
	PreCompact   = "precompact"
	SessionEnd   = "session-end"
	Worker       = "capture-worker"
)

// Nested reports whether we're inside team-memory's own `claude -p` call.
func Nested() bool { return os.Getenv(llm.NestedEnv) != "" }

// Run handles one hook event. stdin is Claude Code's JSON payload.
func Run(ctx context.Context, a *app.App, event string, stdin io.Reader, stdout io.Writer) error {
	if Nested() {
		return nil
	}
	var in app.HookInput
	if event != Worker {
		if err := json.NewDecoder(stdin).Decode(&in); err != nil && err != io.EOF {
			a.Log.Printf("hook %s: bad input: %v", event, err)
			return nil
		}
	}
	switch event {
	case SessionStart:
		out := a.SessionStart(ctx, in)
		if a.QueueLen() > 0 {
			spawnWorker(a) // retry anything left over from offline sessions
		}
		return emit(stdout, "SessionStart", out)
	case PromptSubmit:
		return emit(stdout, "UserPromptSubmit", a.PromptSubmit(in))
	case PreCompact, SessionEnd:
		if !a.Configured() || in.TranscriptPath == "" {
			return nil
		}
		if err := a.Enqueue(app.Job{SessionID: in.SessionID, TranscriptPath: in.TranscriptPath, Cwd: in.Cwd, Event: event}); err != nil {
			a.Log.Printf("hook %s: enqueue: %v", event, err)
			return nil
		}
		spawnWorker(a)
		return nil
	case Worker:
		// Bound the run so a hung LLM/GitHub call can't leave zombies.
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		a.ProcessQueue(ctx)
		return nil
	}
	return fmt.Errorf("unknown hook event %q (want %s|%s|%s|%s)", event, SessionStart, PromptSubmit, PreCompact, SessionEnd)
}

func emit(w io.Writer, eventName string, out app.HookOutput) error {
	if out.Context == "" && out.SystemMessage == "" {
		return nil
	}
	payload := map[string]any{}
	if out.Context != "" {
		payload["hookSpecificOutput"] = map[string]any{
			"hookEventName":     eventName,
			"additionalContext": out.Context,
		}
	}
	if out.SystemMessage != "" {
		payload["systemMessage"] = out.SystemMessage
	}
	return json.NewEncoder(w).Encode(payload)
}

// spawnWorker starts `team-memory-mcp hook capture-worker` fully detached
// from Claude Code so it survives the session ending.
func spawnWorker(a *app.App) {
	if os.Getenv("TEAM_MEMORY_NO_SPAWN") != "" { // tests run the worker inline
		return
	}
	exe, err := os.Executable()
	if err != nil {
		a.Log.Printf("spawn worker: %v", err)
		return
	}
	logPath := filepath.Join(a.DataDir, "logs", "worker.log")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		logf = nil
	}
	cmd := exec.Command(exe, "hook", Worker)
	cmd.Env = os.Environ()
	cmd.Stdin = nil
	if logf != nil {
		cmd.Stdout, cmd.Stderr = logf, logf
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		a.Log.Printf("spawn worker: %v", err)
	} else {
		_ = cmd.Process.Release()
	}
	if logf != nil {
		logf.Close()
	}
}
