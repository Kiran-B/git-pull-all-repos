package github

import (
	"os"
	"testing"
)

func TestParseList(t *testing.T) {
	data, err := os.ReadFile("testdata/list.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}

	repos, err := ParseList(data)
	if err != nil {
		t.Fatalf("ParseList failed: %v", err)
	}

	if len(repos) != 8 {
		t.Fatalf("expected 8 repos, got %d", len(repos))
	}

	// Check mirror repo
	if repos[0].Name != "mirror-repo" {
		t.Errorf("expected name mirror-repo, got %s", repos[0].Name)
	}
	if repos[0].URL != "https://github.com/test/mirror-repo" {
		t.Errorf("expected URL https://github.com/test/mirror-repo, got %s", repos[0].URL)
	}
	if repos[0].SSHURL != "git@github.com:test/mirror-repo.git" {
		t.Errorf("expected SSHURL git@github.com:test/mirror-repo.git, got %s", repos[0].SSHURL)
	}
	if repos[0].DefaultBranch != "main" {
		t.Errorf("expected DefaultBranch main, got %s", repos[0].DefaultBranch)
	}
	if repos[0].Language != "Go" {
		t.Errorf("expected Language Go, got %s", repos[0].Language)
	}
	if repos[0].IsMirror != true {
		t.Errorf("expected IsMirror true, got %v", repos[0].IsMirror)
	}
	if repos[0].Stars != 10 {
		t.Errorf("expected Stars 10, got %d", repos[0].Stars)
	}

	// Check fork repo
	if repos[1].Name != "fork-repo" {
		t.Errorf("expected name fork-repo, got %s", repos[1].Name)
	}
	if repos[1].IsFork != true {
		t.Errorf("expected IsFork true, got %v", repos[1].IsFork)
	}

	// Check repo with null primaryLanguage
	if repos[2].Name != "no-language-repo" {
		t.Errorf("expected name no-language-repo, got %s", repos[2].Name)
	}
	if repos[2].Language != "" {
		t.Errorf("expected Language empty string for null, got %s", repos[2].Language)
	}

	// Check empty repo with null defaultBranchRef
	if repos[3].Name != "empty-repo" {
		t.Errorf("expected name empty-repo, got %s", repos[3].Name)
	}
	if repos[3].DefaultBranch != "" {
		t.Errorf("expected DefaultBranch empty string for null, got %s", repos[3].DefaultBranch)
	}
	if repos[3].IsEmpty != true {
		t.Errorf("expected IsEmpty true, got %v", repos[3].IsEmpty)
	}
}

func TestSelect(t *testing.T) {
	data, err := os.ReadFile("testdata/list.json")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}

	repos, err := ParseList(data)
	if err != nil {
		t.Fatalf("ParseList failed: %v", err)
	}

	// Test: mirror is dropped
	selected := Select(repos, "pushed", 0)
	if len(selected) != 7 {
		t.Fatalf("expected 7 repos after dropping mirror, got %d", len(selected))
	}
	for _, r := range selected {
		if r.IsMirror {
			t.Errorf("mirror repo should have been dropped")
		}
	}

	// Test: pushed sort (newest first)
	selected = Select(repos, "pushed", 0)
	expectedNames := []string{"fork-repo", "beta-equal-stars", "alpha-equal-stars", "no-language-repo", "aaa-equal-pushed", "zzz-equal-pushed", "empty-repo"}
	if len(selected) != len(expectedNames) {
		t.Fatalf("expected %d repos, got %d", len(expectedNames), len(selected))
	}
	for i, expectedName := range expectedNames {
		if selected[i].Name != expectedName {
			t.Errorf("pushed sort: at index %d expected %s, got %s", i, expectedName, selected[i].Name)
		}
	}

	// Test: pushed sort tie-break by name (aaa-equal-pushed and zzz-equal-pushed have same PushedAt)
	// They should be ordered as aaa-equal-pushed (name ascending) then zzz-equal-pushed
	aaaaIndex := -1
	zzzzIndex := -1
	for i, r := range selected {
		if r.Name == "aaa-equal-pushed" {
			aaaaIndex = i
		}
		if r.Name == "zzz-equal-pushed" {
			zzzzIndex = i
		}
	}
	if aaaaIndex > zzzzIndex {
		t.Errorf("pushed sort tie-break: aaa-equal-pushed should come before zzz-equal-pushed")
	}

	// Test: stars sort (highest first)
	selected = Select(repos, "stars", 0)
	if selected[0].Name != "fork-repo" || selected[0].Stars != 25 {
		t.Errorf("stars sort: expected fork-repo with 25 stars first, got %s with %d", selected[0].Name, selected[0].Stars)
	}
	if selected[1].Name != "aaa-equal-pushed" || selected[1].Stars != 15 {
		t.Errorf("stars sort: expected aaa-equal-pushed with 15 stars second, got %s with %d", selected[1].Name, selected[1].Stars)
	}

	// Test: stars sort tie-break by name (alpha-equal-stars, beta-equal-stars, no-language-repo all have 5 stars)
	alphaIndex := -1
	betaIndex := -1
	noLangIndex := -1
	for i, r := range selected {
		if r.Name == "alpha-equal-stars" {
			alphaIndex = i
		}
		if r.Name == "beta-equal-stars" {
			betaIndex = i
		}
		if r.Name == "no-language-repo" {
			noLangIndex = i
		}
	}
	if alphaIndex > betaIndex || betaIndex > noLangIndex {
		t.Errorf("stars sort tie-break: repos with 5 stars should be ordered alphabetically: alpha < beta < no-language-repo, but got indexes %d, %d, %d", alphaIndex, betaIndex, noLangIndex)
	}

	// Test: limit
	selected = Select(repos, "pushed", 2)
	if len(selected) != 2 {
		t.Fatalf("expected 2 repos with limit 2, got %d", len(selected))
	}
	if selected[0].Name != "fork-repo" || selected[1].Name != "beta-equal-stars" {
		t.Errorf("limit 2: expected fork-repo and beta-equal-stars, got %s and %s", selected[0].Name, selected[1].Name)
	}

	// Test: limit 0 means all
	selected = Select(repos, "pushed", 0)
	if len(selected) != 7 {
		t.Fatalf("expected 7 repos with limit 0 (all), got %d", len(selected))
	}

	// Test: caller's slice not mutated
	original := make([]Repo, len(repos))
	copy(original, repos)
	Select(repos, "pushed", 0)
	if repos[0].Name != original[0].Name {
		t.Errorf("caller's slice was mutated by Select")
	}
}

