// Package store wraps the local git clone that holds committed memories.
//
// Reads are plain filesystem reads: instant, offline, and never rate limited.
// Sync is fetch plus fast-forward-only merge. This package never force-pushes,
// never rewrites history, and never runs gc.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/AndrewSkea/team-memory/mcp/paths"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

// ErrNotFastForward means the remote has commits the local clone cannot reach
// by fast-forward. Resolving that is a user decision, not something to
// automate.
var ErrNotFastForward = errors.New("store: remote has diverged; refusing non-fast-forward merge")

// Store is a handle on the local clone.
type Store struct {
	layout paths.Layout
	repo   *git.Repository
}

// Open opens the clone, initialising an empty repository if none exists.
func Open(l paths.Layout) (*Store, error) {
	if err := l.EnsureDirs(); err != nil {
		return nil, err
	}
	repo, err := git.PlainOpen(l.Clone)
	if errors.Is(err, git.ErrRepositoryNotExists) {
		repo, err = git.PlainInit(l.Clone, false)
	}
	if err != nil {
		return nil, fmt.Errorf("store: open clone at %q: %w", l.Clone, err)
	}
	return &Store{layout: l, repo: repo}, nil
}

// Layout exposes the paths this store was opened with.
func (s *Store) Layout() paths.Layout { return s.layout }

// Worktree returns the clone's worktree.
func (s *Store) Worktree() (*git.Worktree, error) {
	wt, err := s.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("store: worktree: %w", err)
	}
	return wt, nil
}

// Sync fetches from origin and fast-forwards the checked-out branch. A
// repository with no remote is a valid local-only state and syncs to a no-op.
func (s *Store) Sync(ctx context.Context) error {
	if _, err := s.repo.Remote(git.DefaultRemoteName); err != nil {
		if errors.Is(err, git.ErrRemoteNotFound) {
			return nil
		}
		return fmt.Errorf("store: lookup remote: %w", err)
	}

	err := s.repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: git.DefaultRemoteName,
		Force:      false,
		RefSpecs: []config.RefSpec{
			config.RefSpec("+refs/heads/*:refs/remotes/origin/*"),
		},
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return fmt.Errorf("store: fetch: %w", err)
	}

	head, err := s.repo.Head()
	if err != nil {
		// An empty repository has no HEAD yet. Nothing to fast-forward onto.
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil
		}
		return fmt.Errorf("store: head: %w", err)
	}

	branch := head.Name()
	remoteRef := plumbing.NewRemoteReferenceName(git.DefaultRemoteName, branch.Short())
	remote, err := s.repo.Reference(remoteRef, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return nil // branch does not exist upstream yet
		}
		return fmt.Errorf("store: resolve %s: %w", remoteRef, err)
	}
	if remote.Hash() == head.Hash() {
		return nil // already in sync
	}

	// Local ahead of remote: nothing to merge. This must be checked before
	// the fast-forward direction, or a local-only commit (Task 5 will start
	// making these) gets misreported as divergence.
	ahead, err := s.isAncestor(remote.Hash(), head.Hash())
	if err != nil {
		return err
	}
	if ahead {
		return nil
	}

	ff, err := s.isAncestor(head.Hash(), remote.Hash())
	if err != nil {
		return err
	}
	if !ff {
		return ErrNotFastForward
	}

	// Move the branch ref to the remote hash, then bring the working tree
	// across with a hard reset rather than Checkout-by-hash. Checkout with a
	// bare Hash detaches HEAD (writes it as a direct hash reference instead
	// of a symbolic ref to the branch); every subsequent Sync would then
	// resolve the branch name as the literal string "HEAD", fail to find
	// refs/remotes/origin/HEAD, and silently no-op forever. Reset does not
	// touch HEAD's symbolic-ness, so the branch stays attached. This hard
	// reset is safe only because the ancestry check above already proved
	// head is a strict ancestor of remote, so there is nothing local to
	// lose — keep that ordering.
	ref := plumbing.NewHashReference(branch, remote.Hash())
	if err := s.repo.Storer.SetReference(ref); err != nil {
		return fmt.Errorf("store: update %s: %w", branch, err)
	}
	wt, err := s.Worktree()
	if err != nil {
		return err
	}
	if err := wt.Reset(&git.ResetOptions{Commit: remote.Hash(), Mode: git.HardReset}); err != nil {
		return fmt.Errorf("store: fast-forward reset: %w", err)
	}
	return nil
}

// isAncestor reports whether ancestor is reachable from descendant.
func (s *Store) isAncestor(ancestor, descendant plumbing.Hash) (bool, error) {
	a, err := s.repo.CommitObject(ancestor)
	if err != nil {
		return false, fmt.Errorf("store: load commit %s: %w", ancestor, err)
	}
	d, err := s.repo.CommitObject(descendant)
	if err != nil {
		return false, fmt.Errorf("store: load commit %s: %w", descendant, err)
	}
	ok, err := a.IsAncestor(d)
	if err != nil {
		return false, fmt.Errorf("store: ancestry check: %w", err)
	}
	return ok, nil
}
