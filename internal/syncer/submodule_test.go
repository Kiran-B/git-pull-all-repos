package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/gitops"
	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

// withSubmodule gives the origin of setup a submodule "sub" and returns the
// submodule's bare origin. The Syncer allows file:// submodules.
func withSubmodule(t *testing.T) (*Syncer, Target, string, string) {
	t.Helper()
	s, tg, bare := setup(t)
	s.Git = &gitops.Git{ExtraConfig: []string{"protocol.file.allow=always"}}
	subBare := testgit.NewOriginAt(t, "acme", "sub", "main")
	sc := scratch(t, bare)
	testgit.GitFile(t, sc, "submodule", "add", subBare, "sub")
	testgit.Git(t, sc, "commit", "-m", "add submodule")
	testgit.Git(t, sc, "push", "origin", "main")
	return s, tg, bare, subBare
}

func subEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func TestSubmodules_ClonedRecursively(t *testing.T) {
	s, tg, _, _ := withSubmodule(t)
	s.Submodules = true
	if r := s.Sync(context.Background(), tg); r.Action != Cloned || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "sub", "README.md")); err != nil {
		t.Fatalf("submodule not populated: %v", err)
	}
}

func TestSubmodules_UpdatedAfterPull(t *testing.T) {
	s, tg, bare, subBare := withSubmodule(t)
	s.Submodules = true
	ctx := context.Background()
	if r := s.Sync(ctx, tg); r.Action != Cloned || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	testgit.Commit(t, subBare, "main", "new.txt", "x\n")
	sc := scratch(t, bare)
	testgit.GitFile(t, sc, "submodule", "update", "--init")
	testgit.GitFile(t, sc, "submodule", "update", "--remote", "sub")
	testgit.Git(t, sc, "commit", "-am", "bump sub")
	testgit.Git(t, sc, "push", "origin", "main")

	r := s.Sync(ctx, tg)
	if r.Action != Updated || r.Err != nil || len(r.Warnings) != 0 {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "sub", "new.txt")); err != nil {
		t.Fatalf("submodule did not follow: %v", err)
	}
}

func TestSubmodules_Disabled(t *testing.T) {
	s, tg, _, _ := withSubmodule(t)
	if r := s.Sync(context.Background(), tg); r.Action != Cloned || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if got := subEntries(t, tg.Dir); len(got) != 0 {
		t.Fatalf("submodule dir not empty: %v", got)
	}
}

func TestSubmodules_UpdateFailureWarns(t *testing.T) {
	s, tg, bare, _ := withSubmodule(t)
	cloneIt(t, bare, tg.Dir)
	s.Submodules = true
	// Without file:// allowed, the submodule update fails but the pull stands.
	s.Git = &gitops.Git{}
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil || len(r.Warnings) != 1 ||
		!hasPrefix(r.Warnings[0], "submodule update failed: ") {
		t.Fatalf("got %+v", r)
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
