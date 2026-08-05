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
	if strings.IndexAny(input, "\n\r") == -1 {
		return input
	}

	out := make([]byte, len(input))
	copy(out, input)
	for i := 0; i < len(out); i++ {
		c := out[i]
		if c == '\n' || c == '\r' {
			out[i] = '_'
		}
	}
	return unsafe.String(unsafe.SliceData(out), len(out))
}
