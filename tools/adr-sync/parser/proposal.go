package parser

import (
	"regexp"
	"strings"

	"github.com/alsotoes/momo/tools/adr-sync/model"
)

var (
	sectionRe = regexp.MustCompile(`(?m)^##\s+(.+)$`)
	contentRe = regexp.MustCompile(`(?s)##\s+([^#\n]+)\n(.*?)(?:\n##|$)`)
)

func ParseProposal(content string) model.Proposal {
	proposal := model.Proposal{}

	sections := extractSections(content)

	if why, ok := sections["Why"]; ok {
		proposal.Why = cleanText(why)
	}
	if what, ok := sections["What Changes"]; ok {
		proposal.WhatChanges = cleanText(what)
	}
	if nonGoals, ok := sections["Non-Goals"]; ok {
		proposal.NonGoals = cleanText(nonGoals)
	}

	return proposal
}

func extractSections(content string) map[string]string {
	sections := make(map[string]string)
	matches := contentRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			key := strings.TrimSpace(m[1])
			val := strings.TrimSpace(m[2])
			sections[key] = val
		}
	}
	return sections
}

func cleanText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\n\r\t ")
	return text
}
