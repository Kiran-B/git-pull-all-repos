package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/cli"
	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

type fixtureRepo struct {
	name   string
	url    string
	mirror bool
	pushed string
}

// fakeGh installs a fake gh on PATH that serves the given repos.
func fakeGh(t *testing.T, repos []fixtureRepo) {
	t.Helper()
	var list []map[string]any
	for _, r := range repos {
		list = append(list, map[string]any{
			"name":             r.name,
			"url":              r.url,
			"sshUrl":           r.url,
			"defaultBranchRef": map[string]string{"name": "main"},
			"primaryLanguage":  nil,
			"isArchived":       false,
			"isFork":           false,
			"isMirror":         r.mirror,
			"isEmpty":          false,
			"pushedAt":         r.pushed,
			"stargazerCount":   1,
		})
	}
	data, err := json.Marshal(list)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture.json")
	if err := os.WriteFile(fixture, data, 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1 $2" = "repo list" ]; then cat %q; exit 0; fi
if [ "$1 $2 $3" = "config get git_protocol" ]; then echo https; exit 0; fi
exit 1
`, fixture)
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func opts(t *testing.T) cli.Options {
	return cli.Options{Host: "github.com", Org: "acme", Dest: filepath.Join(t.TempDir(), "dest"), Sort: "pushed", Jobs: 1}
}

func run(t *testing.T, o cli.Options) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(context.Background(), o, &out, &errb)
	return code, out.String(), errb.String()
}

// setupOrigins creates origins named a, b, c with descending pushedAt.
func setupOrigins(t *testing.T, names ...string) map[string]string {
	t.Helper()
	origins := map[string]string{}
	var repos []fixtureRepo
	for i, n := range names {
		bare := testgit.NewOriginAt(t, "acme", n, "main")
		origins[n] = bare
		repos = append(repos, fixtureRepo{name: n, url: bare, pushed: fmt.Sprintf("2024-01-%02dT00:00:00Z", 20-i)})
	}
	fakeGh(t, repos)
	return origins
}

func TestRun_FirstRunClonesAll(t *testing.T) {
	testgit.Env(t)
	setupOrigins(t, "a", "b", "c")
	o := opts(t)
	code, out, errs := run(t, o)
	if code != 0 {
		t.Fatalf("exit %d\n%s\n%s", code, out, errs)
	}
	for _, n := range []string{"a", "b", "c"} {
		if _, err := os.Stat(filepath.Join(o.Dest, n, "README.md")); err != nil {
			t.Errorf("%s not cloned: %v", n, err)
		}
	}
	if !strings.Contains(out, "3 cloned") {
		t.Errorf("summary missing: %s", out)
	}
}

func TestRun_SecondRunUpdates(t *testing.T) {
	testgit.Env(t)
	origins := setupOrigins(t, "a")
	o := opts(t)
	if code, out, _ := run(t, o); code != 0 {
		t.Fatalf("first run exit %d\n%s", code, out)
	}
	testgit.Commit(t, origins["a"], "main", "new.txt", "x")
	code, out, _ := run(t, o)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(o.Dest, "a", "new.txt")); err != nil {
		t.Errorf("update not pulled: %v\n%s", err, out)
	}
}

func TestRun_LimitStillUpdatesExistingClones(t *testing.T) {
	testgit.Env(t)
	origins := setupOrigins(t, "a", "b")
	o := opts(t)
	if code, out, _ := run(t, o); code != 0 {
		t.Fatalf("first run exit %d\n%s", code, out)
	}
	for _, bare := range origins {
		testgit.Commit(t, bare, "main", "new.txt", "x")
	}
	o.Limit = 1
	code, out, _ := run(t, o)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// "b" is older so not selected by --limit 1.
	if _, err := os.Stat(filepath.Join(o.Dest, "b", "new.txt")); err != nil {
		t.Errorf("unselected clone not updated: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(o.Dest, "a", "new.txt")); err != nil {
		t.Errorf("selected clone not updated: %v\n%s", err, out)
	}
}

func TestRun_MirrorExcluded(t *testing.T) {
	testgit.Env(t)
	a := testgit.NewOriginAt(t, "acme", "a", "main")
	m := testgit.NewOriginAt(t, "acme", "m", "main")
	fakeGh(t, []fixtureRepo{
		{name: "a", url: a, pushed: "2024-01-02T00:00:00Z"},
		{name: "m", url: m, mirror: true, pushed: "2024-01-03T00:00:00Z"},
	})
	o := opts(t)
	if code, out, _ := run(t, o); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(o.Dest, "m")); !os.IsNotExist(err) {
		t.Errorf("mirror was cloned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(o.Dest, "a")); err != nil {
		t.Errorf("a missing: %v", err)
	}
}

func TestRun_FailureExitCode1(t *testing.T) {
	testgit.Env(t)
	a := testgit.NewOriginAt(t, "acme", "a", "main")
	b := testgit.NewOriginAt(t, "acme", "b", "main")
	fakeGh(t, []fixtureRepo{
		{name: "a", url: a, pushed: "2024-01-03T00:00:00Z"},
		{name: "gone", url: filepath.Join(t.TempDir(), "acme", "gone.git"), pushed: "2024-01-02T00:00:00Z"},
		{name: "b", url: b, pushed: "2024-01-01T00:00:00Z"},
	})
	o := opts(t)
	code, out, _ := run(t, o)
	if code != 1 {
		t.Fatalf("exit %d, want 1\n%s", code, out)
	}
	for _, n := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(o.Dest, n)); err != nil {
			t.Errorf("%s not cloned: %v", n, err)
		}
	}
}

func TestRun_MissingGh(t *testing.T) {
	testgit.Env(t)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(gitPath, filepath.Join(dir, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	code, _, errs := run(t, opts(t))
	if code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if !strings.HasPrefix(errs, "git-pull-all-repos: ") || !strings.Contains(errs, "gh") {
		t.Errorf("stderr: %q", errs)
	}
}

func TestRun_JobsParallelSameResult(t *testing.T) {
	testgit.Env(t)
	origins := setupOrigins(t, "a", "b", "c", "d", "e")
	o := opts(t)
	o.Jobs = 3
	if code, out, _ := run(t, o); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for _, bare := range origins {
		testgit.Commit(t, bare, "main", "new.txt", "x")
	}
	code, out, _ := run(t, o)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	for n := range origins {
		if _, err := os.Stat(filepath.Join(o.Dest, n, "new.txt")); err != nil {
			t.Errorf("%s not updated: %v", n, err)
		}
	}
	if !strings.Contains(out, "5 updated") {
		t.Errorf("summary: %s", out)
	}
}

func TestRun_DryRunCreatesNothing(t *testing.T) {
	testgit.Env(t)
	setupOrigins(t, "a", "b")
	o := opts(t)
	o.DryRun = true
	code, out, _ := run(t, o)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if _, err := os.Stat(o.Dest); !os.IsNotExist(err) {
		t.Errorf("dest exists after dry run: %v", err)
	}
	if !strings.Contains(out, "2 would clone") {
		t.Errorf("summary: %s", out)
	}
}
