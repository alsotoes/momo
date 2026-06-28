package common

import "unsafe"

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

// NormalizeVirtualPath trims leading/trailing slashes, resolutions, and whitespace.
func NormalizeVirtualPath(p string) string {
	for len(p) > 0 && (p[0] == ' ' || p[0] == '/') {
		p = p[1:]
	}
	for len(p) > 0 && (p[len(p)-1] == ' ' || p[len(p)-1] == '/') {
		p = p[:len(p)-1]
	}
	// Replace consecutive slashes
	for i := 0; i < len(p)-1; i++ {
		if p[i] == '/' && p[i+1] == '/' {
			p = p[:i] + p[i+1:]
			i--
		}
	}
	return p
}
