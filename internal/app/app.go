// Package app wires listing, syncing and reporting together.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Kiran-B/git-pull-all-repos/internal/cli"
	"github.com/Kiran-B/git-pull-all-repos/internal/github"
	"github.com/Kiran-B/git-pull-all-repos/internal/gitops"
	"github.com/Kiran-B/git-pull-all-repos/internal/report"
	"github.com/Kiran-B/git-pull-all-repos/internal/syncer"
)

// Run executes the whole sync and returns the process exit code.
func Run(ctx context.Context, opts cli.Options, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "git-pull-all-repos: %v\n", err)
		return 2
	}

	for _, bin := range []string{"git", "gh"} {
		if _, err := exec.LookPath(bin); err != nil {
			return fail(err)
		}
	}

	lister := github.Lister{Host: opts.Host}
	protocol, err := lister.GitProtocol(ctx)
	if err != nil {
		return fail(err)
	}
	repos, err := lister.List(ctx, opts.Org, github.Filter{
		NoForks:    opts.NoForks,
		NoArchived: opts.NoArchived,
		Language:   opts.Language,
	})
	if err != nil {
		return fail(err)
	}
	selected := github.Select(repos, opts.Sort, opts.Limit)

	if !opts.DryRun {
		if err := os.MkdirAll(opts.Dest, 0o755); err != nil {
			return fail(err)
		}
	}

	g := &gitops.Git{Host: opts.Host}
	targets := make([]syncer.Target, 0, len(selected))
	listed := make(map[string]bool, len(selected))
	for _, r := range selected {
		listed[strings.ToLower(r.Name)] = true
		targets = append(targets, syncer.Target{
			Name:          r.Name,
			Dir:           filepath.Join(opts.Dest, r.Name),
			CloneURL:      r.CloneURL(protocol),
			DefaultBranch: r.DefaultBranch,
			IsEmpty:       r.IsEmpty,
		})
	}
	discovered, err := syncer.Discover(ctx, g, opts.Dest, opts.Host, opts.Org, listed)
	if err != nil {
		return fail(err)
	}
	targets = append(targets, discovered...)

	s := &syncer.Syncer{Git: g, Submodules: !opts.NoSubmodules, DryRun: opts.DryRun, Org: opts.Org}
	rep := &report.Reporter{Out: stdout, Color: useColor(stdout)}

	jobs := opts.Jobs
	if jobs < 1 {
		jobs = 1
	}
	work := make(chan syncer.Target)
	out := make(chan syncer.Result)
	var wg sync.WaitGroup
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range work {
				out <- s.Sync(ctx, t)
			}
		}()
	}
	go func() {
		defer close(work)
		for _, t := range targets {
			if ctx.Err() != nil {
				return
			}
			select {
			case work <- t:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(out)
	}()

	var results []syncer.Result
	for res := range out {
		rep.Line(res)
		results = append(results, res)
	}

	interrupted := ctx.Err() != nil
	rep.Summary(results, interrupted)
	if code := report.ExitCode(results); code != 0 {
		return code
	}
	if interrupted {
		return 1
	}
	return 0
}

// useColor reports whether w is a terminal and NO_COLOR is unset.
func useColor(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
