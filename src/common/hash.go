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
	var returnHashString string
	file, err := os.Open(filePath)
	if err != nil {
		return returnHashString, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return returnHashString, err
	}
	// ⚡ Bolt: Eliminate heap allocation by providing a stack-allocated buffer
	// to hash.Sum(), rather than letting it allocate a new slice with Sum(nil).
	var buf [sha256.Size]byte
	hashInBytes := hash.Sum(buf[:0])

	// ⚡ Bolt: Eliminate heap allocation by using a stack-allocated byte array for hex encoding.
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], hashInBytes)
	returnHashString = string(hexBuf[:])

	return returnHashString, nil
}
