// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	momo_common "github.com/alsotoes/momo/src/common"
)

// handleReplicationChange is a testable version of the connection handling logic inside ChangeReplicationModeServer.
// It reads replication data from a connection and updates the global CurrentReplicationMode.
func handleReplicationChange(t *testing.T, connection net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	defer connection.Close()

	bufferReplicationMode := make([]byte, momo_common.FileInfoLength)
	_, err := connection.Read(bufferReplicationMode)
	if err != nil {
		t.Logf("connection read error: %v", err) // Log as info, as pipe closure can cause an expected EOF.
		return
	}

	replicationJSON := momo_common.ReplicationData{}
	// Trim null bytes before decoding
	trimmedBytes := bytes.TrimRight(bufferReplicationMode, "\x00")
	if err := json.NewDecoder(bytes.NewReader(trimmedBytes)).Decode(&replicationJSON); err != nil {
		t.Errorf("JSON decode error: %v", err)
		return
	}

	CurrentReplicationMode = replicationJSON.New
}

// TestChangeReplicationModeServerLogic verifies that the server correctly
// updates its replication mode based on data from a client connection.
func TestChangeReplicationModeServerLogic(t *testing.T) {
	// Arrange: Set initial state and create a network pipe to simulate a client-server connection.
	CurrentReplicationMode = momo_common.ReplicationNone // Initial mode
	client, server := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)
	go handleReplicationChange(t, server, &wg)

	// Act: Marshal and send the new replication data from the client side of the pipe.
	expectedMode := momo_common.ReplicationSplay
	data := momo_common.ReplicationData{
		New:       expectedMode,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal json: %v", err)
	}

	// Copy to a fixed-size buffer to simulate the network read.
	buffer := make([]byte, momo_common.FileInfoLength)
	copy(buffer, jsonBytes)

	_, err = client.Write(buffer)
	if err != nil {
		t.Fatalf("Client write failed: %v", err)
	}
	client.Close() // Close the client side to signal end of data.

	wg.Wait() // Wait for the server-side handler to finish.

	// Assert: Check if the replication mode was updated correctly.
	if CurrentReplicationMode != expectedMode {
		t.Errorf("Expected replication mode to be %d, but got %d", expectedMode, CurrentReplicationMode)
	}
}

// TestChangeReplicationModeClient verifies that the client function correctly sends the
// replication mode JSON payload to a listening server.
func TestChangeReplicationModeClient(t *testing.T) {
	// Arrange: Set up a listener to act as a mock server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	received := make(chan []byte, 1) // Channel to receive data from the mock server.

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return // Exit goroutine on listener close.
		}
		defer conn.Close()

		buf := make([]byte, momo_common.FileInfoLength)
		n, _ := conn.Read(buf)
		received <- buf[:n] // Send received data to the channel.
	}()

	// Act: Call the function under test.
	daemons := []*momo_common.Daemon{
		{ChangeReplication: serverAddr}, // Configure the daemon to connect to our mock server.
	}
	jsonString := `{"New":5,"TimeStamp":1662756600}`

	changeReplicationModeClient(daemons, jsonString, 0)

	// Assert: Verify the mock server received the correct data.
	select {
	case data := <-received:
		trimmedData := strings.TrimRight(string(data), "\x00")
		if trimmedData != jsonString {
			t.Errorf("Expected to receive '%s', but got '%s'", jsonString, trimmedData)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out, no data received by the server.")
	}
}
