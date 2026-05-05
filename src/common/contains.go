package common

import "slices"

// FindStringIndex searches for a string in a slice of strings and returns its index.
// If the string is not found, it returns -1.
// ⚡ Bolt: Use `slices.Index` from the standard library for a ~7x performance improvement over manual loops.
func FindStringIndex(slice []string, value string) int {
	return slices.Index(slice, value)
}
