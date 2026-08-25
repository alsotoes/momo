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
	var buf [sha256.Size]byte
	hashInBytes := hash.Sum(buf[:0])

	// ⚡ Bolt: Eliminate heap allocation by using a stack-allocated byte array for hex encoding.
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], hashInBytes)
	returnHashString = string(hexBuf[:])

	return returnHashString, nil
}

// HashBytes calculates the SHA-256 hash of a byte slice and returns it as a hex-encoded string.
func HashBytes(data []byte) string {
	hash := sha256.Sum256(data)
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], hash[:])
	return string(hexBuf[:])
}

// HashReader calculates the SHA-256 hash of a stream and returns it as a
// hex-encoded string. It streams through a fixed-size buffer (bounded memory),
// mirroring HashFile. The reader is fully consumed.
func HashReader(r io.Reader) (string, error) {
	h := sha256.New()
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	var sumBuf [sha256.Size]byte
	sum := h.Sum(sumBuf[:0])
	var hexBuf [sha256.Size * 2]byte
	hex.Encode(hexBuf[:], sum)
	return string(hexBuf[:]), nil
}
