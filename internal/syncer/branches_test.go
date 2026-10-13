package syncer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

// scratch clones bare into a temp dir for manipulating origin.
func scratch(t *testing.T, bare string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "scratch")
	testgit.Git(t, filepath.Dir(dir), "clone", bare, dir)
	return dir
}

// originBranch creates branch on origin from main.
func originBranch(t *testing.T, bare, branch string) {
	t.Helper()
	sc := scratch(t, bare)
	testgit.Git(t, sc, "push", "origin", "origin/main:refs/heads/"+branch)
}

// onFeature clones and checks out tracking branch feat.
func onFeature(t *testing.T) (*Syncer, Target, string) {
	s, tg, bare := setup(t)
	originBranch(t, bare, "feat")
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "checkout", "feat")
	return s, tg, bare
}

func rev(t *testing.T, dir, ref string) string { return testgit.Git(t, dir, "rev-parse", ref) }

func TestFeature_DefaultFastForwardedWithoutSwitch(t *testing.T) {
	s, tg, bare := onFeature(t)
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
	if rev(t, tg.Dir, "main") != rev(t, tg.Dir, "origin/main") {
		t.Fatal("main not fast-forwarded")
	}
	if got := testgit.Git(t, tg.Dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
		t.Fatalf("branch = %q", got)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "new.txt")); err == nil {
		t.Fatal("working tree changed")
	}
}

func TestFeature_DirtyStillFastForwardsDefault(t *testing.T) {
	s, tg, bare := onFeature(t)
	write(t, tg.Dir, "README.md", "dirty\n")
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "uncommitted changes, fetched only" {
		t.Fatalf("got %+v", r)
	}
	if rev(t, tg.Dir, "main") != rev(t, tg.Dir, "origin/main") {
		t.Fatal("main not fast-forwarded")
	}
	if b, _ := os.ReadFile(filepath.Join(tg.Dir, "README.md")); string(b) != "dirty\n" {
		t.Fatalf("working file changed: %q", b)
	}
}

func TestFeature_RebasedOntoUpstream(t *testing.T) {
	s, tg, bare := onFeature(t)
	testgit.Commit(t, bare, "feat", "feat.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "feat.txt")); err != nil {
		t.Fatalf("feat.txt missing: %v", err)
	}
}

func TestFeature_ConflictAborted(t *testing.T) {
	s, tg, bare := onFeature(t)
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local")
	before := rev(t, tg.Dir, "HEAD")
	testgit.Commit(t, bare, "feat", "README.md", "remote\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "rebase conflict, left unchanged" {
		t.Fatalf("got %+v", r)
	}
	if s.Git.OperationInProgress(tg.Dir) || rev(t, tg.Dir, "HEAD") != before {
		t.Fatal("repo not restored")
	}
}

func TestFeature_LocalOnlyBranchNoWarning(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "checkout", "-b", "local-only")
	r := s.Sync(context.Background(), tg)
	if r.Action != UpToDate || r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestFeature_DetachedHead(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "checkout", "--detach")
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	// local main is the only branch and is behind; it is fast-forwarded.
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
	if rev(t, tg.Dir, "main") != rev(t, tg.Dir, "origin/main") {
		t.Fatal("main not fast-forwarded")
	}
	if _, detached, _ := s.Git.CurrentBranch(context.Background(), tg.Dir); !detached {
		t.Fatal("HEAD no longer detached")
	}
}

func TestDefault_Diverged(t *testing.T) {
	s, tg, bare := onFeature(t)
	testgit.Git(t, tg.Dir, "checkout", "main")
	write(t, tg.Dir, "mine.txt", "x\n")
	testgit.Git(t, tg.Dir, "add", "mine.txt")
	testgit.Git(t, tg.Dir, "commit", "-m", "mine")
	testgit.Git(t, tg.Dir, "checkout", "feat")
	mainBefore := rev(t, tg.Dir, "main")
	testgit.Commit(t, bare, "main", "theirs.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || r.Warnings[0] != "local main has diverged from origin, not updated" {
		t.Fatalf("got %+v", r)
	}
	if rev(t, tg.Dir, "main") != mainBefore {
		t.Fatal("main changed")
	}
}

func TestGone_MergedDeleted(t *testing.T) {
	s, tg, bare := setup(t)
	originBranch(t, bare, "done")
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "branch", "--track", "done", "origin/done")
	testgit.Git(t, scratch(t, bare), "push", bare, "--delete", "done")
	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
	if out := testgit.Git(t, tg.Dir, "branch", "--list", "done"); out != "" {
		t.Fatalf("branch not deleted: %q", out)
	}
}

func TestGone_UnmergedKeptWithWarning(t *testing.T) {
	s, tg, bare := setup(t)
	// feat has a commit that origin later squash-merges into main.
	sc := scratch(t, bare)
	testgit.Git(t, sc, "checkout", "-b", "feat")
	write(t, sc, "feat.txt", "x\n")
	testgit.Git(t, sc, "add", "feat.txt")
	testgit.Git(t, sc, "commit", "-m", "feat work")
	testgit.Git(t, sc, "push", "origin", "feat")
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "branch", "--track", "feat", "origin/feat")

	testgit.Git(t, sc, "checkout", "main")
	testgit.Git(t, sc, "merge", "--squash", "feat")
	testgit.Git(t, sc, "commit", "-m", "squash feat")
	testgit.Git(t, sc, "push", "origin", "main")
	testgit.Git(t, sc, "push", "origin", "--delete", "feat")

	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || r.Warnings[0] != "branch feat: deleted on GitHub but not merged, kept" {
		t.Fatalf("got %+v", r)
	}
	if out := testgit.Git(t, tg.Dir, "branch", "--list", "feat"); out == "" {
		t.Fatal("branch was deleted")
	}
}

