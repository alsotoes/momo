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
	s := strings.ReplaceAll(input, "\n", "_")
	s = strings.ReplaceAll(s, "\r", "_")
	return s
}
