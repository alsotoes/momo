package common

import (
	"bytes"
	"fmt"
	"path"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// WritePaddedInt writes the string representation of val into dst starting at
// dst[0] and zero-pads it to width bytes. It overwrites the start of dst rather
// than appending at dst[len(dst)]; callers must pass a slice sized exactly
// width (typically a fixed-size buffer's sub-slice). If len(dst) < width or the
// int representation exceeds width bytes, it returns syscall.EINVAL.
func WritePaddedInt(dst []byte, val int64, width int) error {
	if len(dst) < width {
		return syscall.EINVAL
	}
	var numBuf [32]byte
	b := strconv.AppendInt(numBuf[:0], val, 10)
	if len(b) > width {
		return syscall.EINVAL
	}
	copy(dst, b)
	for i := len(b); i < width; i++ {
		dst[i] = 0
	}
	return nil
}

// HasPathTraversalChars returns true if the string contains path separators (/ or \)
// or the parent directory sequence (..). Single dots (file extensions) are allowed.
// It is inlineable and operates directly on the string bytes without any heap allocation (Rule 19).
func HasPathTraversalChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '/' || c == '\\' {
			return true
		}
		if c == '.' && i+1 < len(s) && s[i+1] == '.' {
			return true
		}
	}
	return false
}

// PadString pads a string with null bytes to the given length.
// Overlong input is a programming error that would silently corrupt wire
// fields (hashes, auth tokens, names). Failing loudly via panic (Rule 37:
// recovered at caller boundaries) is safer than silent truncation (Rule 4).
func PadString(input string, length int) string {
	if length < 0 {
		return input
	}
	if len(input) > length {
		panic(fmt.Sprintf("PadString: input length %d exceeds length %d", len(input), length))
	}
	if len(input) == length {
		return input
	}
	b := make([]byte, length)
	copy(b, input)
	// ⚡ Bolt: Eliminate string allocation overhead by using unsafe.String.
	return unsafe.String(unsafe.SliceData(b), length)
}

// NormalizeVirtualPath cleans and validates virtual remote paths.
// It trims surrounding whitespace, resolves parent directory references via path.Clean,
// and strictly rejects any directory traversal (..) sequences to prevent security escalation.
// Whitespace within path segments is significant and preserved verbatim (e.g. "my folder").
func NormalizeVirtualPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}

	// Strictly reject path traversal segments (..), backslashes, and null bytes.
	// Null bytes must be rejected: downstream C libraries, shells, or log
	// aggregators would truncate the path at the first \x00 (injection vector).
	if strings.ContainsRune(p, 0) {
		return "", syscall.EINVAL
	}
	if strings.Contains(p, "\\") {
		return "", syscall.EINVAL
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", syscall.EINVAL
		}
	}

	// Resolve slashes and remove redundancies efficiently
	cleaned := path.Clean(p)

	// Split and validate each segment to ensure no empty segments exist.
	// Segment content is preserved verbatim: whitespace within a segment is
	// meaningful (e.g. "my folder/file") and must not be trimmed.
	segments := strings.Split(cleaned, "/")
	var validSegments []string

	for _, seg := range segments {
		if seg == "" || seg == "." || seg == ".." || strings.TrimSpace(seg) == "" {
			continue
		}
		validSegments = append(validSegments, seg)
	}

	if len(validSegments) == 0 {
		return "", syscall.EINVAL
	}

	return strings.Join(validSegments, "/"), nil
}

// TrimNullBytesString finds the first null byte and returns a string up to that byte.
func TrimNullBytesString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx != -1 {
		return string(b[:idx])
	}
	return string(b)
}

// TrimNullBytesFromString finds the first null byte and returns a substring up to that byte
// using strings.IndexByte. This is significantly faster than strings.TrimRight.
func TrimNullBytesFromString(s string) string {
	if idx := strings.IndexByte(s, 0); idx != -1 {
		return s[:idx]
	}
	return s
}

// ReplaceCRLF replaces all carriage return (\r) and line feed (\n) characters
// in a string with spaces (' '). It operates in a single pass and avoids
// allocation entirely if no replacements are needed by using strings.IndexAny.

// Note: Callers must ensure the input string is bounded to prevent unbounded memory allocation (Rule 32).
func ReplaceCRLF(s string) string {
	if strings.IndexAny(s, "\r\n") == -1 {
		return s
	}
	b := make([]byte, len(s))
	copy(b, s)
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' || b[i] == '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}
