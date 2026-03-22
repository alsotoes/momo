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
	"strconv"
	"strings"

	momo_common "github.com/alsotoes/momo/src/common"
)

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

	fileHash := string(bytes.Trim(bufferFileHash, "\x00"))

	// 🛡️ Sentinel: Sanitize fileName immediately to prevent path traversal in all downstream consumers.
	rawFileName := string(bytes.Trim(bufferFileName, "\x00"))
	if rawFileName == "." || rawFileName == ".." || strings.Contains(rawFileName, "/") || strings.Contains(rawFileName, "\\") {
		return metadata, &os.PathError{Op: "getMetadata", Path: rawFileName, Err: os.ErrInvalid}
	}
	fileName := filepath.Base(rawFileName)
	if fileName == "." || fileName == ".." || fileName == "/" || fileName == "\\" {
		return metadata, &os.PathError{Op: "getMetadata", Path: fileName, Err: os.ErrInvalid}
	}

	fileSize, err := strconv.ParseInt(string(bytes.Trim(bufferFileSize, "\x00")), 10, 64)
	if err != nil {
		return metadata, err
	}

	// 🛡️ Sentinel: Enforce file size limit to prevent Denial of Service via unbound resource allocation.
	if fileSize > momo_common.MaxFileSize || fileSize < 0 {
		return metadata, fmt.Errorf("file size %d exceeds limit or is negative", fileSize)
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
	tmpPath := fullPath + ".tmp"
	newFile, err := os.Create(tmpPath)

	if err != nil {
		return err
	}

	defer func() {
		newFile.Close()
		// 🛡️ Sentinel: Clean up potentially malicious/corrupt/partial files on any error
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	// ⚡ Bolt: Compute SHA-256 hash simultaneously while writing to disk using an io.TeeReader.
	// This eliminates the need to re-read the entire file from disk just to hash it,
	// cutting disk I/O in half and significantly speeding up file processing.
	hashCalc := sha256.New()
	reader := io.TeeReader(connection, hashCalc)

	// Optimization: Use a single io.CopyN instead of manually chunking in a loop.
	// This enables the Go standard library to utilize zero-copy system calls
	// (like splice or sendfile) and reduces function call overhead.
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

	// 🛡️ Sentinel: Close file before renaming (required for Windows) and perform atomic rename
	newFile.Close()
	if err = os.Rename(tmpPath, fullPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	log.Printf("=> Expected Hash: %s", expectedHash)
	log.Printf("=> Actual Hash:   %s", hash)
	log.Printf("=> Name:          %s", fullPath)
	log.Printf("Received file completely!")
	log.Printf("Sending ACK to client connection")
	return nil
}
