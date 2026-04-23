package common

import (
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestPadString(t *testing.T) {
	testCases := []struct {
		input    string
		length   int
		expected string
	}{
		{"test", 10, "test\x00\x00\x00\x00\x00\x00"},
		{"test", 4, "test"},
		{"longstring", 5, "longs"},
	}

	for _, tc := range testCases {
		result := padString(tc.input, tc.length)
		if result != tc.expected {
			t.Errorf("Expected '%s', got '%s'", tc.expected, result)
		}
	}
}

func startMockServer(t *testing.T, expectedMode int, delay time.Duration) (string, net.Listener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, TimestampLength)
		io.ReadFull(conn, buf)
		conn.Write([]byte(strconv.Itoa(expectedMode)))

		if expectedMode == ReplicationPrimarySplay {
			// Splay read dummy handshake for extra nodes
		}

		// Read file metadata
		bufHash := make([]byte, hashLength)
		io.ReadFull(conn, bufHash)
		bufName := make([]byte, FileInfoLength)
		io.ReadFull(conn, bufName)
		bufSize := make([]byte, FileInfoLength)
		io.ReadFull(conn, bufSize)

		// Send ACK after a small delay
		time.Sleep(delay)
		conn.Write([]byte("ACK"))
	}()
	return ln.Addr().String(), ln
}

func startDummyServer(t *testing.T) (string, net.Listener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				// Just read and respond basic handshake then ACK
				buf := make([]byte, TimestampLength)
				io.ReadFull(c, buf)
				c.Write([]byte("4")) // Not Splay

				// Wait for metadata
				bufHash := make([]byte, hashLength)
				io.ReadFull(c, bufHash)
				bufName := make([]byte, FileInfoLength)
				io.ReadFull(c, bufName)
				bufSize := make([]byte, FileInfoLength)
				io.ReadFull(c, bufSize)

				c.Write([]byte("ACK"))
			}(conn)
		}
	}()
	return ln.Addr().String(), ln
}

func TestConnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	// Create a temp file to send
	file, err := os.CreateTemp("", "test_connect_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("test data")
	file.Close()

	// Normal Connect non-splay
	addr1, ln1 := startMockServer(t, ReplicationNone, 10*time.Millisecond)
	defer ln1.Close()

	daemons := []*Daemon{
		{Host: addr1, ChangeReplication: addr1, Data: "/tmp", Drive: "/dev/sda1"},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	Connect(&wg, daemons, file.Name(), 0, time.Now().UnixNano())
	wg.Wait()

	// Splay Connect
	addr2, ln2 := startDummyServer(t)
	defer ln2.Close()
	addr3, ln3 := startDummyServer(t)
	defer ln3.Close()

	daemonsSplay := []*Daemon{
		{Host: addr1, ChangeReplication: addr1, Data: "/tmp", Drive: "/dev/sda1"},
		{Host: addr2, ChangeReplication: addr2, Data: "/tmp", Drive: "/dev/sda1"},
		{Host: addr3, ChangeReplication: addr3, Data: "/tmp", Drive: "/dev/sda1"},
	}

	// For Splay, initial server needs to return ReplicationPrimarySplay (3)
	// We'll create a special listener
	lnSplay, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start listener: %v", err)
	}
	addrSplay := lnSplay.Addr().String()
	defer lnSplay.Close()

	go func() {
		conn, err := lnSplay.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, TimestampLength)
		io.ReadFull(conn, buf)
		conn.Write([]byte(strconv.Itoa(ReplicationPrimarySplay))) // Send 3

		// Read file metadata
		bufHash := make([]byte, hashLength)
		io.ReadFull(conn, bufHash)
		bufName := make([]byte, FileInfoLength)
		io.ReadFull(conn, bufName)
		bufSize := make([]byte, FileInfoLength)
		io.ReadFull(conn, bufSize)
		conn.Write([]byte("ACK"))
	}()

	daemonsSplay[0].Host = addrSplay

	wg.Add(1)
	Connect(&wg, daemonsSplay, file.Name(), 0, time.Now().UnixNano())
	wg.Wait()
}

func TestSendFile(t *testing.T) {
	defer goleak.VerifyNone(t)
	file, err := os.CreateTemp("", "test_sendfile_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("test sendFile data")
	file.Close()

	addr, ln := startMockServer(t, 0, 10*time.Millisecond)
	defer ln.Close()

	conn, err := DialSocket(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	// Skip the initial timestamp read/write
	conn.Write([]byte(padString("123", TimestampLength)))
	io.ReadFull(conn, make([]byte, 1))

	var wg sync.WaitGroup
	wg.Add(1)
	sendFile(&wg, conn, file.Name())
	wg.Wait()
}
