// Package gitops wraps the git CLI for repo state queries and commands.
package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Git runs git commands. Host is the GitHub host used for credential helper
// configuration on network calls (default github.com). ExtraConfig entries
// are key=value pairs passed as -c on every call.
type Git struct {
	Host        string
	ExtraConfig []string
}

func (g *Git) host() string {
	if g.Host == "" {
		return "github.com"
	}
	return g.Host
}

func (g *Git) buildArgs(net bool, args []string) []string {
	var out []string
	for _, c := range g.ExtraConfig {
		out = append(out, "-c", c)
	}
	if net {
		key := "credential.https://" + g.host() + ".helper"
		out = append(out, "-c", key+"=", "-c", key+"=!gh auth git-credential")
	}
	return append(out, args...)
}

// exec runs git and returns trimmed stdout, trimmed stderr and the raw error.
func (g *Git) exec(ctx context.Context, net bool, dir string, args []string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", g.buildArgs(net, args)...)
	cmd.Dir = dir
	// On cancellation ask git to stop (so it can clean up) instead of SIGKILL.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return strings.TrimSpace(stdout.String()), strings.TrimSpace(stderr.String()), err
}

func (g *Git) run(ctx context.Context, net bool, dir string, args []string) (string, error) {
	out, errOut, err := g.exec(ctx, net, dir, args)
	if err != nil {
		return out, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errOut)
	}
	return out, nil
}

// Run runs git in dir and returns trimmed stdout. Errors include stderr.
func (g *Git) Run(ctx context.Context, dir string, args ...string) (string, error) {
	return g.run(ctx, false, dir, args)
}

// RunNet is like Run but adds the gh credential helper configuration.
func (g *Git) RunNet(ctx context.Context, dir string, args ...string) (string, error) {
	return g.run(ctx, true, dir, args)
}

// IsRepo reports whether dir is itself the top level of a git work tree.
func (g *Git) IsRepo(dir string) bool {
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	top, err := g.Run(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	a, err1 := filepath.EvalSymlinks(top)
	b, err2 := filepath.EvalSymlinks(dir)
	return err1 == nil && err2 == nil && a == b
}

// OriginURL returns the URL of the origin remote.
func (g *Git) OriginURL(ctx context.Context, dir string) (string, error) {
	return g.Run(ctx, dir, "remote", "get-url", "origin")
}

// CurrentBranch returns the checked-out branch, or detached=true on a detached HEAD.
func (g *Git) CurrentBranch(ctx context.Context, dir string) (string, bool, error) {
	out, errOut, err := g.exec(ctx, false, dir, []string{"symbolic-ref", "--short", "-q", "HEAD"})
	if err == nil {
		return out, false, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && out == "" && errOut == "" {
		return "", true, nil
	}
	return "", false, fmt.Errorf("git symbolic-ref: %w: %s", err, errOut)
}

// HasTrackedChanges reports whether tracked files have uncommitted changes.
func (g *Git) HasTrackedChanges(ctx context.Context, dir string) (bool, error) {
	out, err := g.Run(ctx, dir, "--no-optional-locks", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// OperationInProgress reports whether a rebase, merge or cherry-pick is underway.
func (g *Git) OperationInProgress(dir string) bool {
	gd, err := g.Run(context.Background(), dir, "rev-parse", "--git-dir")
	if err != nil {
		return false
	}
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD"} {
		if _, err := os.Stat(filepath.Join(gd, name)); err == nil {
			return true
		}
	}
	return false
}

// LocalDefaultBranch returns the branch refs/remotes/origin/HEAD points to,
// without touching the network. It errors if origin/HEAD is not set.
func (g *Git) LocalDefaultBranch(ctx context.Context, dir string) (string, error) {
	out, err := g.Run(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(out, "origin/"), nil
}

// DefaultBranchFromOrigin returns the branch origin/HEAD points to. If it is
// not set, it runs `remote set-head origin --auto` once and retries.
func (g *Git) DefaultBranchFromOrigin(ctx context.Context, dir string) (string, error) {
	if b, err := g.LocalDefaultBranch(ctx, dir); err == nil {
		return b, nil
	}
	if _, err := g.RunNet(ctx, dir, "remote", "set-head", "origin", "--auto"); err != nil {
		return "", err
	}
	return g.LocalDefaultBranch(ctx, dir)
}

// Upstream returns the upstream ref of branch ("" if none) and whether it is gone.
func (g *Git) Upstream(ctx context.Context, dir, branch string) (string, bool, error) {
	out, err := g.Run(ctx, dir, "for-each-ref", "--format=%(upstream:short)%09%(upstream:track)", "refs/heads/"+branch)
	if err != nil {
		return "", false, err
	}
	ref, track, _ := strings.Cut(out, "\t")
	return ref, strings.TrimSpace(track) == "[gone]", nil
}

// GoneBranches lists local branches whose upstream no longer exists.
func (g *Git) GoneBranches(ctx context.Context, dir string) ([]string, error) {
	out, err := g.Run(ctx, dir, "for-each-ref", "--format=%(refname:short)%09%(upstream:track)", "refs/heads")
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, line := range strings.Split(out, "\n") {
		name, track, _ := strings.Cut(line, "\t")
		if name != "" && strings.TrimSpace(track) == "[gone]" {
			gone = append(gone, name)
		}
	}
	return gone, nil
}

// RevParse resolves ref to a commit id.
func (g *Git) RevParse(ctx context.Context, dir, ref string) (string, error) {
	return g.Run(ctx, dir, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
}

// IsAncestor reports whether a is an ancestor of b.
func (g *Git) IsAncestor(ctx context.Context, dir, a, b string) (bool, error) {
	_, errOut, err := g.exec(ctx, false, dir, []string{"merge-base", "--is-ancestor", a, b})
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w: %s", a, b, err, errOut)
}

// BranchCheckedOutElsewhere reports whether branch is checked out in any
// worktree of the repository at dir (callers use it for branches that are not
// checked out in dir itself).
func (g *Git) BranchCheckedOutElsewhere(ctx context.Context, dir, branch string) (bool, error) {
	out, err := g.Run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	want := "branch refs/heads/" + branch
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true, nil
		}
	}
	return false, nil
}
