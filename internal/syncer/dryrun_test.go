package syncer

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

func snapshot(t *testing.T, dir string) [2]string {
	t.Helper()
	return [2]string{
		testgit.Git(t, dir, "for-each-ref"),
		testgit.Git(t, dir, "status", "--porcelain"),
	}
}

func TestDryRun_NoChanges(t *testing.T) {
	s, tg, bare := setup(t)
	s.DryRun = true
	ctx := context.Background()

	if r := s.Sync(ctx, tg); r.Action != WouldClone || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(tg.Dir); err == nil {
		t.Fatal("dry-run created the directory")
	}

	cloneIt(t, bare, tg.Dir)
	originBranch(t, bare, "feat")
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	testgit.Git(t, tg.Dir, "checkout", "-b", "local-only")
	before := snapshot(t, tg.Dir)
	if r := s.Sync(ctx, tg); r.Action != WouldUpdate || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if after := snapshot(t, tg.Dir); !reflect.DeepEqual(before, after) {
		t.Fatalf("repo changed:\nbefore %q\nafter  %q", before, after)
	}
}

func TestDryRun_Actions(t *testing.T) {
	s, tg, bare := setup(t)
	s.DryRun = true
	ctx := context.Background()
	cloneIt(t, bare, tg.Dir)

	r := s.Sync(ctx, tg)
	if r.Action != WouldUpdate || len(r.Warnings) != 0 {
		t.Fatalf("clean: %+v", r)
	}

	write(t, tg.Dir, "README.md", "dirty\n")
	r = s.Sync(ctx, tg)
	if r.Action != WouldUpdate || !reflect.DeepEqual(r.Warnings, []string{"uncommitted changes, would fetch only"}) {
		t.Fatalf("dirty: %+v", r)
	}
	testgit.Git(t, tg.Dir, "checkout", "--", "README.md")

	testgit.Git(t, tg.Dir, "checkout", "-b", "feat")
	r = s.Sync(ctx, tg)
	if r.Action != WouldUpdate || !reflect.DeepEqual(r.Warnings, []string{"on branch feat; default would be fast-forwarded"}) {
		t.Fatalf("feature: %+v", r)
	}

	// Conflicting rebase left in progress.
	testgit.Git(t, tg.Dir, "checkout", "main")
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local")
	testgit.Commit(t, bare, "main", "README.md", "remote\n")
	testgit.Git(t, tg.Dir, "fetch")
	if out, err := s.Git.Run(ctx, tg.Dir, "rebase", "origin/main"); err == nil {
		t.Fatalf("expected conflict: %s", out)
	}
	r = s.Sync(ctx, tg)
	if r.Action != WouldUpdate || len(r.Warnings) != 1 || r.Warnings[0] != "rebase/merge in progress, fetched only" {
		t.Fatalf("in progress: %+v", r)
	}
}

func TestDryRun_SkipsLikeReal(t *testing.T) {
	s, tg, _ := setup(t)
	s.DryRun = true
	if err := os.MkdirAll(tg.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.Sync(context.Background(), tg)
	if r.Action != Skipped || len(r.Warnings) != 1 || r.Warnings[0] != "folder exists but is not a git repository" {
		t.Fatalf("got %+v", r)
	}
}
