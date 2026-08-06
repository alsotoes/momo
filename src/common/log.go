package common

import (
	"io"
	"log"
	"strings"
)

// LogStdOut configures the logging output for the application.
// If logApp is true, it sets the log flags to include timestamps, file names, and line numbers.
// If logApp is false, it discards all log output.
func LogStdOut(logApp bool) {
	if logApp {
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile | log.LUTC)
	} else {
		log.SetOutput(io.Discard)
	}
}

// SanitizeLog removes CRLF characters from a string to prevent log injection.
func SanitizeLog(input string) string {
	// ⚡ Bolt: Optimize sequential ReplaceAll by using a fast-path match check
	// and a strings.Builder to eliminate unnecessary allocations and unsafe code.
	if !strings.ContainsAny(input, "\n\r") {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == '\n' || c == '\r' {
			b.WriteByte('_')
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}
