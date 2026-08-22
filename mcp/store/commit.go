package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AndrewSkea/team-memory/mcp/memory"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// ErrOutsideClone is returned when a commit is attempted on a path that does
// not resolve inside the clone worktree. Every write funnels through the check
// that produces this error.
//
// The privacy guarantee of the product rests on this error: staging/ is a
// sibling of clone/, so anything under staging/ resolves outside the clone and
// is refused here before it can reach the git index.
var ErrOutsideClone = errors.New("store: refusing to commit a path outside the clone")

// ErrGitInternal is returned when a path resolves inside the clone but targets
// git's own metadata directory. Nothing legitimate writes there through this
// store, and staging it would corrupt the repository.
var ErrGitInternal = errors.New("store: refusing to commit a path inside .git")

// Put writes a memory to its content-addressed path and commits it.
func (s *Store) Put(m memory.Memory, msg string) (plumbing.Hash, error) {
	rel, err := memory.RepoPath(m)
	if err != nil {
		return plumbing.ZeroHash, err
	}
	data, err := memory.Marshal(m)
	if err != nil {
		return plumbing.ZeroHash, err
	}

	// The candidate path is derived by joining the clone root with a
	// repo-relative path built by memory.RepoPath. Deriving it any other way
	// — from an independently-cased or caller-supplied absolute string —
	// would put the guard's case sensitivity in play on Windows. Keep this
	// joining discipline in every new write path.
	abs := filepath.Join(s.layout.Clone, filepath.FromSlash(rel))
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: create directory for %q: %w", rel, err)
	}
	if err := os.WriteFile(abs, data, 0o600); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: write %q: %w", rel, err)
	}
	return s.commitPath(abs, msg)
}

// Remove deletes a committed memory file and commits the deletion. The blob
// remains in git history — deletion is described honestly as "stop surfacing
// this", not as erasure.
func (s *Store) Remove(repoPath, msg string) (plumbing.Hash, error) {
	abs := filepath.Join(s.layout.Clone, filepath.FromSlash(repoPath))
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := os.Remove(abs); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: remove %q: %w", repoPath, err)
	}
	return s.commitPath(abs, msg)
}

// assertInsideClone is the guard. Every path that reaches git passes through
// here first, and there is exactly one implementation of the check so that
// there is exactly one place to audit.
//
// It applies three tests, all of which fail closed — any error resolving a
// path is a refusal, never an admission:
//
//  1. paths.IsInsideClone, which resolves symlinks over the longest existing
//     prefix, so a POSIX symlink pointing out to staging is refused.
//  2. A walk of every path component from the clone root down, refusing any
//     component that is not a plain directory or regular file. This closes a
//     hole that test 1 alone does not: on Windows, filepath.EvalSymlinks does
//     not follow a directory junction (verified on go1.26 — it returns the
//     junction path unchanged with a nil error), so a junction planted inside
//     the clone and pointing at staging would otherwise be judged "inside".
//     Because the walk starts at the clone root and follows exactly the
//     relative path handed to worktree.Add, it checks the route git will
//     actually take.
//  3. A refusal of anything under the clone's own .git directory.
func (s *Store) assertInsideClone(abs string) error {
	inside, err := s.layout.IsInsideClone(abs)
	if err != nil {
		return fmt.Errorf("store: check path %q: %w", abs, err)
	}
	if !inside {
		return fmt.Errorf("%w: %s", ErrOutsideClone, abs)
	}

	rel, err := filepath.Rel(s.layout.Clone, abs)
	if err != nil {
		return fmt.Errorf("store: relativise %q: %w", abs, err)
	}
	cur := s.layout.Clone
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "" || seg == "." {
			continue
		}
		if seg == ".git" {
			return fmt.Errorf("%w: %s", ErrGitInternal, abs)
		}
		cur = filepath.Join(cur, seg)
		fi, err := os.Lstat(cur)
		if errors.Is(err, os.ErrNotExist) {
			// Nothing exists here yet, so this component cannot be a link
			// and neither can anything below it.
			continue
		}
		if err != nil {
			return fmt.Errorf("store: inspect path component %q: %w", cur, err)
		}
		// A junction reports as ModeIrregular; a symlink as ModeSymlink.
		// Nothing this store legitimately writes is either.
		if fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return fmt.Errorf("%w: %s is a link, not a real directory entry", ErrOutsideClone, cur)
		}
	}
	return nil
}

// commitPath stages one absolute path and commits it. It re-runs the guard so
// that no future caller can bypass the check by calling this directly.
//
// This is the ONLY place in the codebase that may call worktree.Add. Any new
// write path must funnel through here.
func (s *Store) commitPath(abs, msg string) (plumbing.Hash, error) {
	if err := s.assertInsideClone(abs); err != nil {
		return plumbing.ZeroHash, err
	}
	wt, err := s.Worktree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	rel, err := filepath.Rel(s.layout.Clone, abs)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: relativise %q: %w", abs, err)
	}
	slashed := filepath.ToSlash(rel)

	if _, err := wt.Add(slashed); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: stage %q: %w", slashed, err)
	}
	hash, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "team-memory",
			Email: "team-memory@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("store: commit %q: %w", slashed, err)
	}
	return hash, nil
}
