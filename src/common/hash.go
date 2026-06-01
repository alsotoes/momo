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
	hashInBytes := hash.Sum(nil)
	returnHashString = hex.EncodeToString(hashInBytes)
	return returnHashString, nil
}
