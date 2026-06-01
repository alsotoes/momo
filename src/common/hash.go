package common

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// HashFile calculates the SHA-256 hash of a file.
// It takes the file path as input and returns the SHA-256 hash as a hex-encoded string.
func HashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	// ⚡ Bolt: Use a stack-allocated buffer for hashing to eliminate heap allocation during file I/O.
	var copyBuf [32 * 1024]byte
	if _, err := io.CopyBuffer(hash, file, copyBuf[:]); err != nil {
		return "", err
	}
	// ⚡ Bolt: Pre-allocate a fixed-size array on the stack and pass a zero-length slice of it
	// to hash.Sum() to eliminate the heap allocation and improve performance.
	var sumBuf [sha256.Size]byte
	return hex.EncodeToString(hash.Sum(sumBuf[:0])), nil
}
