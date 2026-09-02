package generator

import (
	"fmt"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/model"
)

func GenerateADR(adr model.ADR, specID string, num int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %04d-%s\n\n", num, specID))

	b.WriteString("## Status\n")
	b.WriteString(adr.Status + "\n\n")

	b.WriteString("## Confidence\n")
	b.WriteString(string(adr.Confidence) + "\n\n")

	b.WriteString("## Context\n")
	b.WriteString(adr.Context + "\n\n")

	b.WriteString("## Decision\n")
	b.WriteString(adr.Decision + "\n\n")

	b.WriteString("## Consequences\n")
	b.WriteString(adr.Consequences + "\n\n")

	b.WriteString("## Alternatives Considered\n")
	if len(adr.Alternatives) == 0 {
		b.WriteString("None documented.\n\n")
	} else {
		for _, alt := range adr.Alternatives {
			b.WriteString(fmt.Sprintf("- **%s**: %s", alt.Label, alt.Description))
			if len(alt.Pros) > 0 || len(alt.Cons) > 0 {
				b.WriteString(" — ")
				if len(alt.Pros) > 0 {
					b.WriteString("Pros: " + strings.Join(alt.Pros, ", "))
				}
				if len(alt.Cons) > 0 {
					if len(alt.Pros) > 0 {
						b.WriteString("; ")
					}
					b.WriteString("Cons: " + strings.Join(alt.Cons, ", "))
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("## Confidence\n")
	b.WriteString(string(adr.Confidence) + "\n\n")

	b.WriteString("## Implementation Status\n")
	b.WriteString(fmt.Sprintf("- **Code**: %s\n", adr.Implementation.Code))
	b.WriteString(fmt.Sprintf("- **Tests**: %s\n", adr.Implementation.Tests))
	b.WriteString(fmt.Sprintf("- **Docs**: %s\n", adr.Implementation.Docs))
	b.WriteString(fmt.Sprintf("- **Blog post**: %s\n\n", adr.Implementation.Blog))

	b.WriteString("## References\n")
	b.WriteString(fmt.Sprintf("- Issue: %s\n", adr.References.Issue))
	b.WriteString(fmt.Sprintf("- PR: %s\n", adr.References.PR))
	b.WriteString(fmt.Sprintf("- Spec: `openspec/changes/%s/`\n", adr.References.Spec))
	b.WriteString(fmt.Sprintf("- Blog: %s\n", adr.References.Blog))
	if adr.References.Supersedes != "" {
		b.WriteString(fmt.Sprintf("- Supersedes: %s\n", adr.References.Supersedes))
	}
	if adr.References.SupersededBy != "" {
		b.WriteString(fmt.Sprintf("- Superseded by: %s\n", adr.References.SupersededBy))
	}
	b.WriteString("\n")

	return b.String()
}

func FormatStatus(status string) string {
	switch status {
	case "done", "Done", "DONE":
		return "Accepted"
	case "proposed", "Proposed", "PROPOSED":
		return "Proposed"
	case "deprecated", "Deprecated", "DEPRECATED":
		return "Deprecated"
	default:
		return "Proposed"
	}
}
