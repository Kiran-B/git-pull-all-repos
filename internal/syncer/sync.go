package syncer

import (
	"context"
	"fmt"
	"os"
)

// Sync clones or updates one repository. It never panics and always returns
// a Result with Name set.
func (s *Syncer) Sync(ctx context.Context, t Target) (res Result) {
	res = Result{Name: t.Name}
	defer func() {
		if r := recover(); r != nil {
			res = Result{Name: t.Name, Action: Failed, Err: fmt.Errorf("internal error: %v", r)}
		}
	}()

	if t.IsEmpty {
		return skipped(t, "empty repository")
	}

	if _, err := os.Stat(t.Dir); os.IsNotExist(err) {
		return s.clone(ctx, t)
	}
	if !s.Git.IsRepo(t.Dir) {
		return skipped(t, "folder exists but is not a git repository")
	}

	url, _ := s.Git.OriginURL(ctx, t.Dir)
	if !s.originMatches(url, t.Name) {
		return skipped(t, fmt.Sprintf("origin points to %s, not this repo", url))
	}

	if _, err := s.Git.RunNet(ctx, t.Dir, "fetch", "--prune", "--tags", "--force", "origin"); err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}

	branch, detached, err := s.Git.CurrentBranch(ctx, t.Dir)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if detached || branch != t.DefaultBranch {
		return s.offDefaultBranch(t, branch, detached)
	}
	return s.updateDefault(ctx, t)
}

func skipped(t Target, warning string) Result {
	return Result{Name: t.Name, Action: Skipped, Warnings: []string{warning}}
}

func fetchedOnly(t Target, warning string) Result {
	return Result{Name: t.Name, Action: FetchedOnly, Warnings: []string{warning}}
}

// originMatches is a stub; Task 7 replaces it with a real origin comparison.
func (s *Syncer) originMatches(url, name string) bool { return true }

func (s *Syncer) clone(ctx context.Context, t Target) Result {
	args := []string{"clone"}
	if s.Submodules {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, t.CloneURL, t.Dir)
	if _, err := s.Git.RunNet(ctx, "", args...); err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	return Result{Name: t.Name, Action: Cloned}
}

// offDefaultBranch handles a repo that is detached or on a non-default branch.
// Task 5 replaces this with the feature-branch path.
func (s *Syncer) offDefaultBranch(t Target, branch string, detached bool) Result {
	if detached {
		return fetchedOnly(t, "detached HEAD, fetched only")
	}
	return fetchedOnly(t, fmt.Sprintf("on branch %s, fetched only", branch))
}

func (s *Syncer) updateDefault(ctx context.Context, t Target) Result {
	if s.Git.OperationInProgress(t.Dir) {
		return fetchedOnly(t, "rebase/merge in progress, fetched only")
	}
	dirty, err := s.Git.HasTrackedChanges(ctx, t.Dir)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if dirty {
		return fetchedOnly(t, "uncommitted changes, fetched only")
	}

	before, err := s.Git.RevParse(ctx, t.Dir, "HEAD")
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if _, err := s.Git.Run(ctx, t.Dir, "rebase", "origin/"+t.DefaultBranch); err != nil {
		if _, abortErr := s.Git.Run(ctx, t.Dir, "rebase", "--abort"); abortErr != nil {
			return Result{Name: t.Name, Action: Failed, Err: fmt.Errorf("%w; abort failed: %w", err, abortErr)}
		}
		return fetchedOnly(t, "rebase conflict, left unchanged")
	}
	after, err := s.Git.RevParse(ctx, t.Dir, "HEAD")
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if after != before {
		return Result{Name: t.Name, Action: Updated}
	}
	return Result{Name: t.Name, Action: UpToDate}
}