func TestGone_CurrentBranchWarned(t *testing.T) {
	s, tg, bare := onFeature(t)
	testgit.Git(t, scratch(t, bare), "push", bare, "--delete", "feat")
	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || r.Warnings[0] != "current branch feat was deleted on GitHub" {
		t.Fatalf("got %+v", r)
	}
	if got := testgit.Git(t, tg.Dir, "rev-parse", "--abbrev-ref", "HEAD"); got != "feat" {
		t.Fatalf("branch = %q", got)
	}
	if testgit.Git(t, tg.Dir, "branch", "--list", "feat") == "" {
		t.Fatal("current branch deleted")
	}
}

func TestDefaultRenamed(t *testing.T) {
	testgit.Env(t)
	bare := testgit.NewOrigin(t, "master")
	dest := filepath.Join(t.TempDir(), "dest", "repo")
	s, _, _ := setup(t)
	tg := Target{Name: "repo", Dir: dest, CloneURL: bare, DefaultBranch: "main"}
	cloneIt(t, bare, dest)

	sc := scratch(t, bare)
	testgit.Git(t, sc, "push", "origin", "origin/master:refs/heads/main")
	testgit.Git(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	testgit.Git(t, sc, "push", "origin", "--delete", "master")

	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || r.Warnings[0] != "current branch master was deleted on GitHub" {
		t.Fatalf("got %+v", r)
	}
	if rev(t, dest, "main") != rev(t, dest, "origin/main") {
		t.Fatal("local main missing or wrong")
	}
	if got := testgit.Git(t, dest, "rev-parse", "--abbrev-ref", "HEAD"); got != "master" {
		t.Fatalf("branch = %q", got)
	}
	if testgit.Git(t, dest, "branch", "--list", "master") == "" {
		t.Fatal("master deleted")
	}
	if !strings.Contains(testgit.Git(t, dest, "branch", "--list", "main"), "main") {
		t.Fatal("main not created")
	}
}

func TestFeature_RebaseInProgressFetchedOnly(t *testing.T) {
	s, tg, bare := onFeature(t)
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local")
	testgit.Commit(t, bare, "feat", "README.md", "remote\n")
	testgit.Git(t, tg.Dir, "fetch")
	if out, err := s.Git.Run(context.Background(), tg.Dir, "rebase", "origin/feat"); err == nil {
		t.Fatalf("expected conflict: %s", out)
	}
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "rebase/merge in progress, fetched only" {
		t.Fatalf("got %+v", r)
	}
	if !s.Git.OperationInProgress(tg.Dir) {
		t.Fatal("user's rebase was aborted")
	}
}

func TestFeature_RebaseInProgressDoesNotMoveDefault(t *testing.T) {
	s, tg, bare := onFeature(t)
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local")
	testgit.Commit(t, bare, "feat", "README.md", "remote\n")
	testgit.Git(t, tg.Dir, "fetch")
	if out, err := s.Git.Run(context.Background(), tg.Dir, "rebase", "origin/feat"); err == nil {
		t.Fatalf("expected conflict: %s", out)
	}
	mainBefore := rev(t, tg.Dir, "main")
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != FetchedOnly || len(r.Warnings) != 1 || r.Warnings[0] != "rebase/merge in progress, fetched only" {
		t.Fatalf("got %+v", r)
	}
	if rev(t, tg.Dir, "main") != mainBefore {
		t.Fatal("main moved during an in-progress operation")
	}
	if !s.Git.OperationInProgress(tg.Dir) {
		t.Fatal("user's rebase was aborted")
	}
}

func TestFeature_DefaultCheckedOutInLinkedWorktree(t *testing.T) {
	s, tg, bare := onFeature(t)
	wt := filepath.Join(t.TempDir(), "wt")
	testgit.Git(t, tg.Dir, "worktree", "add", wt, "main")
	mainBefore := rev(t, tg.Dir, "main")
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || r.Warnings[0] != "main is checked out in another worktree, not updated" {
		t.Fatalf("got %+v", r)
	}
	if r.Action != UpToDate {
		t.Fatalf("action = %s", r.Action)
	}
	if rev(t, tg.Dir, "main") != mainBefore {
		t.Fatal("main moved although checked out in a linked worktree")
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err == nil {
		t.Fatal("linked worktree changed")
	}
}

func TestGone_DeleteFailureShowsReason(t *testing.T) {
	s, tg, bare := setup(t)
	originBranch(t, bare, "done")
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "branch", "--track", "done", "origin/done")
	wt := filepath.Join(t.TempDir(), "wt")
	testgit.Git(t, tg.Dir, "worktree", "add", wt, "done")
	testgit.Git(t, scratch(t, bare), "push", bare, "--delete", "done")
	r := s.Sync(context.Background(), tg)
	if r.Err != nil || len(r.Warnings) != 1 || !strings.HasPrefix(r.Warnings[0], "branch done: could not delete: ") {
		t.Fatalf("got %+v", r)
	}
	if strings.Contains(r.Warnings[0], "\n") {
		t.Fatalf("warning should be one line: %q", r.Warnings[0])
	}
}
