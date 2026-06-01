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
	"syscall"

	momo_common "github.com/alsotoes/momo/src/common"
)

// getMetadata reads file metadata (Hash, name, size) from a network connection.
// It reads the Hash string, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(connection net.Conn) (momo_common.FileMetadata, error) {
	var metadata momo_common.FileMetadata

	// ⚡ Bolt: Use a single stack-allocated buffer and single io.ReadFull call to reduce system calls and eliminate heap allocations.
	var buffer [64 + momo_common.FileInfoLength + momo_common.FileInfoLength]byte

	if _, err := io.ReadFull(connection, buffer[:]); err != nil {
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

	// ⚡ Bolt: Use a stack-allocated buffer for file transfer to reduce heap allocations.
	var copyBuf [32 * 1024]byte
	if fileSize > 0 {
		if _, copyErr := io.CopyBuffer(newFile, io.LimitReader(reader, fileSize), copyBuf[:]); copyErr != nil {
			err = copyErr
			return err
		}
	}

	// ⚡ Bolt: Pre-allocate a fixed-size array on the stack and pass a zero-length slice of it
	// to hashCalc.Sum() to eliminate the heap allocation and improve performance.
	var sumBuf [sha256.Size]byte
	hash := hex.EncodeToString(hashCalc.Sum(sumBuf[:0]))

	if hash != expectedHash {
		// 🛡️ Sentinel: Reject files with mismatched hashes to prevent integrity check bypass
		// ⚡ Bolt: Return syscall.EBADMSG to indicate data corruption, as requested in issue #27.
		err = fmt.Errorf("file hash mismatch: expected %s, got %s: %w", expectedHash, hash, syscall.EBADMSG)
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

// parsePaddedIntFast parses a null-padded or null-terminated byte slice into an int64
// without allocating an intermediate string.
func parsePaddedIntFast(b []byte) (int64, error) {
	idx := bytes.IndexByte(b, 0)
	if idx == -1 {
		idx = len(b)
	}

	if idx == 0 {
		return 0, strconv.ErrSyntax
	}

	var res uint64
	var sign int64 = 1
	start := 0

	if b[0] == '-' {
		sign = -1
		start = 1
		if idx == 1 {
			return 0, strconv.ErrSyntax
		}
	} else if b[0] == '+' {
		start = 1
		if idx == 1 {
			return 0, strconv.ErrSyntax
		}
	}

	var cutoff uint64 = (1<<63 - 1)
	if sign == -1 {
		cutoff = 1 << 63
	}

	maxVal := cutoff / 10

	for i := start; i < idx; i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, strconv.ErrSyntax
		}

		v := uint64(c - '0')

		// overflow check for int64
		if res > maxVal {
			return 0, strconv.ErrRange
		}
		if res == maxVal && v > cutoff%10 {
			return 0, strconv.ErrRange
		}

		res = res*10 + v
	}

	if sign == -1 {
		return int64(^res + 1), nil
	}

	return int64(res), nil
}
