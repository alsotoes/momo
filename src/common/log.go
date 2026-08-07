package common

import (
	"io"
	"log"
	"strings"
	"unsafe"
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
	idx := strings.IndexAny(input, "\n\r")
	if idx == -1 {
		return input
	}

	b := make([]byte, len(input))
	copy(b, input)
	for i := idx; i < len(b); i++ {
		c := b[i]
		if c == '\n' || c == '\r' {
			b[i] = '_'
		}
	}
	// ⚡ Bolt: Eliminate string allocation overhead by using unsafe.String.
	return unsafe.String(unsafe.SliceData(b), len(b))
}
