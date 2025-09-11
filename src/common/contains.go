package common

// FindStringIndex searches for a string in a slice of strings and returns its index.
// If the string is not found, it returns -1.
// Source: https://programming.guide/go/find-search-contains-slice.html
func FindStringIndex(slice []string, value string) int {
	for i, item := range slice {
		if item == value {
			return i
		}
	}
	return -1
}
