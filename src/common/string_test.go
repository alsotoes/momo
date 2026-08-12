package common

import (
	"strings"
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
		{"Null byte", "hello\x00world", "hello_world"},
		{"ANSI escape", "hello\x1b[31mred", "hello_[31mred"},
		{"Control chars", "a\x01b\x02c\x07d", "a_b_c_d"},
		{"Tab preserved", "a\tb", "a\tb"},
		{"Vertical tab and FF", "a\x0bb\x0cc", "a_b_c"},
		{"DEL preserved", "a\x7fb", "a\x7fb"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeLog(tt.input); got != tt.expected {
				t.Errorf("SanitizeLog(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTrimNullBytesFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"With null bytes", "hello\x00\x00\x00", "hello"},
		{"Without null bytes", "world", "world"},
		{"Empty string", "", ""},
		{"Only null bytes", "\x00\x00\x00", ""},
		{"Null byte in middle", "hello\x00world", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TrimNullBytesFromString(tt.input); got != tt.expected {
				t.Errorf("TrimNullBytesFromString(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestHasPathTraversalChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"No special characters", "helloworld", false},
		{"Contains dot (extension)", "hello.world", false},
		{"Contains slash", "hello/world", true},
		{"Contains backslash", "hello\\world", true},
		{"Path traversal", "../etc/passwd", true},
		{"Just dots", "..", true},
		{"Double dot in filename", "file..txt", true},
		{"Empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasPathTraversalChars(tt.input); got != tt.expected {
				t.Errorf("HasPathTraversalChars(%q) = %v, want %v", tt.input, got, tt.expected)
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

func TestPadString_OverlongInputPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("PadString(%q, %d) expected panic, got nil", "hello world", 5)
		}
	}()
	PadString("hello world", 5)
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
		{"Interior segment whitespace preserved", "customer01/my folder/document", "customer01/my folder/document", false},
		{"Segment leading/trailing spaces preserved", "customer01/ mydoc /file", "customer01/ mydoc /file", false},
		{"Whitespace-only segment dropped", "customer01/  /documents", "customer01/documents", false},
		{"Empty path", "", "", false},
		{"Spaces and slashes only", "  /  ///  ", "", true},
		{"Traversal segment", "customer01/../etc", "", true},
		{"Traversal prefix", "../customer01", "", true},
		{"Nested traversal", "a/b/../../c", "", true},
		{"Null byte in segment", "file\x00.txt", "", true},
		{"Null byte after slash", "customer01/doc\x00uments", "", true},
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

func TestTrimNullBytesString_BufferReuse(t *testing.T) {
	buf := []byte("hello\x00world")
	result := TrimNullBytesString(buf)
	if result != "hello" {
		t.Fatalf("expected 'hello', got %q", result)
	}
	buf[0] = 'X'
	if result != "hello" {
		t.Fatalf("result changed after buffer reuse: got %q, expected 'hello'", result)
	}
}

func BenchmarkSanitizeLog(b *testing.B) {
	safe := strings.Repeat("hello world ", 50)
	unsafe := strings.Repeat("hello\r\nworld\n", 50)
	b.Run("Safe", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeLog(safe)
		}
	})
	b.Run("Unsafe", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			SanitizeLog(unsafe)
		}
	})
}

func TestReplaceCRLF(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean", "hello world", "hello world"},
		{"only CR", "hello\rworld", "hello world"},
		{"only LF", "hello\nworld", "hello world"},
		{"both CRLF", "hello\r\nworld\r\n", "hello  world  "},
		{"multiple", "\r\r\n\n", "    "},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ReplaceCRLF(tt.input)
			if result != tt.expected {
				t.Errorf("ReplaceCRLF(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}
