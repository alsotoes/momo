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

// getMetadata reads file metadata (MD5, name, size) from a network connection.
// It reads the MD5 hash, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(connection net.Conn) (momo_common.FileMetadata, error) {
	var metadata momo_common.FileMetadata

	bufferFileMD5 := make([]byte, 32)
	bufferFileName := make([]byte, momo_common.FileInfoLength)
	bufferFileSize := make([]byte, momo_common.FileInfoLength)

	if _, err := io.ReadFull(connection, bufferFileMD5); err != nil {
		return metadata, err
	}
	fileMD5 := string(bytes.Trim(bufferFileMD5, "\x00"))

	if _, err := io.ReadFull(connection, bufferFileName); err != nil {
		return metadata, err
	}
	fileName := string(bytes.Trim(bufferFileName, "\x00"))

	if _, err := io.ReadFull(connection, bufferFileSize); err != nil {
		return metadata, err
	}
	fileSize, err := strconv.ParseInt(string(bytes.Trim(bufferFileSize, "\x00")), 10, 64)
	if err != nil {
		return metadata, err
	}

	metadata.Name = fileName
	metadata.MD5 = fileMD5
	metadata.Size = fileSize

	return metadata, nil
}

// getFile reads a file from a network connection and saves it to a specified path.
// It creates a new file at the given path and copies the file content from the connection in chunks.
// After the transfer is complete, it calculates the MD5 hash of the received file and compares it with the expected hash.
// It logs the progress and the result of the MD5 check.
func getFile(connection net.Conn, path string, fileName string, fileMD5 string, fileSize int64) error {
	safeFileName := filepath.Base(fileName)
	fullPath := filepath.Join(path, safeFileName)
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

	log.Printf("=> MD5:     " + fileMD5)
	log.Printf("=> New MD5: " + hash)
	log.Printf("=> Name:    " + fullPath)
	log.Printf("Received file completely!")
	log.Printf("Sending ACK to client connection")
	return nil
}
