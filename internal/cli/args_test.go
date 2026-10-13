package cli

import (
	"errors"
	"testing"
)

func TestParseOrg(t *testing.T) {
	cases := map[string][2]string{
		"acme":                          {"github.com", "acme"},
		"httpie":                        {"github.com", "httpie"},
		"http-party":                    {"github.com", "http-party"},
		"https://github.com/orgs/acme":  {"github.com", "acme"},
		"https://github.com/orgs/acme/": {"github.com", "acme"},
		"https://github.com/orgs/acme/repositories": {"github.com", "acme"},
		"https://github.com/acme":                   {"github.com", "acme"},
		"http://GitHub.com/Acme/":                   {"github.com", "Acme"},
		"https://ghe.corp.example/orgs/platform":    {"ghe.corp.example", "platform"},
	}

	for input, expected := range cases {
		host, org, err := ParseOrg(input)
		if err != nil {
			t.Errorf("ParseOrg(%q): unexpected error: %v", input, err)
			continue
		}
		if host != expected[0] || org != expected[1] {
			t.Errorf("ParseOrg(%q): got (%q, %q), want (%q, %q)", input, host, org, expected[0], expected[1])
		}
	}

	// Test error cases
	errCases := []string{"", "https://github.com/", "https://github.com/orgs/"}
	for _, input := range errCases {
		_, _, err := ParseOrg(input)
		if err == nil {
			t.Errorf("ParseOrg(%q): expected error, got nil", input)
		}
	}
}

func TestResolveDest(t *testing.T) {
	cases := []struct {
		org      string
		destArg  string
		home     string
		expected string
	}{
		{"acme", "", "/h", "acme"},
		{"acme", "/x/my-folder/", "/h", "/x/my-folder"},
		{"acme", "~/my-folder", "/h", "/h/my-folder"},
		{"acme", "~", "/h", "/h"},
		{"acme", "/", "/h", "/"},
		{"acme", "//", "/h", "/"},
	}

	for _, tc := range cases {
		result := ResolveDest(tc.org, tc.destArg, tc.home)
		if result != tc.expected {
			t.Errorf("ResolveDest(%q, %q, %q): got %q, want %q", tc.org, tc.destArg, tc.home, result, tc.expected)
		}
	}
}

func TestParse(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		home     string
		expected Options
		wantErr  bool
	}{
		{
			name: "basic org and destination",
			args: []string{"acme", "--limit", "5", "/tmp/d", "--sort", "stars"},
			home: "/h",
			expected: Options{
				Host:  "github.com",
				Org:   "acme",
				Dest:  "/tmp/d",
				Limit: 5,
				Sort:  "stars",
				Jobs:  1,
			},
		},
		{
			name: "org only with defaults",
			args: []string{"acme"},
			home: "/h",
			expected: Options{
				Host: "github.com",
				Org:  "acme",
				Dest: "acme",
				Sort: "pushed",
				Jobs: 1,
			},
		},
		{
			name: "version flag",
			args: []string{"--version"},
			home: "/h",
			expected: Options{
				ShowVersion: true,
			},
		},
		{
			name: "all flags set",
			args: []string{"myorg", "/dest", "--no-forks", "--no-archived", "--language", "go", "--sort", "stars", "--limit", "10", "--jobs", "4", "--no-submodules", "--dry-run"},
			home: "/h",
			expected: Options{
				Host:         "github.com",
				Org:          "myorg",
				Dest:         "/dest",
				NoForks:      true,
				NoArchived:   true,
				Language:     "go",
				Sort:         "stars",
				Limit:        10,
				Jobs:         4,
				NoSubmodules: true,
				DryRun:       true,
				ShowVersion:  false,
			},
		},
		{
			name:    "no args",
			args:    []string{},
			home:    "/h",
			wantErr: true,
		},
		{
			name:    "too many positional args",
			args:    []string{"acme", "/tmp/d", "extra"},
			home:    "/h",
			wantErr: true,
		},
		{
			name:    "invalid sort value",
			args:    []string{"acme", "--sort", "name"},
			home:    "/h",
			wantErr: true,
		},
		{
			name:    "invalid jobs value",
			args:    []string{"acme", "--jobs", "0"},
			home:    "/h",
			wantErr: true,
		},
		{
			name:    "invalid limit value",
			args:    []string{"acme", "--limit", "-1"},
			home:    "/h",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := Parse(tc.args, tc.home)
			if tc.wantErr {
				if err == nil {
					t.Errorf("Parse(%v, %q): expected error, got nil", tc.args, tc.home)
				}
			} else {
				if err != nil {
					t.Errorf("Parse(%v, %q): unexpected error: %v", tc.args, tc.home, err)
					return
				}
				if opts.Host != tc.expected.Host {
					t.Errorf("Host: got %q, want %q", opts.Host, tc.expected.Host)
				}
				if opts.Org != tc.expected.Org {
					t.Errorf("Org: got %q, want %q", opts.Org, tc.expected.Org)
				}
				if opts.Dest != tc.expected.Dest {
					t.Errorf("Dest: got %q, want %q", opts.Dest, tc.expected.Dest)
				}
				if opts.NoForks != tc.expected.NoForks {
					t.Errorf("NoForks: got %v, want %v", opts.NoForks, tc.expected.NoForks)
				}
				if opts.NoArchived != tc.expected.NoArchived {
					t.Errorf("NoArchived: got %v, want %v", opts.NoArchived, tc.expected.NoArchived)
				}
				if opts.Language != tc.expected.Language {
					t.Errorf("Language: got %q, want %q", opts.Language, tc.expected.Language)
				}
				if opts.Limit != tc.expected.Limit {
					t.Errorf("Limit: got %d, want %d", opts.Limit, tc.expected.Limit)
				}
				if opts.Sort != tc.expected.Sort {
					t.Errorf("Sort: got %q, want %q", opts.Sort, tc.expected.Sort)
				}
				if opts.Jobs != tc.expected.Jobs {
					t.Errorf("Jobs: got %d, want %d", opts.Jobs, tc.expected.Jobs)
				}
				if opts.NoSubmodules != tc.expected.NoSubmodules {
					t.Errorf("NoSubmodules: got %v, want %v", opts.NoSubmodules, tc.expected.NoSubmodules)
				}
				if opts.DryRun != tc.expected.DryRun {
					t.Errorf("DryRun: got %v, want %v", opts.DryRun, tc.expected.DryRun)
				}
				if opts.ShowVersion != tc.expected.ShowVersion {
					t.Errorf("ShowVersion: got %v, want %v", opts.ShowVersion, tc.expected.ShowVersion)
				}
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	helpCases := [][]string{
		{"-h"},
		{"--help"},
	}

	for _, args := range helpCases {
		_, err := Parse(args, "/h")
		if err == nil {
			t.Errorf("Parse(%v): expected ErrHelp, got nil", args)
		}
		if !errors.Is(err, ErrHelp) {
			t.Errorf("Parse(%v): expected ErrHelp, got %v", args, err)
		}
	}
}

func TestUsage(t *testing.T) {
	usage := Usage()
	if usage == "" {
		t.Error("Usage(): returned empty string")
	}
	if !contains(usage, "git-pull-all-repos") {
		t.Errorf("Usage() should contain 'git-pull-all-repos', got: %s", usage)
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
