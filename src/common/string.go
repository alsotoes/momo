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
