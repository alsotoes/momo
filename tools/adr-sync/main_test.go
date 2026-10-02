package main

import "testing"

func TestParseADRFileName(t *testing.T) {
	tests := []struct {
		base    string
		wantID  string
		wantNum int
		wantOK  bool
	}{
		{"0029-go-1.26-upgrade.md", "go-1.26-upgrade", 29, true},
		{"0056-go-1.26-upgrade.md", "go-1.26-upgrade", 56, true},
		{"0006-add-comprehensive-testing.md", "add-comprehensive-testing", 6, true},
		{"0006-add-comprehensive-testing.no-blog.md", "", 0, false},
		{"0053-protocol-cluster-separation.no-blog.md", "", 0, false},
		{"0020-blog-posts-hugo.md", "blog-posts-hugo", 20, true},
		{"not-an-adr.md", "", 0, false},
		{"0027-blog-journal-readability.md", "blog-journal-readability", 27, true},
		{"readme.md", "", 0, false},
		{"123-short.md", "", 0, false},
	}
	for _, tc := range tests {
		id, num, ok := parseADRFileName(tc.base)
		if ok != tc.wantOK || id != tc.wantID || num != tc.wantNum {
			t.Errorf("parseADRFileName(%q) = (%q, %d, %v), want (%q, %d, %v)",
				tc.base, id, num, ok, tc.wantID, tc.wantNum, tc.wantOK)
		}
	}
}
