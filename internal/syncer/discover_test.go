package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/gitops"
	"github.com/Kiran-B/git-pull-all-repos/internal/testgit"
)

func TestNormalizeRemote(t *testing.T) {
	tests := []struct {
		url, host, owner, name string
		ok                     bool
	}{
		{"https://github.com/acme/repo.git", "github.com", "acme", "repo", true},
		{"https://github.com/acme/repo", "github.com", "acme", "repo", true},
		{"ssh://git@github.com/acme/repo.git", "github.com", "acme", "repo", true},
		{"git@github.com:acme/repo.git", "github.com", "acme", "repo", true},
		{"https://GitHub.com/ACME/Repo.git", "github.com", "acme", "Repo", true},
		{"git@ghe.example.com:Acme/tool.git", "ghe.example.com", "acme", "tool", true},
		{"https://ghe.example.com/acme/tool.git", "ghe.example.com", "acme", "tool", true},
		{"/tmp/x/acme/repo.git", "", "", "", false},
		{"file:///tmp/x/acme/repo.git", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, tc := range tests {
		h, o, n, ok := NormalizeRemote(tc.url)
		if h != tc.host || o != tc.owner || n != tc.name || ok != tc.ok {
			t.Errorf("NormalizeRemote(%q) = %q,%q,%q,%v", tc.url, h, o, n, ok)
		}
	}
}

func TestOriginMatches(t *testing.T) {
	tests := []struct {
		url, host, org, name string
		want                 bool
	}{
		{"https://github.com/acme/repo.git", "github.com", "acme", "repo", true},
		{"git@github.com:ACME/Repo.git", "github.com", "acme", "repo", true},
		{"https://github.com/other/repo.git", "github.com", "acme", "repo", false},
		{"https://github.com/acme/other.git", "github.com", "acme", "repo", false},
		{"https://gitlab.com/acme/repo.git", "github.com", "acme", "repo", false},
		{"/tmp/x/acme/repo.git", "github.com", "acme", "repo", true},
		{"file:///tmp/x/acme/repo.git", "github.com", "acme", "repo", true},
		{"/tmp/x/acme/repo", "github.com", "acme", "repo", true},
		{"/tmp/x/Acme/Repo.git", "github.com", "acme", "repo", true},
		{"/tmp/x/other/repo.git", "github.com", "acme", "repo", false},
		{"/tmp/x/acme/other.git", "github.com", "acme", "repo", false},
		{"", "github.com", "acme", "repo", false},
		{"https://gitlab.com/group/acme/repo.git", "github.com", "acme", "repo", false},
		{"git@gitlab.com:group/acme/repo.git", "github.com", "acme", "repo", false},
		{"ssh://git@gitlab.com/group/acme/repo.git", "github.com", "acme", "repo", false},
		{"ssh://git@gitlab.com/acme/repo.git", "github.com", "acme", "repo", false},
		{"git@gitlab.com:acme/repo.git", "github.com", "acme", "repo", false},
		{"../x/acme/repo.git", "github.com", "acme", "repo", true},
		{"x/acme/repo.git", "github.com", "acme", "repo", true},
	}
	for _, tc := range tests {
		if got := OriginMatches(tc.url, tc.host, tc.org, tc.name); got != tc.want {
			t.Errorf("OriginMatches(%q,%q,%q,%q) = %v, want %v", tc.url, tc.host, tc.org, tc.name, got, tc.want)
		}
	}
}

func TestOriginOwnerMatches(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://github.com/acme/repo.git", true},
		{"git@github.com:acme/other.git", true},
		{"https://gitlab.com/acme/repo.git", false},
		{"git@gitlab.com:acme/repo.git", false},
		{"https://gitlab.com/group/acme/repo.git", false},
		{"git@gitlab.com:group/acme/repo.git", false},
		{"ssh://git@gitlab.com/group/acme/repo.git", false},
		{"/tmp/x/acme/repo.git", true},
		{"file:///tmp/x/acme/repo.git", true},
		{"/tmp/x/other/repo.git", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := originOwnerMatches(tc.url, "github.com", "acme"); got != tc.want {
			t.Errorf("originOwnerMatches(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestDiscover(t *testing.T) {
	testgit.Env(t)
	acmeA := testgit.NewOriginAt(t, "acme", "listed", "main")
	acmeB := testgit.NewOriginAt(t, "acme", "extra", "main")
	other := testgit.NewOriginAt(t, "other", "foreign", "main")
	dest := t.TempDir()
	testgit.Clone(t, acmeA, filepath.Join(dest, "listed"))
	testgit.Clone(t, acmeB, filepath.Join(dest, "extra"))
	testgit.Clone(t, other, filepath.Join(dest, "foreign"))
	testgit.Clone(t, acmeB, filepath.Join(dest, ".hidden"))
	if err := os.WriteFile(filepath.Join(dest, ".DS_Store"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dest, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(context.Background(), &gitops.Git{}, dest, "github.com", "acme", map[string]bool{"listed": true})
	if err != nil {
		t.Fatal(err)
	}
	want := Target{Name: "extra", Dir: filepath.Join(dest, "extra"), Discovered: true}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
}

func TestDiscover_MissingDest(t *testing.T) {
	got, err := Discover(context.Background(), &gitops.Git{}, filepath.Join(t.TempDir(), "nope"), "github.com", "acme", nil)
	if err != nil || got != nil {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestSync_OriginMismatchSkipped(t *testing.T) {
	s, tg, _ := setup(t)
	other := testgit.NewOriginAt(t, "other", "repo", "main")
	cloneIt(t, other, tg.Dir)
	r := s.Sync(context.Background(), tg)
	if r.Action != Skipped || len(r.Warnings) != 1 {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_NoOriginSkipped(t *testing.T) {
	s, tg, _ := setup(t)
	if err := os.MkdirAll(tg.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	testgit.Git(t, tg.Dir, "init", "-b", "main")
	r := s.Sync(context.Background(), tg)
	if r.Action != Skipped || len(r.Warnings) != 1 || r.Warnings[0] != "no origin remote" {
		t.Fatalf("got %+v", r)
	}
}

func TestSync_DiscoveredUsesOriginHead(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Commit(t, bare, "main", "new.txt", "x\n")
	tg.DefaultBranch = ""
	tg.CloneURL = ""
	tg.Name = "renamed-dir" // dir name differs from the origin repo name
	tg.Discovered = true
	r := s.Sync(context.Background(), tg)
	if r.Action != Updated || r.Err != nil {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(filepath.Join(tg.Dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestSync_DryRunUnknownDefaultBranch(t *testing.T) {
	s, tg, bare := setup(t)
	cloneIt(t, bare, tg.Dir)
	testgit.Git(t, tg.Dir, "remote", "set-head", "origin", "-d")
	tg.DefaultBranch = ""
	s.DryRun = true
	r := s.Sync(context.Background(), tg)
	if r.Action != WouldUpdate || len(r.Warnings) != 1 || r.Warnings[0] != "default branch unknown (origin/HEAD not set)" {
		t.Fatalf("got %+v", r)
	}
}
