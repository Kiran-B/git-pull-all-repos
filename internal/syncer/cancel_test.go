package syncer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

func TestSync_AlreadyCancelledSkips(t *testing.T) {
	s, tg, _ := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := s.Sync(ctx, tg)
	if r.Action != Skipped || len(r.Warnings) != 1 || r.Warnings[0] != "interrupted, not started" {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(tg.Dir); err == nil {
		t.Fatal("dir should not be created")
	}
}

// TestRebase_CancelledMidRebaseStillAborts cancels the context while git is
// still running a conflicting rebase. A wrapper git on PATH runs the real
// rebase (leaving it in progress), signals the test, and then hangs until it
// is interrupted. The cleanup abort must still run.
func TestRebase_CancelledMidRebaseStillAborts(t *testing.T) {
	s, tg, bare := onFeature(t)
	write(t, tg.Dir, "README.md", "local\n")
	testgit.Git(t, tg.Dir, "commit", "-am", "local")
	before := rev(t, tg.Dir, "HEAD")
	testgit.Commit(t, bare, "feat", "README.md", "remote\n")
	testgit.Git(t, tg.Dir, "fetch")

	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	flag := filepath.Join(bin, "flag")
	script := `#!/bin/sh
for a in "$@"; do
  if [ "$a" = "--abort" ]; then exec ` + real + ` "$@"; fi
done
case " $* " in
  *" rebase "*)
    ` + real + ` "$@"
    touch ` + flag + `
    exec sleep 30
    ;;
esac
exec ` + real + ` "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			if _, err := os.Stat(flag); err == nil {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	_, w, err := s.rebaseOnto(ctx, tg.Dir, "@{u}")
	if err != nil {
		t.Fatalf("rebaseOnto error: %v", err)
	}
	if w == "" {
		t.Fatal("expected a warning")
	}
	if s.Git.OperationInProgress(tg.Dir) {
		t.Fatal("repo left mid-rebase")
	}
	if rev(t, tg.Dir, "HEAD") != before {
		t.Fatal("HEAD moved")
	}
}
