// Package syncer implements the per-repository clone/update state machine.
package syncer

import "github.com/Kiran-B/git-pull-all-repos/internal/gitops"

// Action describes what happened to a repository.
type Action string

const (
	Cloned      Action = "cloned"
	Updated     Action = "updated"
	UpToDate    Action = "up to date"
	FetchedOnly Action = "fetched only"
	Skipped     Action = "skipped"
	Failed      Action = "failed"
	WouldClone  Action = "would clone"
	WouldUpdate Action = "would update"
)

// Target is a repository to sync. An empty DefaultBranch means "read from origin".
type Target struct {
	Name, Dir, CloneURL, DefaultBranch string
	IsEmpty                            bool
	Discovered                         bool // found on disk, not listed from GitHub: origin is checked by owner only
}

// Result is the outcome of syncing one repository.
type Result struct {
	Name     string
	Action   Action
	Warnings []string
	Err      error
}

// Syncer syncs repositories of Org using Git.
type Syncer struct {
	Git                *gitops.Git
	Submodules, DryRun bool
	Org                string
}
