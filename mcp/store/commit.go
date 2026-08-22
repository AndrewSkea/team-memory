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

// assertInsideClone is the guard: the single funnel every write path in this
// package passes through, and the single place to audit. Task 9's PutRaw must
// call this and nothing else.
//
// The link-component check that closes the Windows-junction hole now lives in
// paths.IsInsideClone, so the primitive is safe for every consumer rather than
// only for this one. This method adds the two rules that are the store's own
// business:
//
//   - The target must be STRICTLY inside the clone. The clone root itself
//     resolves "inside", but staging it would mean worktree.Add(".") — every
//     file in the worktree in one commit. Every commit this store makes is one
//     memory file, and a guard whose blast radius is the whole worktree is one
//     refactor away from committing something nobody reviewed.
//   - Nothing under .git, which is inside the clone but never ours to stage.
//
// Every branch fails closed: any error resolving a path is a refusal, never an
// admission.
func (s *Store) assertInsideClone(abs string) error {
	inside, err := s.layout.IsInsideClone(abs)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrOutsideClone, abs, err)
	}
	if !inside {
		return fmt.Errorf("%w: %s", ErrOutsideClone, abs)
	}

	rel, err := filepath.Rel(s.layout.Clone, abs)
	if err != nil {
		return fmt.Errorf("store: relativise %q: %w", abs, err)
	}
	slashed := filepath.ToSlash(rel)
	if slashed == "." || slashed == "" {
		return fmt.Errorf("%w: %s is the clone root, not a file within it", ErrOutsideClone, abs)
	}
	for _, seg := range strings.Split(slashed, "/") {
		if seg == ".git" {
			return fmt.Errorf("%w: %s", ErrGitInternal, abs)
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
