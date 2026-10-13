package syncer

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kiran-B/git-pull-all-repos/internal/gitops"
)

// NormalizeRemote parses an https, ssh:// or scp-style git remote into its
// lowercased host and owner and its name without ".git". Local paths and
// file:// URLs give ok=false.
func NormalizeRemote(raw string) (host, owner, name string, ok bool) {
	var path string
	switch {
	case strings.HasPrefix(raw, "file://"):
		return "", "", "", false
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			return "", "", "", false
		}
		host, path = u.Hostname(), u.Path
	default:
		// scp-like: [user@]host:owner/name.git (not a filesystem path)
		if strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, ".") {
			return "", "", "", false
		}
		left, right, found := strings.Cut(raw, ":")
		if !found {
			return "", "", "", false
		}
		if _, h, hasUser := strings.Cut(left, "@"); hasUser {
			left = h
		}
		host, path = left, right
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if host == "" || len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	name = strings.TrimSuffix(parts[1], ".git")
	if name == "" {
		return "", "", "", false
	}
	return strings.ToLower(host), strings.ToLower(parts[0]), name, true
}

// localOwnerName returns the last two path segments of a file:// URL or
// plain filesystem path, with ".git" stripped from the name.
func localOwnerName(raw string) (owner, name string, ok bool) {
	p := strings.TrimPrefix(raw, "file://")
	p = strings.TrimRight(filepath.ToSlash(p), "/")
	parts := strings.Split(p, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	owner, name = parts[len(parts)-2], strings.TrimSuffix(parts[len(parts)-1], ".git")
	return owner, name, owner != "" && name != ""
}

// isLocalRemote reports whether raw is a file:// URL or a filesystem path
// rather than a remote URL NormalizeRemote merely failed to parse.
func isLocalRemote(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "file://"):
		return true
	case strings.Contains(raw, "://"):
		return false
	case strings.HasPrefix(raw, "/"), strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, "../"):
		return true
	}
	// No scheme: a path unless it looks like scp-style host:path.
	return !strings.Contains(raw, ":")
}

// OriginMatches reports whether url points at host/org/name, compared
// case-insensitively. Local paths and file:// URLs ignore host and match on
// their last two path segments, which lets tests use local origins laid out
// as <tmp>/<org>/<name>.git.
func OriginMatches(url, host, org, name string) bool {
	if h, o, n, ok := NormalizeRemote(url); ok {
		return strings.EqualFold(h, host) && strings.EqualFold(o, org) && strings.EqualFold(n, name)
	}
	if !isLocalRemote(url) {
		return false
	}
	o, n, ok := localOwnerName(url)
	return ok && strings.EqualFold(o, org) && strings.EqualFold(n, name)
}

// originOwnerMatches is like OriginMatches but ignores the repository name.
func originOwnerMatches(url, host, org string) bool {
	if h, o, _, ok := NormalizeRemote(url); ok {
		return strings.EqualFold(h, host) && strings.EqualFold(o, org)
	}
	if !isLocalRemote(url) {
		return false
	}
	o, _, ok := localOwnerName(url)
	return ok && strings.EqualFold(o, org)
}

// Discover finds existing clones in dest that belong to org but were not
// listed (listed holds lowercased directory names). Only immediate,
// non-hidden subdirectories are considered; everything else is ignored
// silently. A missing dest yields nil.
func Discover(ctx context.Context, g *gitops.Git, dest, host, org string, listed map[string]bool) ([]Target, error) {
	entries, err := os.ReadDir(dest)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Target
	for _, e := range entries {
		n := e.Name()
		if !e.IsDir() || strings.HasPrefix(n, ".") || listed[strings.ToLower(n)] {
			continue
		}
		dir := filepath.Join(dest, n)
		if !g.IsRepo(dir) {
			continue
		}
		u, err := g.OriginURL(ctx, dir)
		if err != nil || !originOwnerMatches(u, host, org) {
			continue
		}
		out = append(out, Target{Name: n, Dir: dir, Discovered: true})
	}
	return out, nil
}
