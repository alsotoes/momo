package common

import "testing"

func TestFindStringIndex(t *testing.T) {
	testCases := []struct {
		name          string
		slice         []string
		value         string
		expectedIndex int
	}{
		{
			name:          "String in slice",
			slice:         []string{"a", "b", "c"},
			value:         "b",
			expectedIndex: 1,
		},
		{
			name:          "String not in slice",
			slice:         []string{"a", "b", "c"},
			value:         "d",
			expectedIndex: -1,
		},
		{
			name:          "Empty slice",
			slice:         []string{},
			value:         "a",
			expectedIndex: -1,
		},
		{
			name:          "Slice with duplicates",
			slice:         []string{"a", "b", "b", "c"},
			value:         "b",
			expectedIndex: 1, // Should return the first occurrence
		},
		{
			name:          "String at the beginning",
			slice:         []string{"a", "b", "c"},
			value:         "a",
			expectedIndex: 0,
		},
		{
			name:          "String at the end",
			slice:         []string{"a", "b", "c"},
			value:         "c",
			expectedIndex: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actualIndex := FindStringIndex(tc.slice, tc.value)
			if actualIndex != tc.expectedIndex {
				t.Errorf("Expected index %d, but got %d", tc.expectedIndex, actualIndex)
			}
		})
	}
}
