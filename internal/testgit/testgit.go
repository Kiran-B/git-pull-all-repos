// Package testgit provides helpers for building throwaway git repositories in tests.
package testgit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Env isolates the test from the user's git configuration. It must not be
// used together with t.Parallel because it uses t.Setenv.
func Env(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	cfg := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(cfg, nil, 0o644); err != nil {
		t.Fatalf("write empty gitconfig: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

// Git runs plain git in dir and fails the test on error. It returns trimmed stdout.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// NewOrigin creates a bare repo at <tmp>/acme/repo.git with one commit on branch.
func NewOrigin(t *testing.T, branch string) string {
	t.Helper()
	return NewOriginAt(t, "acme", "repo", branch)
}

// NewOriginAt creates a bare repo at <tmp>/<org>/<name>.git with one commit on
// branch and HEAD pointing to it.
func NewOriginAt(t *testing.T, org, name, branch string) string {
	t.Helper()
	root := t.TempDir()
	bare := filepath.Join(root, org, name+".git")
	if err := os.MkdirAll(bare, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	Git(t, bare, "init", "--bare", "-b", branch)
	scratch := filepath.Join(t.TempDir(), "scratch")
	Git(t, root, "init", "-b", branch, scratch)
	if err := os.WriteFile(filepath.Join(scratch, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	Git(t, scratch, "add", "-A")
	Git(t, scratch, "commit", "-m", "initial")
	Git(t, scratch, "push", bare, branch)
	return bare
}

// Commit pushes a new commit (file=content) to branch of bareDir via a scratch clone.
func Commit(t *testing.T, bareDir, branch, file, content string) {
	t.Helper()
	scratch := filepath.Join(t.TempDir(), "scratch")
	Git(t, filepath.Dir(scratch), "clone", "-b", branch, bareDir, scratch)
	path := filepath.Join(scratch, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	Git(t, scratch, "add", "-A")
	Git(t, scratch, "commit", "-m", "update "+file)
	Git(t, scratch, "push", "origin", branch)
}

// Clone clones bareDir into dest.
func Clone(t *testing.T, bareDir, dest string) {
	t.Helper()
	Git(t, filepath.Dir(dest), "clone", bareDir, dest)
}

// GitFile is like Git but allows the file:// protocol, which git blocks by
// default for submodules.
func GitFile(t testing.TB, dir string, args ...string) string {
	t.Helper()
	return Git(t, dir, append([]string{"-c", "protocol.file.allow=always"}, args...)...)
}
