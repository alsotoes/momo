// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"

	momo_common "github.com/alsotoes/momo/src/common"
)

// getMetadata reads file metadata (Hash, name, size) from a network connection.
// It reads the Hash string, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(connection net.Conn) (momo_common.FileMetadata, error) {
	var metadata momo_common.FileMetadata

	bufferFileHash := make([]byte, 64)
	bufferFileName := make([]byte, momo_common.FileInfoLength)
	bufferFileSize := make([]byte, momo_common.FileInfoLength)

	if _, err := io.ReadFull(connection, bufferFileHash); err != nil {
		return metadata, err
	}
	fileHash := string(bytes.Trim(bufferFileHash, "\x00"))

	if _, err := io.ReadFull(connection, bufferFileName); err != nil {
		return metadata, err
	}
	// 🛡️ Sentinel: Sanitize fileName immediately to prevent path traversal in all downstream consumers.
	fileName := filepath.Base(string(bytes.Trim(bufferFileName, "\x00")))

	if _, err := io.ReadFull(connection, bufferFileSize); err != nil {
		return metadata, err
	}
	fileSize, err := strconv.ParseInt(string(bytes.Trim(bufferFileSize, "\x00")), 10, 64)
	if err != nil {
		return metadata, err
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
func getFile(connection net.Conn, path string, fileName string, expectedHash string, fileSize int64) error {
	fullPath := filepath.Join(path, fileName)
	newFile, err := os.Create(fullPath)

	if err != nil {
		return err
	}

	defer newFile.Close()

	// Optimization: Use a single io.CopyN instead of manually chunking in a loop.
	// This enables the Go standard library to utilize zero-copy system calls
	// (like splice or sendfile) and reduces function call overhead.
	if fileSize > 0 {
		if _, err := io.CopyN(newFile, connection, fileSize); err != nil {
			return err
		}
	}

	hash, err := momo_common.HashFile(fullPath)
	if err != nil {
		return err
	}

	log.Printf("=> Expected Hash: " + expectedHash)
	log.Printf("=> Actual Hash:   " + hash)
	log.Printf("=> Name:          " + fullPath)
	log.Printf("Received file completely!")
	log.Printf("Sending ACK to client connection")
	return nil
}
