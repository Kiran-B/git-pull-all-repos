package report

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Kiran-B/git-pull-all-repos/internal/syncer"
)

// Test each symbol
func TestLineSymbols(t *testing.T) {
	tests := []struct {
		name     string
		action   syncer.Action
		err      error
		warnings []string
		wantSym  string
	}{
		{"Failed gets cross", syncer.Failed, errors.New("something broke"), nil, "✗"},
		{"Skipped gets warning", syncer.Skipped, nil, nil, "⚠"},
		{"Warnings get warning", syncer.Updated, nil, []string{"warning1"}, "⚠"},
		{"Cloned gets plus", syncer.Cloned, nil, nil, "+"},
		{"WouldClone gets plus", syncer.WouldClone, nil, nil, "+"},
		{"UpToDate gets check", syncer.UpToDate, nil, nil, "✓"},
		{"Updated gets check", syncer.Updated, nil, nil, "✓"},
		{"FetchedOnly gets check", syncer.FetchedOnly, nil, nil, "✓"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			r := &Reporter{Out: &buf, Color: false}
			res := syncer.Result{Name: "repo", Action: tt.action, Warnings: tt.warnings, Err: tt.err}
			r.Line(res)
			output := buf.String()
			if !strings.HasPrefix(output, tt.wantSym) {
				t.Errorf("expected symbol %s, got output: %s", tt.wantSym, output)
			}
		})
	}
}

// Test multi-warning indentation
func TestLineMultipleWarnings(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	res := syncer.Result{
		Name:     "repo",
		Action:   syncer.Updated,
		Warnings: []string{"first warning", "second warning", "third warning"},
	}
	r.Line(res)
	output := buf.String()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")

	if len(lines) != 3 {
		t.Errorf("expected 3 lines (main line + 2 remaining warnings), got %d: %v", len(lines), lines)
	}
	if !strings.HasPrefix(lines[1], "    second warning") {
		t.Errorf("second warning not indented correctly: %s", lines[1])
	}
	if !strings.HasPrefix(lines[2], "    third warning") {
		t.Errorf("third warning not indented correctly: %s", lines[2])
	}
}

// Test Failed with error and warnings
func TestLineFailedWithWarnings(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	res := syncer.Result{
		Name:     "repo",
		Action:   syncer.Failed,
		Err:      errors.New("clone failed\nmore detail"),
		Warnings: []string{"warning1", "warning2"},
	}
	r.Line(res)
	output := buf.String()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")

	// Should have symbol, first line with error, then warnings
	if !strings.HasPrefix(lines[0], "✗") {
		t.Errorf("failed should start with ✗, got: %s", lines[0])
	}
	if !strings.Contains(lines[0], "clone failed") {
		t.Errorf("error should be on first line, got: %s", lines[0])
	}
	// Remaining lines should be warnings
	if !strings.HasPrefix(lines[1], "    warning1") {
		t.Errorf("warning1 not present: %s", lines[1])
	}
	if !strings.HasPrefix(lines[2], "    warning2") {
		t.Errorf("warning2 not present: %s", lines[2])
	}
}

// Test color output
func TestLineWithColor(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: true}
	res := syncer.Result{Name: "repo", Action: syncer.Cloned}
	r.Line(res)
	output := buf.String()

	if !strings.Contains(output, "\x1b[32m") {
		t.Errorf("expected green color code in output, got: %v", []byte(output))
	}
	if !strings.Contains(output, "\x1b[0m") {
		t.Errorf("expected reset code in output, got: %v", []byte(output))
	}
}

// Test no color output
func TestLineWithoutColor(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	res := syncer.Result{Name: "repo", Action: syncer.Cloned}
	r.Line(res)
	output := buf.String()

	if strings.Contains(output, "\x1b[") {
		t.Errorf("should not contain ANSI codes when Color=false, got: %v", []byte(output))
	}
}

