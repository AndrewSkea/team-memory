// Package paths owns the on-disk layout of team-memory's state directory.
//
// The single most important property here: staging/ is a SIBLING of clone/,
// never a child. Unreviewed content is therefore unreachable by any git
// worktree add pattern, including a buggy glob. The privacy guarantee is a
// property of the layout, not a promise that the commit code is correct.
package paths

import (
	"errors"
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

// ErrLinkedComponent means abs reaches the clone by something other than a
// plain descent from the clone root: one of its components is a symlink, a
// junction, or another reparse point, or the path only lands inside the clone
// after such a link is followed.
//
// A linked component is refused rather than resolved, because the whole point
// of the clone boundary is that a link planted inside the worktree must not be
// able to launder content from staging/ into a shareable commit.
var ErrLinkedComponent = errors.New("paths: path reaches the clone through a link, not a plain descent")

// IsInsideClone reports whether abs resolves to a location within the clone
// worktree. Symlinks are resolved where the path exists, so a symlink pointing
// out of the worktree cannot masquerade as inside it.
//
// It also refuses any path whose route from the clone root passes through a
// link. That second check is not redundant: filepath.EvalSymlinks does NOT
// follow a Windows directory junction — it returns the junction path unchanged
// with a nil error — so resolution alone judged a junction planted inside the
// clone and pointing at staging/ to be "inside". Walking the components is
// OS-independent and needs no platform API, so it closes that class of hole on
// every platform rather than special-casing the one where it was found.
//
// The refusal fails closed in both return values: (false, ErrLinkedComponent).
// A caller that checks only the bool still refuses the write, and a caller that
// checks only the error still learns why.
func (l Layout) IsInsideClone(abs string) (bool, error) {
	clone, err := resolveExisting(l.Clone)
	if err != nil {
		return false, err
	}
	target, err := resolveExisting(abs)
	if err != nil {
		return false, err
	}
	if target != clone && !strings.HasPrefix(target, clone+string(os.PathSeparator)) {
		return false, nil
	}
	if err := l.assertPlainDescent(abs); err != nil {
		return false, err
	}
	return true, nil
}

// assertPlainDescent walks abs one component at a time from the clone root,
// following exactly the route a consumer will hand to git, and refuses any
// component that is not an ordinary directory entry.
func (l Layout) assertPlainDescent(abs string) error {
	rel, err := filepath.Rel(l.Clone, abs)
	if err != nil {
		return fmt.Errorf("paths: relativise %q against clone: %w", abs, err)
	}
	cur := l.Clone
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		switch seg {
		case "", ".":
			continue
		case "..":
			// abs only landed inside the clone because a link was followed
			// on the way. Refuse rather than resolve.
			return fmt.Errorf("%w: %s", ErrLinkedComponent, abs)
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			// Nothing exists here yet, so this component cannot be a link
			// and neither can anything below it.
			return nil
		}
		if err != nil {
			return fmt.Errorf("paths: inspect path component %q: %w", cur, err)
		}
		// A POSIX symlink reports as ModeSymlink; a Windows junction reports
		// as ModeIrregular. Neither is a plain descent.
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("%w: %s", ErrLinkedComponent, cur)
		}
	}
	return nil
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
