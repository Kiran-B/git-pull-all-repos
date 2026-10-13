package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Repo represents a GitHub repository
type Repo struct {
	Name          string
	URL           string
	SSHURL        string
	DefaultBranch string
	Language      string
	IsArchived    bool
	IsFork        bool
	IsMirror      bool
	IsEmpty       bool
	PushedAt      time.Time
	Stars         int
}

// Filter represents filters for listing repositories
type Filter struct {
	NoForks    bool
	NoArchived bool
	Language   string
}

// ListFields defines the JSON fields returned by `gh repo list --json`
const ListFields = "name,url,sshUrl,defaultBranchRef,primaryLanguage,isArchived,isFork,isMirror,isEmpty,pushedAt,stargazerCount"

// ListArgs builds the arguments for `gh repo list` command
func ListArgs(org string, f Filter) []string {
	args := []string{"repo", "list", org, "--limit", "100000", "--json", ListFields}

	if f.NoForks {
		args = append(args, "--source")
	}
	if f.NoArchived {
		args = append(args, "--no-archived")
	}
	if f.Language != "" {
		args = append(args, "--language", f.Language)
	}

	return args
}

// repoJSON represents the intermediate JSON structure from gh repo list
type repoJSON struct {
	Name             string                 `json:"name"`
	URL              string                 `json:"url"`
	SSHUrl           string                 `json:"sshUrl"`
	DefaultBranchRef *struct{ Name string } `json:"defaultBranchRef"`
	PrimaryLanguage  *struct{ Name string } `json:"primaryLanguage"`
	IsArchived       bool                   `json:"isArchived"`
	IsFork           bool                   `json:"isFork"`
	IsMirror         bool                   `json:"isMirror"`
	IsEmpty          bool                   `json:"isEmpty"`
	PushedAt         string                 `json:"pushedAt"`
	StargazerCount   int                    `json:"stargazerCount"`
}

// ParseList parses the JSON output from `gh repo list`
func ParseList(data []byte) ([]Repo, error) {
	var repoList []repoJSON
	if err := json.Unmarshal(data, &repoList); err != nil {
		return nil, fmt.Errorf("failed to parse repository list: %w", err)
	}

	repos := make([]Repo, len(repoList))
	for i, rj := range repoList {
		repos[i] = Repo{
			Name:       rj.Name,
			URL:        rj.URL,
			SSHURL:     rj.SSHUrl,
			IsArchived: rj.IsArchived,
			IsFork:     rj.IsFork,
			IsMirror:   rj.IsMirror,
			IsEmpty:    rj.IsEmpty,
			Stars:      rj.StargazerCount,
		}

		// Handle null defaultBranchRef
		if rj.DefaultBranchRef != nil {
			repos[i].DefaultBranch = rj.DefaultBranchRef.Name
		}

		// Handle null primaryLanguage
		if rj.PrimaryLanguage != nil {
			repos[i].Language = rj.PrimaryLanguage.Name
		}

		// Parse pushedAt as RFC3339
		if rj.PushedAt != "" {
			pt, err := time.Parse(time.RFC3339, rj.PushedAt)
			if err != nil {
				return nil, fmt.Errorf("failed to parse pushedAt for %s: %w", rj.Name, err)
			}
			repos[i].PushedAt = pt
		}
	}

	return repos, nil
}

// Select filters mirrors, sorts, and limits the repository list
func Select(repos []Repo, sortBy string, limit int) []Repo {
	// Copy the slice to avoid mutating the caller's slice
	selected := make([]Repo, len(repos))
	copy(selected, repos)

	// Filter out mirrors
	filtered := make([]Repo, 0, len(selected))
	for _, r := range selected {
		if !r.IsMirror {
			filtered = append(filtered, r)
		}
	}

	// Sort
	sort.Slice(filtered, func(i, j int) bool {
		switch sortBy {
		case "pushed":
			// Sort by PushedAt descending (newest first)
			if !filtered[i].PushedAt.Equal(filtered[j].PushedAt) {
				return filtered[i].PushedAt.After(filtered[j].PushedAt)
			}
		case "stars":
			// Sort by Stars descending (highest first)
			if filtered[i].Stars != filtered[j].Stars {
				return filtered[i].Stars > filtered[j].Stars
			}
		}
		// Tiebreaker: sort by Name ascending
		return filtered[i].Name < filtered[j].Name
	})

	// Apply limit
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return filtered
}

// CloneURL returns the clone URL for the repository based on the protocol
func (r Repo) CloneURL(protocol string) string {
	if protocol == "ssh" {
		return r.SSHURL
	}
	return r.URL
}

// Lister executes gh commands to list and get git protocol information
type Lister struct {
	Host string
}

// run executes a command and returns its output
func (l Lister) run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)

	// Set GH_HOST environment variable if host is not empty and not github.com
	if l.Host != "" && l.Host != "github.com" {
		cmd.Env = append(cmd.Environ(), fmt.Sprintf("GH_HOST=%s", l.Host))
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("command failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	return stdout.Bytes(), nil
}

// List retrieves the list of repositories for an organization
func (l Lister) List(ctx context.Context, org string, f Filter) ([]Repo, error) {
	args := ListArgs(org, f)
	data, err := l.run(ctx, args...)
	if err != nil {
		return nil, err
	}

	return ParseList(data)
}

// GitProtocol retrieves the git protocol configured for the host
func (l Lister) GitProtocol(ctx context.Context) (string, error) {
	args := []string{"config", "get", "git_protocol", "-h", l.Host}
	output, err := l.run(ctx, args...)

	// If the command fails or output is empty, return https
	if err != nil {
		return "https", nil
	}

	protocol := strings.TrimSpace(string(output))
	if protocol == "" {
		return "https", nil
	}

	// Any value other than "ssh" is treated as "https"
	if protocol != "ssh" {
		return "https", nil
	}

	return "ssh", nil
}
