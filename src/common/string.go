package common

import (
	"path"
	"strings"
	"syscall"
	"unsafe"
)

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
