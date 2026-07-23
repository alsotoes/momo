package common

import (
	"bytes"
	"path"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// AppendPaddedInt appends the string representation of val to dst and pads it to width with null bytes.
// It assumes dst is large enough to hold at least width bytes and that the int representation
// is less than or equal to width bytes. If len(dst) < width, it returns syscall.EINVAL.
func AppendPaddedInt(dst []byte, val int64, width int) error {
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

// HasPathTraversalChars returns true if the string contains '.', '/' or '\'.
// It is inlineable and operates directly on the string bytes without any heap allocation (Rule 19).
func HasPathTraversalChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '/' || c == '\\' {
			return true
		}
	}
	return false
}

// PadString pads or truncates a string to the given length.
func PadString(input string, length int) string {
        if len(input) >= length {
                return input[:length]
        }
        b := make([]byte, length)
        copy(b, input)
        // ⚡ Bolt: Eliminate string allocation overhead by using unsafe.String.
        return unsafe.String(unsafe.SliceData(b), length)
}

// NormalizeVirtualPath cleans and validates virtual remote paths.
// It trims whitespace, resolves parent directory references via path.Clean, 
// and strictly rejects any directory traversal (..) sequences to prevent security escalation.
func NormalizeVirtualPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", nil
	}

	// Strictly reject parent directory references and backslashes immediately
	if strings.Contains(p, "..") || strings.Contains(p, "\\") {
		return "", syscall.EINVAL
	}

	// Resolve slashes and remove redundancies efficiently
	cleaned := path.Clean(p)

	// Split and validate each segment to ensure no empty or whitespace-only paths exist
	segments := strings.Split(cleaned, "/")
	var validSegments []string

	for _, seg := range segments {
		trimmedSeg := strings.TrimSpace(seg)
		if trimmedSeg == "" || trimmedSeg == "." || trimmedSeg == ".." {
			continue
		}
		validSegments = append(validSegments, trimmedSeg)
	}

	if len(validSegments) == 0 {
		return "", syscall.EINVAL
	}

	return strings.Join(validSegments, "/"), nil
}

// TrimNullBytesString finds the first null byte and returns a string up to that byte
// using unsafe.String to eliminate string allocation overhead.
func TrimNullBytesString(b []byte) string {
	if idx := bytes.IndexByte(b, 0); idx != -1 {
		return unsafe.String(unsafe.SliceData(b), idx)
	}
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}
