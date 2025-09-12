package common

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os"
)

// HashFile calculates the MD5 hash of a file.
// It takes the file path as input and returns the MD5 hash as a hex-encoded string.
func HashFile(filePath string) (string, error) {
	var returnMD5String string
	file, err := os.Open(filePath)
	if err != nil {
		return returnMD5String, err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return returnMD5String, err
	}
	hashInBytes := hash.Sum(nil)[:16]
	returnMD5String = hex.EncodeToString(hashInBytes)
	return returnMD5String, nil
}
