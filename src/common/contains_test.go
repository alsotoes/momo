package common

import "testing"

func TestContains(t *testing.T) {
	slice := []string{"a", "b", "c"}
	
	// Test case 1: String is in the slice
	if Contains(slice, "b") == -1 {
		t.Errorf("Expected to find 'b' in the slice, but it was not found")
	}

	// Test case 2: String is not in the slice
	if Contains(slice, "d") != -1 {
		t.Errorf("Expected to not find 'd' in the slice, but it was found")
	}
}