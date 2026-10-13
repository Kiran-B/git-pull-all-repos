package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/spf13/pflag"
)

// Options holds the parsed CLI options
type Options struct {
	Host         string
	Org          string
	Dest         string
	NoForks      bool
	NoArchived   bool
	Language     string
	Sort         string
	Limit        int
	Jobs         int
	NoSubmodules bool
	DryRun       bool
	ShowVersion  bool
}

// ErrHelp is the error returned when -h or --help is used
var ErrHelp = pflag.ErrHelp

// ParseOrg extracts the host and org name from various input formats
// Input can be:
// - "acme" (defaults to github.com)
// - "https://github.com/orgs/acme"
// - "https://ghe.corp.example/orgs/platform"
// Returns an error for empty strings or incomplete URLs
func ParseOrg(arg string) (host, org string, err error) {
	if arg == "" {
		return "", "", fmt.Errorf("org cannot be empty")
	}

	// If it doesn't start with http:// or https://, treat it as just an org name
	lowerArg := strings.ToLower(arg)
	if !strings.HasPrefix(lowerArg, "http://") && !strings.HasPrefix(lowerArg, "https://") {
		return "github.com", arg, nil
	}

	// Parse as URL
	u, err := url.Parse(arg)
	if err != nil {
		return "", "", err
	}

	// Extract host (lowercase)
	host = strings.ToLower(u.Host)
	if host == "" {
		return "", "", fmt.Errorf("invalid URL: no host")
	}

	// Extract path and remove trailing slashes
	path := strings.TrimRight(u.Path, "/")
	if path == "" {
		return "", "", fmt.Errorf("invalid URL: no org path")
	}

	// Split path into segments
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	// If the first segment is "orgs", the org is the next one
	if len(segments) >= 1 && segments[0] == "orgs" {
		if len(segments) < 2 || segments[1] == "" {
			return "", "", fmt.Errorf("invalid URL: no org after /orgs/")
		}
		return host, segments[1], nil
	}

	// Otherwise, the first segment is the org (assuming just /org format)
	if segments[0] == "" {
		return "", "", fmt.Errorf("invalid URL: no org path")
	}

	return host, segments[0], nil
}

// flagValues holds pointers to all flag values
type flagValues struct {
	noForks      *bool
	noArchived   *bool
	language     *string
	sort         *string
	limit        *int
	jobs         *int
	noSubmodules *bool
	dryRun       *bool
	showVersion  *bool
}

// newFlagSet creates and configures a new FlagSet with all flags
func newFlagSet() (*pflag.FlagSet, *flagValues) {
	fs := pflag.NewFlagSet("git-pull-all-repos", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs, &flagValues{
		noForks:      fs.Bool("no-forks", false, "exclude forks"),
		noArchived:   fs.Bool("no-archived", false, "exclude archived repos"),
		language:     fs.String("language", "", "filter by language"),
		sort:         fs.String("sort", "pushed", "sort by 'pushed' or 'stars'"),
		limit:        fs.Int("limit", 0, "limit number of repos (0 = all)"),
		jobs:         fs.Int("jobs", 1, "number of parallel jobs"),
		noSubmodules: fs.Bool("no-submodules", false, "skip submodule updates"),
		dryRun:       fs.Bool("dry-run", false, "show what would be done without doing it"),
		showVersion:  fs.Bool("version", false, "show version"),
	}
}

// ResolveDest resolves the destination path
// If destArg is empty, returns the org name (relative path)
// If destArg is given, cleans it and expands ~ with home
func ResolveDest(org, destArg, home string) string {
	if destArg == "" {
		return org
	}

	// Expand ~ to home directory
	if destArg == "~" || destArg == "~/" {
		return home
	}
	if strings.HasPrefix(destArg, "~/") {
		return filepath.Join(home, destArg[2:])
	}

	// Clean the path (removes . and .., trailing and redundant separators)
	return filepath.Clean(destArg)
}

// Parse parses CLI arguments and returns Options
// It uses pflag with interspersed args (allows flags after positional args)
// Returns ErrHelp if -h or --help is used
func Parse(args []string, home string) (Options, error) {
	fs, flags := newFlagSet()

	// Parse flags, allowing interspersed positional args
	err := fs.Parse(args)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return Options{}, pflag.ErrHelp
		}
		return Options{}, err
	}

	// Get remaining positional arguments
	positionals := fs.Args()

	// Handle --version flag (org is optional when version is set)
	if *flags.showVersion {
		return Options{ShowVersion: true}, nil
	}

	// Validate: need at least one positional (org)
	if len(positionals) == 0 {
		return Options{}, fmt.Errorf("org argument is required")
	}

	// Validate: at most 2 positionals (org and optional dest)
	if len(positionals) > 2 {
		return Options{}, fmt.Errorf("too many positional arguments (expected org and optional destination)")
	}

	// Parse org
	orgArg := positionals[0]
	host, org, err := ParseOrg(orgArg)
	if err != nil {
		return Options{}, err
	}

	// Get destination (optional second positional)
	var dest string
	if len(positionals) > 1 {
		dest = ResolveDest(org, positionals[1], home)
	} else {
		dest = ResolveDest(org, "", home)
	}

	// Validate sort
	if *flags.sort != "pushed" && *flags.sort != "stars" {
		return Options{}, fmt.Errorf("--sort must be 'pushed' or 'stars', got %q", *flags.sort)
	}

	// Validate jobs
	if *flags.jobs < 1 {
		return Options{}, fmt.Errorf("--jobs must be at least 1, got %d", *flags.jobs)
	}

	// Validate limit
	if *flags.limit < 0 {
		return Options{}, fmt.Errorf("--limit must be >= 0, got %d", *flags.limit)
	}

	return Options{
		Host:         host,
		Org:          org,
		Dest:         dest,
		NoForks:      *flags.noForks,
		NoArchived:   *flags.noArchived,
		Language:     *flags.language,
		Sort:         *flags.sort,
		Limit:        *flags.limit,
		Jobs:         *flags.jobs,
		NoSubmodules: *flags.noSubmodules,
		DryRun:       *flags.dryRun,
		ShowVersion:  *flags.showVersion,
	}, nil
}

// Usage returns the usage string for the CLI
func Usage() string {
	fs, _ := newFlagSet()
	header := "Usage: git-pull-all-repos <org> [destination] [flags]\n"
	return header + fs.FlagUsages()
}