// Test Summary with various counts
func TestSummaryOmitsZeros(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Cloned},
		{Name: "r2", Action: syncer.Cloned},
		{Name: "r3", Action: syncer.Updated},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	// Extract the Done: line
	lines := strings.Split(output, "\n")
	var doneLine string
	for _, line := range lines {
		if strings.HasPrefix(line, "Done:") {
			doneLine = line
			break
		}
	}

	if doneLine == "" {
		t.Fatalf("expected 'Done:' line in summary")
	}

	// Assert exact format with only non-zero counts
	expected := "Done: 2 cloned, 1 updated"
	if doneLine != expected {
		t.Errorf("expected %q, got %q", expected, doneLine)
	}
}

// Test Summary with nothing to do
func TestSummaryNothingToDo(t *testing.T) {
	results := []syncer.Result{}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	if !strings.Contains(output, "Done: nothing to do") {
		t.Errorf("expected 'Done: nothing to do' for empty results, got: %s", output)
	}
}

// Test Summary with Warnings section
func TestSummaryWarningsSection(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Updated, Warnings: []string{"warning from r1"}},
		{Name: "r2", Action: syncer.Cloned, Warnings: []string{"warning from r2"}},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	if !strings.Contains(output, "Warnings:") {
		t.Errorf("expected 'Warnings:' section, got: %s", output)
	}
	if !strings.Contains(output, "  r1: warning from r1") {
		t.Errorf("expected '  r1: warning from r1' in output, got: %s", output)
	}
	if !strings.Contains(output, "  r2: warning from r2") {
		t.Errorf("expected '  r2: warning from r2' in output, got: %s", output)
	}
}

// Test Summary with Failures section
func TestSummaryFailuresSection(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Failed, Err: errors.New("clone failed")},
		{Name: "r2", Action: syncer.Failed, Err: errors.New("fetch failed\nmore info")},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	if !strings.Contains(output, "Failures:") {
		t.Errorf("expected 'Failures:' section, got: %s", output)
	}
	if !strings.Contains(output, "  r1: clone failed") {
		t.Errorf("expected '  r1: clone failed' in output, got: %s", output)
	}
	if !strings.Contains(output, "  r2: fetch failed") {
		t.Errorf("expected '  r2: fetch failed' (first line only) in output, got: %s", output)
	}
}

// Test Summary with interrupted flag
func TestSummaryInterrupted(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Cloned},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, true)
	output := buf.String()

	if !strings.Contains(output, "Interrupted: remaining repos not processed.") {
		t.Errorf("expected interrupted message, got: %s", output)
	}
}

// Test ExitCode with failures
func TestExitCodeWithFailure(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Cloned},
		{Name: "r2", Action: syncer.Failed, Err: errors.New("failed")},
	}
	code := ExitCode(results)
	if code != 1 {
		t.Errorf("expected exit code 1 for failures, got %d", code)
	}
}

// Test ExitCode without failures
func TestExitCodeWithoutFailure(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Cloned},
		{Name: "r2", Action: syncer.Updated},
		{Name: "r3", Action: syncer.Skipped},
	}
	code := ExitCode(results)
	if code != 0 {
		t.Errorf("expected exit code 0 for no failures, got %d", code)
	}
}

// Test ExitCode with empty results
func TestExitCodeEmpty(t *testing.T) {
	results := []syncer.Result{}
	code := ExitCode(results)
	if code != 0 {
		t.Errorf("expected exit code 0 for empty results, got %d", code)
	}
}

