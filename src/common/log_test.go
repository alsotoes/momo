package common

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

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
