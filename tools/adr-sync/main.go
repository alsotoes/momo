package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/generator"
	"github.com/alsotoes/momo/tools/adr-sync/model"
	"github.com/alsotoes/momo/tools/adr-sync/parser"
)

var (
	issueLinkRe = regexp.MustCompile(`github\.com/[^/]+/[^/]+/issues/(\d+)`)
	prLinkRe    = regexp.MustCompile(`github\.com/[^/]+/[^/]+/pull/(\d+)`)
	issueRefRe  = regexp.MustCompile(`#(\d{2,})`)
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

		proposalBytes, err := os.ReadFile(filepath.Join("openspec", "changes", specID, "proposal.md"))
		if err != nil {
			proposalBytes = nil
		}
		proposalContent := string(proposalBytes)

		specBytes, _ := os.ReadFile(specDirs[0])
		specContent := string(specBytes)

		tasksBytes, _ := os.ReadFile(filepath.Join("openspec", "changes", specID, "tasks.md"))
		tasksContent := string(tasksBytes)

		proposal := parser.ParseProposal(proposalContent)
		spec := parser.ParseSpec(specContent)
		tasks := parser.ParseTasks(tasksContent)

		decision := buildDecisionFromSpec(spec)
		status := generator.FormatStatus(deriveStatus(tasks))
		confidence := deriveConfidence(tasks)
		code, tests, docs := deriveImplementation(tasks)

		issue := resolveIssue(proposalContent, specContent)
		blog := resolveBlog(specID)
		pr := resolvePR(proposalContent, blog)

		ctx := proposal.Why
		if ctx == "" {
			ctx = spec.Purpose
		}

		adr := model.ADR{
			Number:       num,
			SpecID:       specID,
			Status:       status,
			Confidence:   confidence,
			Context:      ctx,
			Decision:     decision,
			Consequences: spec.Consequences,
			Alternatives: spec.Alternatives,
			Implementation: model.Implementation{
				Code:  model.Status(code),
				Tests: model.Status(tests),
				Docs:  model.Status(docs),
				Blog:  blog,
			},
			References: model.References{
				Issue: issue,
				PR:    pr,
				Spec:  specID,
				Blog:  blog,
			},
		}

		expectedContent := generator.GenerateADR(adr, specID, num)

		adrPath := filepath.Join("docs", "adr", fmt.Sprintf("%04d-%s.md", num, specID))

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

// deriveStatus maps task completion to ADR status per Rule 78:
// Accepted (all tasks done), Proposed (partial).
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

// resolveIssue extracts the primary GitHub issue number. The authoritative
// source is the spec.md "GitHub Issue URL" line (the tracking issue). Falls
// back to the proposal's "Related Issues:" section (lowest number) or the first
// bare #NNNN reference.
func resolveIssue(proposalContent, specContent string) string {
	// 1. Authoritative: spec.md GitHub Issue URL
	if links := issueLinkRe.FindAllStringSubmatch(specContent, -1); len(links) > 0 {
		return "#" + links[0][1]
	}
	// 2. Proposal related-issues block (lowest number)
	links := issueLinkRe.FindAllStringSubmatch(proposalContent, -1)
	if len(links) > 0 {
		min := links[0][1]
		for _, m := range links {
			if m[1] < min {
				min = m[1]
			}
		}
		return "#" + min
	}
	// 3. First bare #NNNN reference
	refs := issueRefRe.FindAllStringSubmatch(proposalContent, -1)
	if len(refs) > 0 {
		return "#" + refs[0][1]
	}
	return ""
}

// resolvePR finds a pull-request link: first from the proposal's PR links, then
// from the matched blog post's front-matter artifacts ({type: pr, id: "NNN"}).
func resolvePR(proposalContent, blog string) string {
	if links := prLinkRe.FindAllStringSubmatch(proposalContent, -1); len(links) > 0 {
		return "#" + links[0][1]
	}
	if blog != "" {
		if data, err := os.ReadFile(blog); err == nil {
			prArt := regexp.MustCompile(`\{type:\s*pr,\s*id:\s*"(\d+)"\}`)
			if m := prArt.FindStringSubmatch(string(data)); len(m) > 1 {
				return "#" + m[1]
			}
		}
	}
	return ""
}

// resolveBlog matches docs/blog/posts/<n>-<slug>.md for this spec. Resolution
// order:
//  1. Filename contains the spec ID.
//  2. Front-matter artifacts block: exact `path: openspec/changes/<specID>`.
//     When multiple posts reference the same spec, the most dedicated post wins
//     (fewest `spec` artifacts listed); ties go to the highest post number.
//  3. Body fallback (spec IDs ≥12 chars) when no artifact match.
//
// Returns "" when no post is associated (e.g. bugfix posts exempt from Rule 76).
func resolveBlog(specID string) string {
	matches, _ := filepath.Glob(filepath.Join("docs", "blog", "posts", "*.md"))
	for _, m := range matches {
		base := filepath.Base(m)
		if strings.Contains(base, specID) {
			return "docs/blog/posts/" + base
		}
	}
	artifactRe := regexp.MustCompile(`path:\s*openspec/changes/` + regexp.QuoteMeta(specID) + `\s*[}\]]`)
	specCountRe := regexp.MustCompile(`path:\s*openspec/changes/`)
	best := ""
	bestSpecs := int(^uint(0) >> 1) // max int
	for _, m := range matches {
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		if !artifactRe.Match(data) {
			continue
		}
		nSpecs := len(specCountRe.FindAll(data, -1))
		// Fewer listed specs = more dedicated post; ties → highest post number.
		if nSpecs < bestSpecs || (nSpecs == bestSpecs && m > best) {
			bestSpecs = nSpecs
			best = m
		}
	}
	if best != "" {
		return "docs/blog/posts/" + filepath.Base(best)
	}
	for _, m := range matches {
		if len(specID) < 12 {
			continue
		}
		data, err := os.ReadFile(m)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), specID) {
			return "docs/blog/posts/" + filepath.Base(m)
		}
	}
	return ""
}
