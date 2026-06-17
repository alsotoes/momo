package client

import (
	"crypto/subtle"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
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
		result := common.PadString(tc.input, tc.length)
		if result != tc.expected {
			t.Errorf("Expected '%s', got '%s'", tc.expected, result)
		}
	}
}

func startMockServer(t *testing.T, authToken string, expectedMode int, delay time.Duration) (string, net.Listener) {
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

		bufAuth := make([]byte, common.AuthTokenLength)
		io.ReadFull(conn, bufAuth)
		expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
		if subtle.ConstantTimeCompare(bufAuth, expectedAuthToken) != 1 {
			t.Logf("Server: Invalid AuthToken received: %s", string(bufAuth))
			return
		}

		buf := make([]byte, common.TimestampLength+1) // Read Timestamp (19) + RequestedMode (1)
		io.ReadFull(conn, buf)
		conn.Write([]byte(strconv.Itoa(expectedMode)))

		if expectedMode == common.ReplicationPrimarySplay {
			// Splay read dummy handshake for extra nodes
		}

		// Read file metadata
		bufHash := make([]byte, 64)
		io.ReadFull(conn, bufHash)
		bufName := make([]byte, common.FileInfoLength)
		io.ReadFull(conn, bufName)
		bufSize := make([]byte, common.FileInfoLength)
		io.ReadFull(conn, bufSize)

		// Send metadata status
		conn.Write([]byte{transport.MetadataStatusSendPayload})

		// Send ACK after a small delay
		time.Sleep(delay)
		conn.Write([]byte("ACK"))
	}()
	return ln.Addr().String(), ln
}

func startDummyServer(t *testing.T, authToken string) (string, net.Listener) {
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
				bufAuth := make([]byte, common.AuthTokenLength)
				io.ReadFull(c, bufAuth)
				expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
				if subtle.ConstantTimeCompare(bufAuth, expectedAuthToken) != 1 {
					t.Logf("Dummy Server: Invalid AuthToken received: %s", string(bufAuth))
					return
				}

				buf := make([]byte, common.TimestampLength+1) // Read Timestamp (19) + RequestedMode (1)
				io.ReadFull(c, buf)
				c.Write([]byte(strconv.Itoa(common.ReplicationNone))) // Not Splay

				// Wait for metadata
				bufHash := make([]byte, 64)
				io.ReadFull(c, bufHash)
				bufName := make([]byte, common.FileInfoLength)
				io.ReadFull(c, bufName)
				bufSize := make([]byte, common.FileInfoLength)
				io.ReadFull(c, bufSize)

				// Send metadata status
				c.Write([]byte{transport.MetadataStatusSendPayload})

				c.Write([]byte("ACK"))
			}(conn)
		}
	}()
	return ln.Addr().String(), ln
}

func TestConnect(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"

	// Create a temp file to send
	file, err := os.CreateTemp("", "test_connect_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("test data")
	file.Close()

	// Normal Connect non-splay
	addr1, ln1 := startMockServer(t, authToken, common.ReplicationNone, 10*time.Millisecond)
	defer ln1.Close()

	daemons := []*common.Daemon{
		{Host: addr1, ChangeReplication: addr1, Data: "/tmp", Drive: "/dev/sda1"},
	}
	cfg := common.Configuration{
		Daemons: daemons,
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	Connect(&wg, cfg, file.Name(), 0, time.Now().UnixNano(), 0, 3)
	wg.Wait()

	// Splay Connect
	addr2, ln2 := startDummyServer(t, authToken)
	defer ln2.Close()
	addr3, ln3 := startDummyServer(t, authToken)
	defer ln3.Close()

	daemonsSplay := []*common.Daemon{
		{Host: addr1, ChangeReplication: addr1, Data: "/tmp", Drive: "/dev/sda1"},
		{Host: addr2, ChangeReplication: addr2, Data: "/tmp", Drive: "/dev/sda1"},
		{Host: addr3, ChangeReplication: addr3, Data: "/tmp", Drive: "/dev/sda1"},
	}

	// For Splay, initial server needs to return common.ReplicationPrimarySplay (3)
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

		bufAuth := make([]byte, common.AuthTokenLength)
		io.ReadFull(conn, bufAuth)
		expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
		if subtle.ConstantTimeCompare(bufAuth, expectedAuthToken) != 1 {
			t.Logf("Splay Server: Invalid AuthToken received: %s", string(bufAuth))
			return
		}

		buf := make([]byte, common.TimestampLength+1) // Read Timestamp (19) + RequestedMode (1)
		io.ReadFull(conn, buf)
		conn.Write([]byte(strconv.Itoa(common.ReplicationPrimarySplay))) // Send 3

		// Read file metadata
		bufHash := make([]byte, 64)
		io.ReadFull(conn, bufHash)
		bufName := make([]byte, common.FileInfoLength)
		io.ReadFull(conn, bufName)
		bufSize := make([]byte, common.FileInfoLength)
		io.ReadFull(conn, bufSize)

		// Send metadata status
		conn.Write([]byte{transport.MetadataStatusSendPayload})

		conn.Write([]byte("ACK"))
	}()

	daemonsSplay[0].Host = addrSplay
	cfgSplay := common.Configuration{
		Daemons: daemonsSplay,
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
		},
	}

	wg.Add(1)
	Connect(&wg, cfgSplay, file.Name(), 0, time.Now().UnixNano(), 0, 3)
	wg.Wait()
}

func TestSendFile(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"

	file, err := os.CreateTemp("", "test_sendfile_*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(file.Name())
	file.WriteString("test sendFile data")
	file.Close()

	addr, ln := startMockServer(t, authToken, 0, 10*time.Millisecond)
	defer ln.Close()

	conn, err := common.DialSocket(addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}

	// First, send the AuthToken
	conn.Write([]byte(authToken))

	// Skip the initial timestamp and requestedMode read/write
	conn.Write([]byte(common.PadString("123", common.TimestampLength)))
	conn.Write([]byte{0}) // RequestedMode
	io.ReadFull(conn, make([]byte, 1))

	var wg sync.WaitGroup
	wg.Add(1)

	fileInfo, err := os.Stat(file.Name())
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	fileHash, err := common.HashFile(file.Name())
	if err != nil {
		t.Fatalf("Failed to hash file: %v", err)
	}
	meta := &common.FileMetadata{
		Name: fileInfo.Name(),
		Hash: fileHash,
		Size: fileInfo.Size(),
	}

	comm := transport.NewMomoTCPCommunicator(conn)
	sendFile(&wg, comm, file.Name(), meta)
	wg.Wait()
}
