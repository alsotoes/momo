package common

import (
	"testing"
)

func TestSanitizeLog(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"No special characters", "hello world", "hello world"},
		{"CRLF injection", "hello\r\nworld", "hello__world"},
		{"Multiple CRLFs", "\r\nhello\r\nworld\r\n", "__hello__world__"},
		{"Just CR", "hello\rworld", "hello_world"},
		{"Just LF", "hello\nworld", "hello_world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeLog(tt.input); got != tt.expected {
				t.Errorf("SanitizeLog(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSafeParseInt(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		want    int64
		wantErr bool
	}{
		{"Simple positive", []byte("12345"), 12345, false},
		{"Simple negative", []byte("-12345"), -12345, false},
		{"Null padded", []byte("123\x00\x00"), 123, false},
		{"Max Int64", []byte("9223372036854775807"), 9223372036854775807, false},
		{"Min Int64", []byte("-9223372036854775808"), -9223372036854775808, false},
		{"Invalid character", []byte("12a34"), 0, true},
		{"Empty input", []byte(""), 0, true},
		{"Only nulls", []byte("\x00\x00"), 0, true},
		{"Overflow", []byte("9223372036854775808"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafeParseInt(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafeParseInt(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("SafeParseInt(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestPadString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		length   int
		expected string
	}{
		{"Short string", "hello", 10, "hello\x00\x00\x00\x00\x00"},
		{"Exact length", "hello", 5, "hello"},
		{"Long string", "hello world", 5, "hello"},
		{"Empty string", "", 3, "\x00\x00\x00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PadString(tt.input, tt.length); got != tt.expected {
				t.Errorf("PadString(%q, %d) = %q, want %q", tt.input, tt.length, got, tt.expected)
			}
		})
	}
}

func TestNormalizeVirtualPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"Simple clean path", "customer01/documents", "customer01/documents", false},
		{"Trim leading slash", "/customer01/documents", "customer01/documents", false},
		{"Trim trailing slash", "customer01/documents/", "customer01/documents", false},
		{"Surrounding whitespace", "  customer01/documents  ", "customer01/documents", false},
		{"Consecutive slashes", "customer01//documents///invoice.pdf", "customer01/documents/invoice.pdf", false},
		{"Empty path", "", "", false},
		{"Spaces and slashes only", "  /  ///  ", "", true},
		{"Traversal segment", "customer01/../etc", "", true},
		{"Traversal prefix", "../customer01", "", true},
		{"Nested traversal", "a/b/../../c", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeVirtualPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeVirtualPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeVirtualPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
