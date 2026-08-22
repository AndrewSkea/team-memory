// Package paths owns the on-disk layout of team-memory's state directory.
//
// The single most important property here: staging/ is a SIBLING of clone/,
// never a child. Unreviewed content is therefore unreachable by any git
// worktree add pattern, including a buggy glob. The privacy guarantee is a
// property of the layout, not a promise that the commit code is correct.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Layout is a resolved set of absolute paths.
type Layout struct {
	Home      string // $TEAM_MEMORY_HOME
	Clone     string // Home/clone      — the git worktree. Shareable.
	Staging   string // Home/staging    — sibling of Clone. Never committed.
	Pending   string // Home/staging/pending
	Bin       string // Home/staging/bin
	Index     string // Home/index.sqlite — derived, rebuildable
	TokenFile string // Home/token        — mode 0600
}

// New builds a Layout rooted at an explicit absolute home directory.
func New(home string) (Layout, error) {
	abs, err := filepath.Abs(home)
	if err != nil {
		return Layout{}, fmt.Errorf("paths: resolve %q: %w", home, err)
	}
	staging := filepath.Join(abs, "staging")
	return Layout{
		Home:      abs,
		Clone:     filepath.Join(abs, "clone"),
		Staging:   staging,
		Pending:   filepath.Join(staging, "pending"),
		Bin:       filepath.Join(staging, "bin"),
		Index:     filepath.Join(abs, "index.sqlite"),
		TokenFile: filepath.Join(abs, "token"),
	}, nil
}

// Resolve picks the state directory: $TEAM_MEMORY_HOME if set, otherwise the
// per-OS default.
func Resolve() (Layout, error) {
	if h := os.Getenv("TEAM_MEMORY_HOME"); h != "" {
		return New(h)
	}
	base, err := defaultBase()
	if err != nil {
		return Layout{}, err
	}
	return New(filepath.Join(base, "team-memory"))
}

func defaultBase() (string, error) {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return d, nil
		}
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("paths: locate home directory: %w", err)
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return filepath.Join(home, ".local", "state"), nil
}

// EnsureDirs creates every directory in the layout. Idempotent.
func (l Layout) EnsureDirs() error {
	for _, d := range []string{l.Home, l.Clone, l.Staging, l.Pending, l.Bin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("paths: create %q: %w", d, err)
		}
	}
	return nil
}

// IsInsideClone reports whether abs resolves to a location within the clone
// worktree. Symlinks are resolved where the path exists, so a symlink pointing
// out of the worktree cannot masquerade as inside it.
func (l Layout) IsInsideClone(abs string) (bool, error) {
	clone, err := resolveExisting(l.Clone)
	if err != nil {
		return false, err
	}
	target, err := resolveExisting(abs)
	if err != nil {
		return false, err
	}
	if target == clone {
		return true, nil
	}
	return strings.HasPrefix(target, clone+string(os.PathSeparator)), nil
}

// resolveExisting cleans a path and resolves symlinks on the longest existing
// prefix, so paths that do not exist yet still normalise correctly.
func resolveExisting(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("paths: abs %q: %w", p, err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return filepath.Clean(real), nil
	}
	dir, file := filepath.Split(abs)
	realDir, err := filepath.EvalSymlinks(filepath.Clean(dir))
	if err != nil {
		return filepath.Clean(abs), nil
	}
	return filepath.Join(realDir, file), nil
}
