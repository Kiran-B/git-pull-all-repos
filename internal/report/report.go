// Package report provides output formatting for sync results.
package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kiran-B/git-pull-all-repos/internal/syncer"
)

// Reporter formats and writes sync results.
// Must be called from a single goroutine only; not goroutine-safe (no mutex needed).
type Reporter struct {
	Out   io.Writer
	Color bool
}

// Line writes a single-line result for a repository.
func (r *Reporter) Line(res syncer.Result) {
	sym, colorCode := r.symbolAndColor(res)
	action := string(res.Action)

	// Determine detail: first warning or error
	detail := ""
	var remainingWarnings []string

	if res.Err != nil {
		// For Failed: show first line of error
		errMsg := res.Err.Error()
		lines := strings.Split(errMsg, "\n")
		detail = lines[0]
		// All warnings go to remaining lines
		remainingWarnings = res.Warnings
	} else if len(res.Warnings) > 0 {
		// For non-Failed with warnings: first warning inline
		detail = res.Warnings[0]
		remainingWarnings = res.Warnings[1:]
	}

	// Build first line
	line := fmt.Sprintf("%s %s  %s", sym, res.Name, action)
	if detail != "" {
		line += fmt.Sprintf(": %s", detail)
	}

	// Apply color if needed
	if r.Color {
		line = colorCode + line + "\x1b[0m"
	}

	fmt.Fprintln(r.Out, line)

	// Write remaining warnings as indented lines
	for _, warn := range remainingWarnings {
		indented := fmt.Sprintf("    %s", warn)
		if r.Color {
			indented = "\x1b[33m" + indented + "\x1b[0m" // yellow for warnings
		}
		fmt.Fprintln(r.Out, indented)
	}
}

// Summary writes the run summary.
func (r *Reporter) Summary(results []syncer.Result, interrupted bool) {
	// Blank line before summary
	fmt.Fprintln(r.Out)

	// Count actions
	counts := make(map[syncer.Action]int)
	for _, res := range results {
		counts[res.Action]++
	}

	// Build counts line in specified order
	var countParts []string
	order := []syncer.Action{
		syncer.Cloned,
		syncer.Updated,
		syncer.UpToDate,
		syncer.FetchedOnly,
		syncer.Skipped,
		syncer.Failed,
		syncer.WouldClone,
		syncer.WouldUpdate,
	}

	for _, action := range order {
		if count, ok := counts[action]; ok && count > 0 {
			countParts = append(countParts, fmt.Sprintf("%d %s", count, action))
		}
	}

	withWarnings := 0
	for _, res := range results {
		if len(res.Warnings) > 0 {
			withWarnings++
		}
	}
	if withWarnings > 0 {
		countParts = append(countParts, fmt.Sprintf("%d with warnings", withWarnings))
	}

	if len(countParts) == 0 {
		fmt.Fprintln(r.Out, "Done: nothing to do")
	} else {
		fmt.Fprintf(r.Out, "Done: %s\n", strings.Join(countParts, ", "))
	}

	// Collect warnings and failures
	var warnings []string
	var failures []string

	for _, res := range results {
		if len(res.Warnings) > 0 {
			for _, warn := range res.Warnings {
				warnings = append(warnings, fmt.Sprintf("  %s: %s", res.Name, warn))
			}
		}
		if res.Action == syncer.Failed {
			// Show first line of error, or fallback to "failed" if no error
			errMsg := "failed"
			if res.Err != nil {
				errMsg = res.Err.Error()
				lines := strings.Split(errMsg, "\n")
				errMsg = lines[0]
			}
			failures = append(failures, fmt.Sprintf("  %s: %s", res.Name, errMsg))
		}
	}

	// Write Warnings section if any
	if len(warnings) > 0 {
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, "Warnings:")
		for _, w := range warnings {
			fmt.Fprintln(r.Out, w)
		}
	}

	// Write Failures section if any
	if len(failures) > 0 {
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, "Failures:")
		for _, f := range failures {
			fmt.Fprintln(r.Out, f)
		}
	}

	// Write interrupted message if needed
	if interrupted {
		fmt.Fprintln(r.Out)
		fmt.Fprintln(r.Out, "Interrupted: remaining repos not processed.")
	}
}

// ExitCode returns the exit code for the results.
func ExitCode(results []syncer.Result) int {
	for _, res := range results {
		if res.Action == syncer.Failed {
			return 1
		}
	}
	return 0
}

// symbolAndColor returns both the symbol and color code for a result.
// This ensures they stay in sync and can't drift.
func (r *Reporter) symbolAndColor(res syncer.Result) (string, string) {
	sym := ""
	colorCode := ""

	if res.Action == syncer.Failed {
		sym = "✗"
		colorCode = "\x1b[31m" // red
	} else if res.Action == syncer.Skipped || len(res.Warnings) > 0 {
		sym = "⚠"
		colorCode = "\x1b[33m" // yellow
	} else if res.Action == syncer.Cloned || res.Action == syncer.WouldClone {
		sym = "+"
		colorCode = "\x1b[32m" // green
	} else {
		sym = "✓"
		colorCode = "\x1b[32m" // green
	}

	return sym, colorCode
}
