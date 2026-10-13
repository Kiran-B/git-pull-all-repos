package syncer

import (
	"context"
	"fmt"
	"os"
	"strings"
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

	if ctx.Err() != nil {
		return skipped(t, "interrupted, not started")
	}
	if t.IsEmpty {
		return skipped(t, "empty repository")
	}

	if _, err := os.Stat(t.Dir); os.IsNotExist(err) {
		if s.DryRun {
			return Result{Name: t.Name, Action: WouldClone}
		}
		return s.clone(ctx, t)
	}
	if !s.Git.IsRepo(t.Dir) {
		return skipped(t, "folder exists but is not a git repository")
	}

	url, err := s.Git.OriginURL(ctx, t.Dir)
	if err != nil || url == "" {
		return skipped(t, "no origin remote")
	}
	if !s.originMatches(url, t) {
		return skipped(t, fmt.Sprintf("origin points to %s, not this repo", url))
	}

	if s.DryRun {
		if t.DefaultBranch == "" {
			b, err := s.Git.LocalDefaultBranch(ctx, t.Dir)
			if err != nil {
				return Result{Name: t.Name, Action: WouldUpdate, Warnings: []string{"default branch unknown (origin/HEAD not set)"}}
			}
			t.DefaultBranch = b
		}
		return s.dryRun(ctx, t)
	}

	if _, err := s.Git.RunNet(ctx, t.Dir, "fetch", "--prune", "--tags", "--force", "origin"); err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if t.DefaultBranch == "" {
		b, err := s.Git.DefaultBranchFromOrigin(ctx, t.Dir)
		if err != nil {
			return Result{Name: t.Name, Action: Failed, Err: err}
		}
		t.DefaultBranch = b
	}

	branch, detached, err := s.Git.CurrentBranch(ctx, t.Dir)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	warnings := s.cleanupGone(ctx, t.Dir, branch)
	if detached || branch != t.DefaultBranch {
		res = s.updateFeature(ctx, t, branch, detached)
	} else {
		res = s.updateDefault(ctx, t)
	}
	res.Warnings = append(warnings, res.Warnings...)
	return res
}

func skipped(t Target, warning string) Result {
	return Result{Name: t.Name, Action: Skipped, Warnings: []string{warning}}
}

func fetchedOnly(t Target, warning string) Result {
	return Result{Name: t.Name, Action: FetchedOnly, Warnings: []string{warning}}
}

// originMatches reports whether url belongs to this repo (or, for discovered
// clones, just to the org).
func (s *Syncer) originMatches(url string, t Target) bool {
	host := s.Git.Host
	if host == "" {
		host = "github.com"
	}
	if t.Discovered {
		return originOwnerMatches(url, host, s.Org)
	}
	return OriginMatches(url, host, s.Org, t.Name)
}

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

func (s *Syncer) updateDefault(ctx context.Context, t Target) Result {
	if w, blocked, err := s.blocked(ctx, t.Dir); err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	} else if blocked {
		return fetchedOnly(t, w)
	}
	moved, w, err := s.rebaseOnto(ctx, t.Dir, "origin/"+t.DefaultBranch)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if w != "" {
		return fetchedOnly(t, w)
	}
	res := Result{Name: t.Name, Action: UpToDate}
	if moved {
		res.Action = Updated
	}
	res.Warnings = s.updateSubmodules(ctx, t.Dir)
	return res
}

// updateSubmodules brings submodules to the commits recorded by the checked-out
// branch. A failure is only a warning.
func (s *Syncer) updateSubmodules(ctx context.Context, dir string) []string {
	if !s.Submodules {
		return nil
	}
	if _, err := s.Git.RunNet(ctx, dir, "submodule", "update", "--init", "--recursive"); err != nil {
		return []string{fmt.Sprintf("submodule update failed: %v", err)}
	}
	return nil
}

// dryRun reports what a real run would do, using read-only git queries only.
func (s *Syncer) dryRun(ctx context.Context, t Target) Result {
	dir := t.Dir
	res := Result{Name: t.Name, Action: WouldUpdate}
	if s.Git.OperationInProgress(dir) {
		res.Warnings = append(res.Warnings, "rebase/merge in progress, fetched only")
		return res
	}
	dirty, err := s.Git.HasTrackedChanges(ctx, dir)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if dirty {
		res.Warnings = append(res.Warnings, "uncommitted changes, would fetch only")
	}
	branch, detached, err := s.Git.CurrentBranch(ctx, dir)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if detached {
		res.Warnings = append(res.Warnings, "detached HEAD; default would be fast-forwarded")
	} else if branch != t.DefaultBranch {
		res.Warnings = append(res.Warnings, fmt.Sprintf("on branch %s; default would be fast-forwarded", branch))
	}
	return res
}

// blocked reports (with a warning) whether the working tree state prevents a rebase.
func (s *Syncer) blocked(ctx context.Context, dir string) (warning string, blocked bool, err error) {
	if s.Git.OperationInProgress(dir) {
		return "rebase/merge in progress, fetched only", true, nil
	}
	dirty, err := s.Git.HasTrackedChanges(ctx, dir)
	if err != nil {
		return "", false, err
	}
	if dirty {
		return "uncommitted changes, fetched only", true, nil
	}
	return "", false, nil
}

// rebaseOnto rebases the current branch onto upstream. A non-empty warning
// means the repo was left unchanged; err is set only if cleanup failed or git
// state could not be read.
func (s *Syncer) rebaseOnto(ctx context.Context, dir, upstream string) (moved bool, warning string, err error) {
	before, err := s.Git.RevParse(ctx, dir, "HEAD")
	if err != nil {
		return false, "", err
	}
	if _, rerr := s.Git.Run(ctx, dir, "rebase", upstream); rerr != nil {
		if !s.Git.OperationInProgress(dir) {
			first, _, _ := strings.Cut(rerr.Error(), "\n")
			return false, "rebase failed, left unchanged: " + first, nil
		}
		if _, abortErr := s.Git.Run(context.WithoutCancel(ctx), dir, "rebase", "--abort"); abortErr != nil {
			return false, "", fmt.Errorf("%w; abort failed: %w", rerr, abortErr)
		}
		return false, "rebase conflict, left unchanged", nil
	}
	after, err := s.Git.RevParse(ctx, dir, "HEAD")
	if err != nil {
		return false, "", err
	}
	return after != before, "", nil
}
