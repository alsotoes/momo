package parser

import (
	"regexp"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/model"
)

var (
	taskRe     = regexp.MustCompile(`(?m)^-\s*\[([ x])\]\s*(.+)$`)
	categoryRe = regexp.MustCompile(`(?m)^##\s+(.+)$`)
)

func ParseTasks(content string) model.Tasks {
	tasks := model.Tasks{
		Categories: make(map[string][]model.TaskItem),
	}

	// Find all category sections
	catMatches := categoryRe.FindAllStringSubmatchIndex(content, -1)
	for i, match := range catMatches {
		if len(match) < 4 {
			continue
		}
		category := strings.TrimSpace(content[match[2]:match[3]])

		// Find content until next category or end
		start := match[1]
		end := len(content)
		if i+1 < len(catMatches) {
			end = catMatches[i+1][0]
		}
		sectionContent := content[start:end]

		// Parse tasks in this section
		taskMatches := taskRe.FindAllStringSubmatch(sectionContent, -1)
		for _, m := range taskMatches {
			if len(m) >= 3 {
				done := m[1] == "x"
				text := strings.TrimSpace(m[2])

				// Determine category from text or section
				taskCategory := inferCategory(text, category)

				tasks.Categories[taskCategory] = append(tasks.Categories[taskCategory], model.TaskItem{
					Text:     text,
					Done:     done,
					Category: taskCategory,
				})
			}
		}
	}

	return tasks
}

func inferCategory(text, sectionCategory string) string {
	text = strings.ToLower(text)
	sectionCategory = strings.ToLower(sectionCategory)

	// Check for explicit category prefixes
	if strings.HasPrefix(text, "code:") || strings.HasPrefix(text, "impl:") {
		return "Code"
	}
	if strings.HasPrefix(text, "test:") || strings.HasPrefix(text, "spec:") {
		return "Tests"
	}
	if strings.HasPrefix(text, "doc:") || strings.HasPrefix(text, "docs:") {
		return "Docs"
	}

	// Infer from section category
	switch {
	case strings.Contains(sectionCategory, "test") || strings.Contains(sectionCategory, "spec"):
		return "Tests"
	case strings.Contains(sectionCategory, "doc"):
		return "Docs"
	case strings.Contains(sectionCategory, "code") || strings.Contains(sectionCategory, "impl") || strings.Contains(sectionCategory, "implement"):
		return "Code"
	default:
		// Heuristic based on keywords
		textLower := strings.ToLower(text)
		if strings.Contains(textLower, "test") || strings.Contains(textLower, "spec") || strings.Contains(textLower, "benchmark") {
			return "Tests"
		}
		if strings.Contains(textLower, "doc") || strings.Contains(textLower, "readme") || strings.Contains(textLower, "changelog") {
			return "Docs"
		}
		return "Code"
	}
}
