// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"io"
	"log"
	"net"
	"os"
	"strconv"

	momo_common "github.com/alsotoes/momo/src/common"
)

// getMetadata reads file metadata (MD5, name, size) from a network connection.
// It reads the MD5 hash, file name, and file size from the connection, trims any null characters,
// and returns a FileMetadata struct.
// Null characters are trimmed because the buffers are fixed size, and the actual data may be smaller.
func getMetadata(connection net.Conn) momo_common.FileMetadata {
	var metadata momo_common.FileMetadata

	bufferFileMD5 := make([]byte, 32)
	bufferFileName := make([]byte, momo_common.FileInfoLength)
	bufferFileSize := make([]byte, momo_common.FileInfoLength)

	connection.Read(bufferFileMD5)
	fileMD5 := string(bytes.Trim(bufferFileMD5, "\x00"))

	connection.Read(bufferFileName)
	fileName := string(bytes.Trim(bufferFileName, "\x00"))

	connection.Read(bufferFileSize)
	fileSize, _ := strconv.ParseInt(string(bytes.Trim(bufferFileSize, "\x00")), 10, momo_common.FileInfoLength)

	metadata.Name = fileName
	metadata.MD5 = fileMD5
	metadata.Size = fileSize

	return metadata
}

// getFile reads a file from a network connection and saves it to a specified path.
// It creates a new file at the given path and copies the file content from the connection in chunks.
// After the transfer is complete, it calculates the MD5 hash of the received file and compares it with the expected hash.
// It logs the progress and the result of the MD5 check.
func getFile(connection net.Conn, path string, fileName string, fileMD5 string, fileSize int64) {
	newFile, err := os.Create(path + fileName)

	if err != nil {
		log.Printf(err.Error())
		connection.Close()
		os.Exit(1)
	}

	// TODO: Check on error inside this procedure for posible exit with the connection open :(
	// https://stackoverflow.com/questions/12741386/how-to-know-tcp-connection-is-closed-in-golang-net-package
	// defer connection.Close()
	defer newFile.Close()
	var receivedBytes int64

	for {
		// If the remaining bytes are less than the buffer size, copy the exact number of bytes.
		if (fileSize - receivedBytes) < momo_common.TCPSocketBufferSize {
			if (fileSize - receivedBytes) != 0 {
				io.CopyN(newFile, connection, (fileSize - receivedBytes))
				// Read and discard any remaining bytes in the buffer.
				connection.Read(make([]byte, (receivedBytes+momo_common.TCPSocketBufferSize)-fileSize))
			}
			break
		}
		io.CopyN(newFile, connection, momo_common.TCPSocketBufferSize)
		receivedBytes += momo_common.TCPSocketBufferSize
	}

	hash, err := momo_common.HashFile(path + fileName)
	if err != nil {
		log.Printf(err.Error())
		connection.Close()
		os.Exit(1)
	}

	log.Printf("=> MD5:     " + fileMD5)
	log.Printf("=> New MD5: " + hash)
	log.Printf("=> Name:    " + path + fileName)
	log.Printf("Received file completely!")
	log.Printf("Sending ACK to client connection")
}
