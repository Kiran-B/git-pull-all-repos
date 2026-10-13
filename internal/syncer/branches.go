package syncer

import (
	"context"
	"fmt"
	"strings"
)

// gitErrorDetail returns the first line of git's own message from a gitops
// error ("git <args>: <err>: <stderr>"), falling back to the whole text.
func gitErrorDetail(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && i+2 < len(msg) {
		msg = msg[i+2:]
	}
	first, _, _ := strings.Cut(msg, "\n")
	return strings.TrimSpace(first)
}

// cleanupGone deletes local branches whose upstream was deleted on GitHub,
// using the safe `git branch -d`. It never touches the working tree.
func (s *Syncer) cleanupGone(ctx context.Context, dir, current string) (warnings []string) {
	gone, err := s.Git.GoneBranches(ctx, dir)
	if err != nil {
		return []string{fmt.Sprintf("could not list gone branches: %v", err)}
	}
	for _, b := range gone {
		if b == current {
			warnings = append(warnings, fmt.Sprintf("current branch %s was deleted on GitHub", b))
			continue
		}
		if _, err := s.Git.Run(ctx, dir, "branch", "-d", b); err != nil {
			if strings.Contains(err.Error(), "not fully merged") {
				warnings = append(warnings, fmt.Sprintf("branch %s: deleted on GitHub but not merged, kept", b))
			} else {
				warnings = append(warnings, fmt.Sprintf("branch %s: could not delete: %s", b, gitErrorDetail(err)))
			}
		}
	}
	return warnings
}

// fastForwardDefault brings the local default branch up to origin/<def>
// without checking it out. A missing local branch is created tracking origin.
func (s *Syncer) fastForwardDefault(ctx context.Context, dir, def string) (moved bool, warning string) {
	remote, err := s.Git.RevParse(ctx, dir, "refs/remotes/origin/"+def)
	if err != nil {
		return false, ""
	}
	local, err := s.Git.RevParse(ctx, dir, "refs/heads/"+def)
	if err != nil {
		if _, err := s.Git.Run(ctx, dir, "branch", "--track", def, "origin/"+def); err != nil {
			return false, fmt.Sprintf("could not create local %s: %v", def, err)
		}
		return true, ""
	}
	if local == remote {
		return false, ""
	}
	behind, err := s.Git.IsAncestor(ctx, dir, local, remote)
	if err != nil {
		return false, fmt.Sprintf("could not update local %s: %v", def, err)
	}
	if behind {
		if elsewhere, err := s.Git.BranchCheckedOutElsewhere(ctx, dir, def); err != nil {
			return false, fmt.Sprintf("could not update local %s: %v", def, err)
		} else if elsewhere {
			return false, fmt.Sprintf("%s is checked out in another worktree, not updated", def)
		}
		if _, err := s.Git.Run(ctx, dir, "update-ref", "refs/heads/"+def, remote, local); err != nil {
			return false, fmt.Sprintf("could not update local %s: %v", def, err)
		}
		return true, ""
	}
	ahead, err := s.Git.IsAncestor(ctx, dir, remote, local)
	if err != nil {
		return false, fmt.Sprintf("could not update local %s: %v", def, err)
	}
	if ahead {
		return false, ""
	}
	return false, fmt.Sprintf("local %s has diverged from origin, not updated", def)
}

// updateFeature handles a repo that is detached or on a non-default branch:
// fast-forward the default branch, then rebase the current branch onto its
// upstream when that is safe.
func (s *Syncer) updateFeature(ctx context.Context, t Target, branch string, detached bool) Result {
	res := Result{Name: t.Name}
	if s.Git.OperationInProgress(t.Dir) {
		res.Action = FetchedOnly
		res.Warnings = append(res.Warnings, "rebase/merge in progress, fetched only")
		return res
	}
	moved, warning := s.fastForwardDefault(ctx, t.Dir, t.DefaultBranch)
	if warning != "" {
		res.Warnings = append(res.Warnings, warning)
	}
	finish := func(rebased bool) Result {
		if moved || rebased {
			res.Action = Updated
		} else {
			res.Action = UpToDate
		}
		return res
	}
	if detached {
		return finish(false)
	}
	upstream, gone, err := s.Git.Upstream(ctx, t.Dir, branch)
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if upstream == "" || gone {
		return finish(false)
	}
	if w, blocked, err := s.blocked(ctx, t.Dir); err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	} else if blocked {
		res.Action = FetchedOnly
		res.Warnings = append(res.Warnings, w)
		return res
	}
	rebased, w, err := s.rebaseOnto(ctx, t.Dir, "@{u}")
	if err != nil {
		return Result{Name: t.Name, Action: Failed, Err: err}
	}
	if w != "" {
		res.Action = FetchedOnly
		res.Warnings = append(res.Warnings, w)
		return res
	}
	res.Warnings = append(res.Warnings, s.updateSubmodules(ctx, t.Dir)...)
	return finish(rebased)
}
