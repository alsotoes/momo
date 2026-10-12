package storage

import (
	"strings"
	"testing"
	"fmt"
	"syscall"
)

func BenchmarkParseACLResource_Split(b *testing.B) {
	resource := "object/my-bucket/my-key/with/slashes"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseACLResource(resource)
	}
}

func parseACLResource_IndexByte(resource string) (bucket, object string, err error) {
	switch {
	case strings.HasPrefix(resource, "bucket/"):
		return resource[7:], "", nil
	case strings.HasPrefix(resource, "object/"):
		rest := resource[7:]
		idx := strings.IndexByte(rest, '/')
		if idx == -1 {
			return "", "", fmt.Errorf("invalid object resource format: %w", syscall.EINVAL)
		}
		return rest[:idx], rest[idx+1:], nil
	default:
		return "", "", fmt.Errorf("invalid resource format: %w", syscall.EINVAL)
	}
}

func BenchmarkParseACLResource_IndexByte(b *testing.B) {
	resource := "object/my-bucket/my-key/with/slashes"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseACLResource_IndexByte(resource)
	}
}
