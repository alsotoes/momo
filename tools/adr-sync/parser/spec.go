package parser

import (
	"regexp"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/model"
)

var (
	requirementRe = regexp.MustCompile(`(?m)^###\s+Requirement:\s*(.+)$`)
	scenarioRe    = regexp.MustCompile(`(?m)^####\s+Scenario:\s*(.+)$`)
	gherkinRe     = regexp.MustCompile(`(?m)^-\s*(GIVEN|WHEN|THEN)\s+(.+)$`)
)

func ParseSpec(content string) model.SpecDoc {
	spec := model.SpecDoc{}

	requirements := parseRequirements(content)
	spec.Requirements = requirements

	spec.Consequences = extractSection(content, []string{"Consequences", "Impact", "Trade-offs"})
	spec.Alternatives = parseAlternatives(content)

	return spec
}

func parseRequirements(content string) []model.Requirement {
	var requirements []model.Requirement

	reqMatches := requirementRe.FindAllStringSubmatchIndex(content, -1)
	for i, match := range reqMatches {
		if len(match) < 4 {
			continue
		}
		title := strings.TrimSpace(content[match[2]:match[3]])

		start := match[1]
		end := len(content)
		if i+1 < len(reqMatches) {
			end = reqMatches[i+1][0]
		}
		reqContent := content[start:end]

		summary := extractSummary(reqContent)

		requirements = append(requirements, model.Requirement{
			Title:   title,
			Summary: summary,
		})
	}

	return requirements
}

func extractSummary(content string) string {
	lines := strings.Split(content, "\n")
	var summaryParts []string
	inScenario := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#### Scenario:") {
			inScenario = true
			continue
		}
		if inScenario && strings.HasPrefix(trimmed, "- ") {
			continue
		}
		if inScenario && trimmed == "" {
			inScenario = false
			continue
		}
		if !inScenario && trimmed != "" && !strings.HasPrefix(trimmed, "###") {
			summaryParts = append(summaryParts, trimmed)
		}
	}

	summary := strings.Join(summaryParts, " ")
	summary = strings.TrimSpace(summary)
	if len(summary) > 500 {
		summary = summary[:500] + "..."
	}
	return summary
}

func extractSection(content string, keys []string) string {
	for _, key := range keys {
		// Match ## Key followed by content until next ## or end of string
		re := regexp.MustCompile(`(?is)##\s+` + regexp.QuoteMeta(key) + `\s*\n(.*?)(?:\n##|$)`)
		if match := re.FindStringSubmatch(content); len(match) > 1 {
			return cleanText(match[1])
		}
	}
	return ""
}

func parseAlternatives(content string) []model.Alternative {
	var alts []model.Alternative

	altSection := extractSection(content, []string{"Alternatives", "Alternatives Considered", "Options"})
	if altSection == "" {
		return alts
	}

	altRe := regexp.MustCompile(`(?m)\*\*(Alternative\s+[A-Z]):\*\*\s*(.+?)\s*[—-]\s*Pros\s*/\s*Cons\s*:?\s*(.+?)(?:\n\*\*|\n###|\n##|$)`)
	matches := altRe.FindAllStringSubmatch(altSection, -1)

	for _, m := range matches {
		if len(m) >= 4 {
			label := strings.TrimSpace(m[1])
			desc := strings.TrimSpace(m[2])
			prosCons := strings.TrimSpace(m[3])

			pros, cons := parseProsCons(prosCons)

			alts = append(alts, model.Alternative{
				Label:       label,
				Description: desc,
				Pros:        pros,
				Cons:        cons,
			})
		}
	}

	return alts
}

func parseProsCons(text string) ([]string, []string) {
	var pros, cons []string
	parts := strings.Split(text, "/")
	if len(parts) >= 2 {
		pros = parseList(parts[0])
		cons = parseList(parts[1])
	}
	return pros, cons
}

func parseList(text string) []string {
	var items []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}
