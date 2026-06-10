package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

const (
	maxDiffLines = 1000 // Safety cap to avoid massive token spend
)

type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content Content `json:"content"`
	} `json:"candidates"`
}

func main() {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY environment variable is not set")
	}

	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-1.5-flash"
	}

	// 1. Get the PR diff
	diff, err := getFilteredDiff()
	if err != nil {
		log.Fatalf("Failed to get diff: %v", err)
	}

	if diff == "" {
		fmt.Println("No relevant changes to review.")
		return
	}

	// 2. Load Steering Rules
	rules, _ := os.ReadFile("openspec/project.md")

	// 3. Construct Prompt
	prompt := fmt.Sprintf(`You are an expert Go developer and security auditor.
Review the following Pull Request diff against the provided Project Steering Rules.

STERING RULES:
%s

PR DIFF:
%s

TASK:
- Identify violations of the Zero-Crash Pattern (missing recover, unbounded readers).
- Ensure error mappings use syscall constants (POSIX Error Mapping).
- Look for performance bottlenecks (unnecessary allocations in hot paths).
- Check for security issues (path traversal, sanitization).

INSTRUCTIONS:
- Be concise.
- If everything looks good, just say "✅ All Project Steering Rules and architectural patterns are respected."
- Do NOT repeat the diff or the rules.
- Format your response as a GitHub comment.`, string(rules), diff)

	// 4. Call Gemini API
	review, err := callGemini(apiKey, model, prompt)
	if err != nil {
		log.Fatalf("Gemini API call failed: %v", err)
	}

	// 5. Output the review (to be captured by the GitHub Action)
	fmt.Println(review)
}

func getFilteredDiff() (string, error) {
	// Only compare against origin/master
	cmd := exec.Command("git", "diff", "origin/master...HEAD", "--", ".", ":(exclude)vendor/*", ":(exclude)go.sum", ":(exclude)go.mod", ":(exclude)docs/*")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}

	lines := strings.Split(out.String(), "\n")
	if len(lines) > maxDiffLines {
		return strings.Join(lines[:maxDiffLines], "\n") + "\n\n[DIFF TRUNCATED FOR TOKEN EFFICIENCY]", nil
	}

	return out.String(), nil
}

func callGemini(apiKey, model, prompt string) (string, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, apiKey)

	reqBody := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{{Text: prompt}},
			},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	var geminiResp GeminiResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return "", err
	}

	if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
		return geminiResp.Candidates[0].Content.Parts[0].Text, nil
	}

	return "No review generated.", nil
}