// Test Summary count order
func TestSummaryCounts(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Cloned},
		{Name: "r2", Action: syncer.Updated},
		{Name: "r3", Action: syncer.UpToDate},
		{Name: "r4", Action: syncer.FetchedOnly},
		{Name: "r5", Action: syncer.Skipped},
		{Name: "r6", Action: syncer.Failed, Err: errors.New("fail")},
		{Name: "r7", Action: syncer.WouldClone},
		{Name: "r8", Action: syncer.WouldUpdate},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	// Extract just the Done: line
	lines := strings.Split(output, "\n")
	doneLine := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "Done:") {
			doneLine = line
			break
		}
	}

	if doneLine == "" {
		t.Fatalf("missing Done: line")
	}

	// Verify exact order: cloned, updated, up to date, fetched only, skipped, failed, would clone, would update
	expected := "Done: 1 cloned, 1 updated, 1 up to date, 1 fetched only, 1 skipped, 1 failed, 1 would clone, 1 would update"
	if doneLine != expected {
		t.Errorf("expected %q, got %q", expected, doneLine)
	}
}

// Test line format with name and action
func TestLineFormat(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	res := syncer.Result{Name: "my-repo", Action: syncer.Cloned}
	r.Line(res)
	output := buf.String()

	if !strings.Contains(output, "my-repo") {
		t.Errorf("expected repo name in output: %s", output)
	}
	if !strings.Contains(output, "cloned") {
		t.Errorf("expected action in output: %s", output)
	}
}

// Test line with first warning shown inline
func TestLineFirstWarningInline(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	res := syncer.Result{
		Name:     "repo",
		Action:   syncer.Updated,
		Warnings: []string{"first warning", "second warning"},
	}
	r.Line(res)
	output := buf.String()

	if !strings.Contains(output, "first warning") {
		t.Errorf("expected first warning inline: %s", output)
	}
}

// Test color for Failed action is red
func TestLineColorFailed(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: true}
	res := syncer.Result{Name: "repo", Action: syncer.Failed, Err: errors.New("failed")}
	r.Line(res)
	output := buf.String()

	if !strings.Contains(output, "\x1b[31m") {
		t.Errorf("expected red color code for Failed, got: %v", []byte(output))
	}
	if !strings.Contains(output, "\x1b[0m") {
		t.Errorf("expected reset code in output, got: %v", []byte(output))
	}
}

// Test color for warnings is yellow
func TestLineColorWarnings(t *testing.T) {
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: true}
	res := syncer.Result{Name: "repo", Action: syncer.Updated, Warnings: []string{"warning"}}
	r.Line(res)
	output := buf.String()

	if !strings.Contains(output, "\x1b[33m") {
		t.Errorf("expected yellow color code for warnings, got: %v", []byte(output))
	}
	if !strings.Contains(output, "\x1b[0m") {
		t.Errorf("expected reset code in output, got: %v", []byte(output))
	}
}

// Test Failures section with Failed action
func TestSummaryFailuresWithFailedAction(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Failed, Err: errors.New("clone failed")},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	if !strings.Contains(output, "Failures:") {
		t.Errorf("expected 'Failures:' section, got: %s", output)
	}
	if !strings.Contains(output, "  r1: clone failed") {
		t.Errorf("expected '  r1: clone failed' in output, got: %s", output)
	}
}

// Test Failures section with Failed action and nil error (fallback message)
func TestSummaryFailuresFailedWithNilError(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Failed},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	output := buf.String()

	if !strings.Contains(output, "Failures:") {
		t.Errorf("expected 'Failures:' section, got: %s", output)
	}
	if !strings.Contains(output, "  r1: failed") {
		t.Errorf("expected '  r1: failed' in output with fallback message, got: %s", output)
	}
}

func TestSummaryWarningsCount(t *testing.T) {
	results := []syncer.Result{
		{Name: "r1", Action: syncer.Updated, Warnings: []string{"a", "b"}},
		{Name: "r2", Action: syncer.Skipped, Warnings: []string{"c"}},
		{Name: "r3", Action: syncer.UpToDate},
	}
	var buf bytes.Buffer
	r := &Reporter{Out: &buf, Color: false}
	r.Summary(results, false)
	want := "Done: 1 updated, 1 up to date, 1 skipped, 2 with warnings"
	if !strings.Contains(buf.String(), want+"\n") {
		t.Errorf("expected %q in:\n%s", want, buf.String())
	}
}
