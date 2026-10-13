package gitops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

func setup(t *testing.T) (*Git, string, string) {
	t.Helper()
	testgit.Env(t)
	bare := testgit.NewOrigin(t, "main")
	dest := filepath.Join(t.TempDir(), "clone")
	testgit.Clone(t, bare, dest)
	return &Git{ExtraConfig: []string{"protocol.file.allow=always"}}, bare, dest
}

func TestIsRepo(t *testing.T) {
	g, _, dest := setup(t)
	if !g.IsRepo(dest) {
		t.Error("clone root should be a repo")
	}
	sub := filepath.Join(dest, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if g.IsRepo(sub) {
		t.Error("subdirectory of a repo must not count")
	}
	if g.IsRepo(filepath.Join(dest, "missing")) {
		t.Error("missing dir must not be a repo")
	}
}

func TestOriginURL(t *testing.T) {
	g, bare, dest := setup(t)
	got, err := g.OriginURL(context.Background(), dest)
	if err != nil || got != bare {
		t.Errorf("OriginURL = %q, %v; want %q", got, err, bare)
	}
}

func TestHasTrackedChanges(t *testing.T) {
	g, _, dest := setup(t)
	ctx := context.Background()
	if ch, err := g.HasTrackedChanges(ctx, dest); err != nil || ch {
		t.Fatalf("clean: %v %v", ch, err)
	}
	if err := os.WriteFile(filepath.Join(dest, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch, err := g.HasTrackedChanges(ctx, dest); err != nil || ch {
		t.Fatalf("untracked only should be false: %v %v", ch, err)
	}
	if err := os.WriteFile(filepath.Join(dest, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ch, err := g.HasTrackedChanges(ctx, dest); err != nil || !ch {
		t.Fatalf("modified tracked should be true: %v %v", ch, err)
	}
}

func TestCurrentBranch(t *testing.T) {
	g, _, dest := setup(t)
	ctx := context.Background()
	b, det, err := g.CurrentBranch(ctx, dest)
	if err != nil || det || b != "main" {
		t.Fatalf("got %q %v %v", b, det, err)
	}
	testgit.Git(t, dest, "checkout", "--detach")
	b, det, err = g.CurrentBranch(ctx, dest)
	if err != nil || !det || b != "" {
		t.Fatalf("detached: got %q %v %v", b, det, err)
	}
}

func TestDefaultBranchFromOrigin(t *testing.T) {
	g, _, dest := setup(t)
	ctx := context.Background()
	if b, err := g.DefaultBranchFromOrigin(ctx, dest); err != nil || b != "main" {
		t.Fatalf("got %q %v", b, err)
	}
	testgit.Git(t, dest, "remote", "set-head", "origin", "-d")
	if b, err := g.DefaultBranchFromOrigin(ctx, dest); err != nil || b != "main" {
		t.Fatalf("after -d: got %q %v", b, err)
	}
}

func TestUpstreamAndGone(t *testing.T) {
	g, bare, dest := setup(t)
	ctx := context.Background()
	ref, gone, err := g.Upstream(ctx, dest, "main")
	if err != nil || ref != "origin/main" || gone {
		t.Fatalf("main: %q %v %v", ref, gone, err)
	}
	testgit.Git(t, dest, "branch", "local-only")
	ref, gone, err = g.Upstream(ctx, dest, "local-only")
	if err != nil || ref != "" || gone {
		t.Fatalf("local-only: %q %v %v", ref, gone, err)
	}

	testgit.Git(t, dest, "push", "origin", "main:feature")
	testgit.Git(t, dest, "fetch", "origin")
	testgit.Git(t, dest, "branch", "--set-upstream-to=origin/feature", "local-only")
	testgit.Git(t, bare, "branch", "-D", "feature")
	testgit.Git(t, dest, "fetch", "--prune")
	ref, gone, err = g.Upstream(ctx, dest, "local-only")
	if err != nil || ref != "origin/feature" || !gone {
		t.Fatalf("gone: %q %v %v", ref, gone, err)
	}
	list, err := g.GoneBranches(ctx, dest)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range list {
		if b == "local-only" {
			found = true
		}
		if b == "main" {
			t.Errorf("main must not be gone: %v", list)
		}
	}
	if !found {
		t.Errorf("local-only missing from %v", list)
	}
}

func TestRevParseAndIsAncestor(t *testing.T) {
	g, bare, dest := setup(t)
	ctx := context.Background()
	old, err := g.RevParse(ctx, dest, "HEAD")
	if err != nil || len(old) != 40 {
		t.Fatalf("rev-parse: %q %v", old, err)
	}
	testgit.Commit(t, bare, "main", "a.txt", "a")
	testgit.Git(t, dest, "fetch")
	if anc, err := g.IsAncestor(ctx, dest, "HEAD", "origin/main"); err != nil || !anc {
		t.Errorf("HEAD should be ancestor of origin/main: %v %v", anc, err)
	}
	if anc, err := g.IsAncestor(ctx, dest, "origin/main", "HEAD"); err != nil || anc {
		t.Errorf("origin/main should not be ancestor of HEAD: %v %v", anc, err)
	}
	if _, err := g.RevParse(ctx, dest, "nosuchref"); err == nil {
		t.Error("expected error for unknown ref")
	}
}

func TestOperationInProgress(t *testing.T) {
	g, bare, dest := setup(t)
	if g.OperationInProgress(dest) {
		t.Fatal("no operation expected")
	}
	if err := os.WriteFile(filepath.Join(dest, "a.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	testgit.Git(t, dest, "add", "a.txt")
	testgit.Git(t, dest, "commit", "-m", "local a")
	testgit.Commit(t, bare, "main", "a.txt", "remote")
	testgit.Git(t, dest, "fetch")
	if _, err := g.Run(context.Background(), dest, "rebase", "origin/main"); err == nil {
		t.Fatal("expected rebase conflict")
	}
	if !g.OperationInProgress(dest) {
		t.Error("rebase in progress not detected")
	}
}

func TestRunNetFailsFastWithStderr(t *testing.T) {
	g, _, dest := setup(t)
	start := time.Now()
	_, err := g.RunNet(context.Background(), dest, "ls-remote", "file:///nope")
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 10*time.Second {
		t.Error("took too long")
	}
	if !strings.Contains(err.Error(), "does not appear to be a git repository") {
		t.Errorf("error should include stderr: %v", err)
	}
}

func TestRunErrorIncludesStderr(t *testing.T) {
	g, _, dest := setup(t)
	_, err := g.Run(context.Background(), dest, "bogus-cmd")
	if err == nil || !strings.Contains(err.Error(), "is not a git command") {
		t.Errorf("error should include stderr: %v", err)
	}
}

func TestRunNetBuildsCredentialArgs(t *testing.T) {
	g := &Git{Host: "example.com", ExtraConfig: []string{"a=b"}}
	got := g.buildArgs(true, []string{"fetch"})
	want := []string{"-c", "a=b",
		"-c", "credential.https://example.com.helper=",
		"-c", "credential.https://example.com.helper=!gh auth git-credential",
		"fetch"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("got %q want %q", got, want)
	}
	if def := (&Git{}).buildArgs(true, []string{"x"}); !strings.Contains(def[1], "https://github.com.") {
		t.Errorf("default host: %q", def)
	}
	if plain := g.buildArgs(false, []string{"status"}); len(plain) != 3 {
		t.Errorf("plain: %q", plain)
	}
}

func TestHasTrackedChanges_DoesNotWriteIndex(t *testing.T) {
	g, _, dest := setup(t)
	f := filepath.Join(dest, "README.md")
	// Refresh the mtime without changing content so a plain `git status`
	// would want to rewrite the index.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(f, future, future); err != nil {
		t.Fatal(err)
	}
	idx := filepath.Join(dest, ".git", "index")
	before, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	dirty, err := g.HasTrackedChanges(context.Background(), dest)
	if err != nil || dirty {
		t.Fatalf("dirty=%v err=%v", dirty, err)
	}
	after, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("git status rewrote the index")
	}
}
