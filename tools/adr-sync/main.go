package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/model"
	"github.com/alsotoes/momo/tools/adr-sync/parser"
)

func main() {
	checkOnly := flag.Bool("check-only", false, "Only check if ADRs are in sync, don't write")
	flag.Parse()

	entries, err := os.ReadDir("openspec/changes")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading specs dir:", err)
		os.Exit(1)
	}

	var specIDs []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != "archive" {
			specIDs = append(specIDs, entry.Name())
		}
	}
	sort.Strings(specIDs)

	mismatch := false

	for i, specID := range specIDs {
		num := i + 1

		specDirs, _ := filepath.Glob(filepath.Join("openspec", "changes", specID, "specs", "*", "spec.md"))
		if len(specDirs) == 0 {
			continue
		}

		proposalBytes, _ := os.ReadFile(filepath.Join("openspec", "changes", specID, "proposal.md"))
		proposalContent := string(proposalBytes)

		specBytes, _ := os.ReadFile(specDirs[0])
		specContent := string(specBytes)

		tasksBytes, _ := os.ReadFile(filepath.Join("openspec", "changes", specID, "tasks.md"))
		tasksContent := string(tasksBytes)

		proposal := parser.ParseProposal(proposalContent)
		spec := parser.ParseSpec(specContent)
		tasks := parser.ParseTasks(tasksContent)

		decision := buildDecisionFromSpec(spec)
		status := deriveStatus(tasks)
		confidence := deriveConfidence(tasks)
		code, tests, docs := deriveImplementation(tasks)

		expectedContent := fmt.Sprintf("# %04d-%s\n\n## Status\n%s\n\n## Confidence\n%s\n\n## Context\n%s\n\n## Decision\n%s\n\n## Consequences\n%s\n\n## Alternatives Considered\nNone documented.\n\n## Implementation Status\n- **Code**: %s\n- **Tests**: %s\n- **Docs**: %s\n- **Blog post**: docs/blog/posts/...md\n\n## References\n- Issue: #...\n- PR: #...\n- Spec: openspec/changes/%s/\n- Blog: docs/blog/posts/...md\n", num, specID, status, confidence, proposal.Why, decision, spec.Consequences, code, tests, docs, specID)

		adrPath := filepath.Join("docs", "adr", fmt.Sprintf("%04d-%s.md", i+1, specID))

		if *checkOnly {
			existing, err := os.ReadFile(adrPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ADR missing: %s\n", adrPath)
				mismatch = true
				continue
			}
			if string(existing) != expectedContent {
				fmt.Fprintf(os.Stderr, "ADR out of sync: %s\n", adrPath)
				mismatch = true
			}
		} else {
			if err := os.WriteFile(adrPath, []byte(expectedContent), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing ADR: %v\n", err)
				os.Exit(1)
			}
		}
	}

	if *checkOnly && mismatch {
		fmt.Fprintln(os.Stderr, "ADRs out of sync with specs")
		os.Exit(1)
	} else if !*checkOnly {
		fmt.Printf("Synced %d ADRs\n", len(specIDs))
	}
}

func buildDecisionFromSpec(spec model.SpecDoc) string {
	var parts []string
	for _, req := range spec.Requirements {
		parts = append(parts, fmt.Sprintf("- %s: %s", req.Title, req.Summary))
	}
	return strings.Join(parts, "\n")
}

// deriveStatus maps task completion to ADR Status per Rule 78:
// Accepted (all tasks done), Proposed (partial), Deprecated (archive, unreachable here).
func deriveStatus(tasks model.Tasks) string {
	done, total := taskCounts(tasks)
	if total == 0 || done < total {
		return "Proposed"
	}
	return "Accepted"
}

// deriveConfidence maps task completion ratio to High/Medium/Low per Rule 78.
func deriveConfidence(tasks model.Tasks) string {
	done, total := taskCounts(tasks)
	if total == 0 {
		return "Low"
	}
	ratio := float64(done) / float64(total)
	switch {
	case ratio >= 0.9:
		return "High"
	case ratio >= 0.5:
		return "Medium"
	default:
		return "Low"
	}
}

// deriveImplementation returns per-category status from tasks.md checkboxes.
func deriveImplementation(tasks model.Tasks) (code, tests, docs string) {
	code = categoryStatus(tasks, "Code")
	tests = categoryStatus(tasks, "Tests")
	docs = categoryStatus(tasks, "Docs")
	return
}

func categoryStatus(tasks model.Tasks, category string) string {
	items := tasks.Categories[category]
	if len(items) == 0 {
		return "Planned"
	}
	done := 0
	for _, it := range items {
		if it.Done {
			done++
		}
	}
	switch {
	case done == len(items):
		return "Done"
	case done == 0:
		return "Planned"
	default:
		return "Partial"
	}
}

func taskCounts(tasks model.Tasks) (done, total int) {
	for _, items := range tasks.Categories {
		for _, it := range items {
			total++
			if it.Done {
				done++
			}
		}
	}
	return
}
