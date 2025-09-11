package client

import (
	"bytes"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

func TestPadString(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		length   int
		expected string
	}{
		{"Short string", "hello", 10, "hello\x00\x00\x00\x00\x00"},
		{"Exact length", "world", 5, "world"},
		{"Longer string", "longstring", 5, "longs"},
		{"Empty string", "", 5, "\x00\x00\x00\x00\x00"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := padString(tc.input, tc.length)
			if result != tc.expected {
				t.Errorf("Expected '%s', but got '%s'", tc.expected, result)
			}
		})
	}
}

func TestSendFile(t *testing.T) {
	// Create a temporary file to send
	content := "hello world"
	tmpfile, err := ioutil.TempFile("", "testfile.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Fatal(err)
	}

	// Create a mock server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	var serverWg sync.WaitGroup
	serverWg.Add(1)

	go func() {
		defer serverWg.Done()
		conn, err := listener.Accept()
		if err != nil {
			t.Errorf("Server accept error: %v", err)
			return
		}
		defer conn.Close()

		// Read metadata
		md5Buffer := make([]byte, md5Length)
		if _, err := conn.Read(md5Buffer); err != nil {
			t.Errorf("Server could not read md5: %v", err)
			return
		}

		nameBuffer := make([]byte, momo_common.LENGTHINFO)
		if _, err := conn.Read(nameBuffer); err != nil {
			t.Errorf("Server could not read name: %v", err)
			return
		}

		sizeBuffer := make([]byte, momo_common.LENGTHINFO)
		if _, err := conn.Read(sizeBuffer); err != nil {
			t.Errorf("Server could not read size: %v", err)
			return
		}

		// Verify metadata
		expectedHash, _ := momo_common.HashFile(tmpfile.Name())
		if string(bytes.TrimRight(md5Buffer, "\x00")) != expectedHash {
			t.Errorf("Expected hash '%s', but got '%s'", expectedHash, string(md5Buffer))
		}

		fileInfo, _ := os.Stat(tmpfile.Name())
		if string(bytes.TrimRight(nameBuffer, "\x00")) != fileInfo.Name() {
			t.Errorf("Expected name '%s', but got '%s'", fileInfo.Name(), string(nameBuffer))
		}

		size, _ := strconv.ParseInt(string(bytes.TrimRight(sizeBuffer, "\x00")), 10, 64)
		if size != fileInfo.Size() {
			t.Errorf("Expected size '%d', but got '%d'", fileInfo.Size(), size)
		}

		// Read file content
		fileBuffer := make([]byte, momo_common.BUFFERSIZE)
		bytesRead, err := conn.Read(fileBuffer)
		if err != nil {
			t.Errorf("Server could not read file content: %v", err)
			return
		}

		if string(fileBuffer[:bytesRead]) != content {
			t.Errorf("Expected content '%s', but got '%s'", content, string(fileBuffer[:bytesRead]))
		}

		// Send ACK
		if _, err := conn.Write([]byte("ACK")); err != nil {
			t.Errorf("Server could not write ACK: %v", err)
			return
		}
	}()

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("Client dial error: %v", err)
	}

	var clientWg sync.WaitGroup
	clientWg.Add(1)
	go sendFile(&clientWg, conn, tmpfile.Name())
	clientWg.Wait()

	serverWg.Wait()
}
