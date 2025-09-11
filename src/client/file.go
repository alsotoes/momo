package client

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"

	momo_common "github.com/alsotoes/momo/src/common"
)

const (
	md5Length = 32
	ackLength = 4
)

// sendFile sends a file over a network connection.
// It first sends metadata (MD5 hash, filename, size) and then the file content.
// It waits for an ACK from the server after sending the file.
func sendFile(wg *sync.WaitGroup, conn net.Conn, filePath string) {
	defer wg.Done()

	if err := doSendFile(conn, filePath); err != nil {
		log.Printf("Failed to send file to %s: %v", conn.RemoteAddr(), err)
	}
}

func doSendFile(conn net.Conn, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	hash, err := momo_common.HashFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to hash file: %w", err)
	}

	// Prepare metadata
	fileMD5 := padString(hash, md5Length)
	fileName := padString(fileInfo.Name(), momo_common.LENGTHINFO)
	fileSize := padString(strconv.FormatInt(fileInfo.Size(), 10), momo_common.LENGTHINFO)

	// Send metadata
	log.Printf("Sending metadata for %s", fileInfo.Name())
	if _, err := conn.Write([]byte(fileMD5)); err != nil {
		return fmt.Errorf("failed to send MD5: %w", err)
	}
	if _, err := conn.Write([]byte(fileName)); err != nil {
		return fmt.Errorf("failed to send filename: %w", err)
	}
	if _, err := conn.Write([]byte(fileSize)); err != nil {
		return fmt.Errorf("failed to send filesize: %w", err)
	}

	// Send file content
	log.Printf("Sending file content for %s", fileInfo.Name())
	sendBuffer := make([]byte, momo_common.BUFFERSIZE)
	for {
		n, err := file.Read(sendBuffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read file chunk: %w", err)
		}
		if _, err := conn.Write(sendBuffer[:n]); err != nil {
			return fmt.Errorf("failed to send file chunk: %w", err)
		}
	}

	// Wait for server ACK
	log.Printf("Waiting for ACK from server for %s", fileInfo.Name())
	bufferACK := make([]byte, ackLength)
	if _, err := conn.Read(bufferACK); err != nil {
		return fmt.Errorf("failed to read ACK: %w", err)
	}
	log.Printf("Received ACK: %s", string(bufferACK))

	log.Printf("File %s has been sent successfully.", fileInfo.Name())
	return nil
}

// padString pads a string with null characters to a specified length.
func padString(input string, length int) string {
	if len(input) >= length {
		return input[:length]
	}
	return input + string(make([]byte, length-len(input)))
}
