// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	momo_common "github.com/alsotoes/momo/src/common"
)

// ⚡ Bolt: Fast parsing of null-padded integers without string allocation.
func parsePaddedIntFast(b []byte) (int64, error) {
	if len(b) == 0 || b[0] == 0 {
		return 0, fmt.Errorf("empty input")
	}
	var res int64
	for _, c := range b {
		if c == 0 {
			break
		}
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid character '%c' in integer", c)
		}
		// simple overflow check for positive int64
		if res > (1<<63-1)/10 || (res == (1<<63-1)/10 && int64(c-'0') > (1<<63-1)%10) {
			return 0, fmt.Errorf("overflow")
		}
		res = res*10 + int64(c-'0')
	}
	return res, nil
}

// getMetadata reads file metadata (Hash, name, size) from a network connection.
// It reads the Hash string, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(connection net.Conn) (momo_common.FileMetadata, error) {
	var metadata momo_common.FileMetadata

	// ⚡ Bolt: Use a single buffer and single io.ReadFull call to reduce system calls and allocations.
	buffer := make([]byte, 64+momo_common.FileInfoLength+momo_common.FileInfoLength)

	if _, err := io.ReadFull(connection, buffer); err != nil {
		return metadata, err
	}

	// Extract the sub-slices from the main buffer
	bufferFileHash := buffer[:64]
	bufferFileName := buffer[64 : 64+momo_common.FileInfoLength]
	bufferFileSize := buffer[64+momo_common.FileInfoLength:]

	// ⚡ Bolt: Use bytes.IndexByte to find the first null character for faster trimming.
	trimNull := func(b []byte) string {
		if i := bytes.IndexByte(b, 0); i != -1 {
			return string(b[:i])
		}
		return string(b)
	}

	fileHash := trimNull(bufferFileHash)

	// 🛡️ Sentinel: Sanitize fileName immediately to prevent path traversal in all downstream consumers.
	rawFileName := trimNull(bufferFileName)
	if rawFileName == "." || rawFileName == ".." || strings.Contains(rawFileName, "/") || strings.Contains(rawFileName, "\\") {
		return metadata, &os.PathError{Op: "getMetadata", Path: rawFileName, Err: os.ErrInvalid}
	}
	fileName := filepath.Base(rawFileName)
	if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
		return metadata, &os.PathError{Op: "getMetadata", Path: fileName, Err: os.ErrInvalid}
	}

	// ⚡ Bolt: Parse integer directly from pre-allocated buffer padding.
	fileSize, err := parsePaddedIntFast(bufferFileSize)
	if err != nil {
		return metadata, err
	}

	// 🛡️ Sentinel: Enforce maximum file size to prevent Denial of Service via resource exhaustion
	if fileSize < 0 || fileSize > momo_common.MaxFileSize {
		return metadata, fmt.Errorf("invalid file size: %d (max: %d)", fileSize, momo_common.MaxFileSize)
	}

	metadata.Name = fileName
	metadata.Hash = fileHash
	metadata.Size = fileSize

	return metadata, nil
}

// getFile reads a file from a network connection and saves it to a specified path.
// It creates a new file at the given path and copies the file content from the connection in chunks.
// After the transfer is complete, it calculates the SHA-256 hash of the received file and compares it with the expected hash.
// It logs the progress and the result of the hash check.
func getFile(connection net.Conn, path string, fileName string, expectedHash string, fileSize int64) (err error) {
	fullPath := filepath.Join(path, fileName)
	// 🛡️ Sentinel: Use os.CreateTemp for secure, unpredictable temporary file creation.
	newFile, err := os.CreateTemp(path, fileName+"-*.tmp")

	if err != nil {
		return err
	}
	tempPath := newFile.Name()

	defer func() {
		newFile.Close()
		// 🛡️ Sentinel: Clean up the temporary file on any error.
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// ⚡ Bolt: Compute SHA-256 hash simultaneously while writing to disk using an io.TeeReader.
	hashCalc := sha256.New()
	reader := io.TeeReader(connection, hashCalc)

	// Optimization: Use a single io.CopyN instead of manually chunking in a loop.
	if fileSize > 0 {
		if _, copyErr := io.CopyN(newFile, reader, fileSize); copyErr != nil {
			err = copyErr
			return err
		}
	}

	hash := hex.EncodeToString(hashCalc.Sum(nil))

	if hash != expectedHash {
		// 🛡️ Sentinel: Reject files with mismatched hashes to prevent integrity check bypass
		err = fmt.Errorf("file hash mismatch: expected %s, got %s", expectedHash, hash)
		return err
	}

	// 🛡️ Sentinel: Atomically rename the temporary file to the final destination after all checks pass.
	newFile.Close()
	if err = os.Rename(tempPath, fullPath); err != nil {
		return err
	}

	log.Printf("=> Expected Hash: %s", expectedHash)
	log.Printf("=> Actual Hash:   %s", hash)
	log.Printf("=> Name:          %s", fullPath)
	log.Printf("Received file completely!")
	log.Printf("Sending ACK to client connection")
	return nil
}
