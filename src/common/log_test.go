package common

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestSanitizeLog(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no newlines",
			input:    "normal log entry",
			expected: "normal log entry",
		},
		{
			name:     "with line feed",
			input:    "injected\nlog",
			expected: "injected_log",
		},
		{
			name:     "with carriage return",
			input:    "injected\rlog",
			expected: "injected_log",
		},
		{
			name:     "with crlf",
			input:    "injected\r\nlog",
			expected: "injected__log",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := SanitizeLog(tc.input)
			if actual != tc.expected {
				t.Errorf("SanitizeLog(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestLogStdOut(t *testing.T) {
	// Redirect log output to a buffer
	var buf bytes.Buffer
	log.SetOutput(&buf)

	// Test with logApp = true
	LogStdOut(true)
	log.Print("test true")
	if !strings.Contains(buf.String(), "test true") {
		t.Errorf("Expected 'test true' in log output, got %s", buf.String())
	}
	buf.Reset()

	// Test with logApp = false
	LogStdOut(false)
	log.Print("test false")
	if strings.Contains(buf.String(), "test false") {
		t.Errorf("Expected empty log output, got %s", buf.String())
	}
}
