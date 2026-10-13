package syncer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/gitops"
	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

func setup(t *testing.T) (*Syncer, Target, string) {
	t.Helper()
	testgit.Env(t)
	bare := testgit.NewOrigin(t, "main")
	dest := filepath.Join(t.TempDir(), "dest", "repo")
	s := &Syncer{Git: &gitops.Git{}, Org: "acme"}
	return s, Target{Name: "repo", Dir: dest, CloneURL: bare, DefaultBranch: "main"}, bare
}

func cloneIt(t *testing.T, bare, dest string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	testgit.Clone(t, bare, dest)
}

func write(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSync_ClonesMissing(t *testing.T) {
	s, tg, _ := setup(t)
	r := s.Sync(context.Background(), tg)
	if r.Action != Cloned || r.Err != nil || r.Name != "repo" {
		t.Fatalf("got %+v", r)
	}
	if got := testgit.Git(t, tg.Dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Fatalf("branch = %q", got)
	}
}

func TestSync_EmptySkipped(t *testing.T) {
	s, tg, _ := setup(t)
	tg.IsEmpty = true
	r := s.Sync(context.Background(), tg)
	if r.Action != Skipped || len(r.Warnings) != 1 || r.Warnings[0] != "empty repository" {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(tg.Dir); err == nil {
		t.Fatal("dir should not be created")
	}
}

func TestSync_NotARepo(t *testing.T) {
	s, tg, _ := setup(t)
	if err := os.MkdirAll(tg.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.Sync(context.Background(), tg)
	if r.Action != Skipped || len(r.Warnings) != 1 || r.Warnings[0] != "folder exists but is not a git repository" {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_CloneFailureContinues(t *testing.T) {
	s, tg, _ := setup(t)
	tg.CloneURL = filepath.Join(t.TempDir(), "does-not-exist.git")
	r := s.Sync(context.Background(), tg)
	if r.Action != Failed || r.Err == nil || r.Name != "repo" {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_UpToDate(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	r := s.Sync(context.Background(), tg)
	if r.Action != UpToDate || r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_PullsNewCommit(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Commit(t, bare, "main", "new.txt", "hello\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "new.txt")); err != nil {
		t.Fatalf("new file missing: %v", err)
	}
}

func TestSync_DirtyFetchesOnly(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	write(t, tg.Dir, "README.md", "dirty\n")
	testgit.Commit(t, bare, "main", "new.txt", "hello\n")
	before := testgit.Git(t, tg.Dir, "rev-parse", "HEAD")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "uncommitted changes, fetched only" {
		t.Fatalf("got %+v", r)
	}
	if got := testgit.Git(t, tg.Dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD moved")
	}
	if testgit.Git(t, tg.Dir, "rev-parse", "origin/main") == before {
		t.Fatal("origin/main did not advance")
	}
	b, _ := os.ReadFile(filepath.Join(tg.Dir, "README.md"))
	if string(b) != "dirty\n" {
		t.Fatalf("working file changed: %q", b)
	}
}

func TestSync_OperationInProgressFetchesOnly(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	gd := testgit.Git(t, tg.Dir, "rev-parse", "--absolute-git-dir")
	if err := os.WriteFile(filepath.Join(gd, "MERGE_HEAD"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "rebase/merge in progress, fetched only" {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_RebaseConflictAborted(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local change")
	before := testgit.Git(t, tg.Dir, "rev-parse", "HEAD")
	testgit.Commit(t, bare, "main", "README.md", "remote\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "rebase conflict, left unchanged" {
		t.Fatalf("got %+v", r)
	}
	if s.Git.OperationInProgress(tg.Dir) {
		t.Fatal("operation still in progress")
	}
	if got := testgit.Git(t, tg.Dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD changed")
	}
}

func TestSync_LocalCommitsRebased(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	write(t, tg.Dir, "local.txt", "local\n")
	testgit.Git(t, tg.Dir, "add", "local.txt")
	testgit.Git(t, tg.Dir, "commit", "-m", "local commit")
	testgit.Commit(t, bare, "main", "remote.txt", "remote\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if got := testgit.Git(t, tg.Dir, "log", "-1", "--format=%s"); got != "local commit" {
		t.Fatalf("top commit = %q", got)
	}
	if got := testgit.Git(t, tg.Dir, "log", "-2", "--format=%s"); !strings.Contains(got, "update remote.txt") {
		t.Fatalf("log = %q", got)
	}
}

func TestSync_NotOnDefaultBranchFetchesOnly(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "checkout", "-b", "feature")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "on branch feature, fetched only" {
		t.Fatalf("got %+v", r)
	}
}