func TestListArgs(t *testing.T) {
	tests := []struct {
		name     string
		org      string
		filter   Filter
		expected []string
	}{
		{
			name:     "no filter",
			org:      "testorg",
			filter:   Filter{},
			expected: []string{"repo", "list", "testorg", "--limit", "100000", "--json", ListFields},
		},
		{
			name:     "no forks",
			org:      "testorg",
			filter:   Filter{NoForks: true},
			expected: []string{"repo", "list", "testorg", "--limit", "100000", "--json", ListFields, "--source"},
		},
		{
			name:     "no archived",
			org:      "testorg",
			filter:   Filter{NoArchived: true},
			expected: []string{"repo", "list", "testorg", "--limit", "100000", "--json", ListFields, "--no-archived"},
		},
		{
			name:     "language filter",
			org:      "testorg",
			filter:   Filter{Language: "Go"},
			expected: []string{"repo", "list", "testorg", "--limit", "100000", "--json", ListFields, "--language", "Go"},
		},
		{
			name:     "all filters",
			org:      "testorg",
			filter:   Filter{NoForks: true, NoArchived: true, Language: "Python"},
			expected: []string{"repo", "list", "testorg", "--limit", "100000", "--json", ListFields, "--source", "--no-archived", "--language", "Python"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := ListArgs(tt.org, tt.filter)
			if len(args) != len(tt.expected) {
				t.Fatalf("expected %d args, got %d: %v", len(tt.expected), len(args), args)
			}
			for i, arg := range args {
				if arg != tt.expected[i] {
					t.Errorf("arg %d: expected %s, got %s", i, tt.expected[i], arg)
				}
			}
		})
	}
}

func TestCloneURL(t *testing.T) {
	repo := Repo{
		URL:    "https://github.com/test/repo",
		SSHURL: "git@github.com:test/repo.git",
	}

	sshURL := repo.CloneURL("ssh")
	if sshURL != "git@github.com:test/repo.git" {
		t.Errorf("ssh protocol: expected git@github.com:test/repo.git, got %s", sshURL)
	}

	httpsURL := repo.CloneURL("https")
	if httpsURL != "https://github.com/test/repo" {
		t.Errorf("https protocol: expected https://github.com/test/repo, got %s", httpsURL)
	}

	defaultURL := repo.CloneURL("anything")
	if defaultURL != "https://github.com/test/repo" {
		t.Errorf("default protocol: expected https://github.com/test/repo, got %s", defaultURL)
	}
}
