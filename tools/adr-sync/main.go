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

		proposal := parser.ParseProposal(proposalContent)
		spec := parser.ParseSpec(specContent)

		decision := buildDecisionFromSpec(spec)

		expectedContent := fmt.Sprintf("# %04d-%s\n\n## Status\nAccepted\n\n## Confidence\nHigh\n\n## Context\n%s\n\n## Decision\n%s\n\n## Consequences\n%s\n\n## Alternatives Considered\nNone documented.\n\n## Confidence\nHigh\n\n## Implementation Status\n- **Code**: Done\n- **Tests**: Done\n- **Docs**: Done\n- **Blog post**: docs/blog/posts/...md\n\n## References\n- Issue: #...\n- PR: #...\n- Spec: openspec/changes/%s/\n- Blog: docs/blog/posts/...md\n", num, specID, proposal.Why, decision, spec.Consequences, specID)

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
