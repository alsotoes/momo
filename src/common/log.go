package common

import (
	"io"
	"log"
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

// SanitizeLog strips control characters from a string to prevent log injection.
// All control bytes below 0x20 are replaced with '_' except tab (\x09) which is
// preserved. This neutralizes CRLF, null bytes, ANSI escape sequences (ESC \x1b),
// and other control characters that could manipulate terminals or hide log content.
func SanitizeLog(input string) string {
	if !hasControlByte(input) {
		return input
	}

	b := make([]byte, len(input))
	copy(b, input)
	for i := range b {
		if b[i] < 0x20 && b[i] != '\t' {
			b[i] = '_'
		}
	}
	// ⚡ Bolt: Eliminate string allocation overhead by using unsafe.String.
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func hasControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 && b != '\t' {
			return true
		}
	}
	return false
}
