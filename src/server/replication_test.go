// Package server provides the core functionality for the momo server.
package server

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/transport"
)

// handleReplicationChange is a testable version of the connection handling logic inside ChangeReplicationModeServer.
// It reads replication data from a connection and updates the global CurrentReplicationMode.
func handleReplicationChange(t *testing.T, authToken string, connection net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	defer connection.Close()

	bufferAuthToken := make([]byte, common.AuthTokenLength)
	if _, err := io.ReadFull(connection, bufferAuthToken); err != nil {
		t.Logf("Error reading AuthToken: %v", err)
		return
	}
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	if subtle.ConstantTimeCompare(bufferAuthToken, expectedAuthToken) != 1 {
		t.Logf("Invalid AuthToken received")
		return
	}

	bufferReplicationMode := make([]byte, 256)
	_, err := connection.Read(bufferReplicationMode)
	if err != nil {
		t.Logf("connection read error: %v", err) // Log as info, as pipe closure can cause an expected EOF.
		return
	}

	replicationJSON := common.ReplicationData{}
	// Trim null bytes before decoding
	idx := bytes.IndexByte(bufferReplicationMode, 0)
	var trimmedBytes []byte
	if idx != -1 {
		trimmedBytes = bufferReplicationMode[:idx]
	} else {
		trimmedBytes = bufferReplicationMode
	}
	if err := json.NewDecoder(bytes.NewReader(trimmedBytes)).Decode(&replicationJSON); err != nil {

		t.Errorf("JSON decode error: %v", err)
		return
	}

	SetReplicationState(replicationJSON.New, replicationJSON.TimeStamp)
}

// TestChangeReplicationModeServerLogic verifies that the server correctly
// updates its replication mode based on data from a client connection.
func TestChangeReplicationModeServerLogic(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// Arrange: Set initial state and create a network pipe to simulate a client-server connection.
	SetReplicationState(common.ReplicationNone, 0) // Initial mode
	client, server := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)
	go handleReplicationChange(t, authToken, server, &wg)

	// Act: Marshal and send the new replication data from the client side of the pipe.
	client.Write([]byte(authToken))
	expectedMode := common.ReplicationSplay
	data := common.ReplicationData{
		New:       expectedMode,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal json: %v", err)
	}

	// Copy to a fixed-size buffer to simulate the network read.
	buffer := make([]byte, common.FileInfoLength)
	copy(buffer, jsonBytes)

	_, err = client.Write(buffer)
	if err != nil {
		t.Fatalf("Client write failed: %v", err)
	}
	client.Close() // Close the client side to signal end of data.

	wg.Wait() // Wait for the server-side handler to finish.

	// Assert: Check if the replication mode was updated correctly.
	currentMode := GetCurrentReplicationMode()
	if currentMode != expectedMode {
		t.Errorf("Expected replication mode to be %d, but got %d", expectedMode, currentMode)
	}
}

// TestChangeReplicationModeClient verifies that the client function correctly sends the
// replication mode JSON payload to a listening server.
func TestChangeReplicationModeClient(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	// Arrange: Set up a listener to act as a mock server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	receivedAuth := make(chan []byte, 1)
	receivedJSON := make(chan []byte, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return // Exit goroutine on listener close.
		}
		defer conn.Close()

		// Read AuthToken, Timestamp, and RequestedMode (64 + 19 + 1 = 84 bytes)
		var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
		io.ReadFull(conn, handshakeBuf[:])
		receivedAuth <- handshakeBuf[:common.AuthTokenLength]

		// Send back a dummy replication mode to complete handshake
		conn.Write([]byte("0"))

		bufJSON := make([]byte, common.FileInfoLength)
		n, _ := conn.Read(bufJSON)
		receivedJSON <- bufJSON[:n] // Send received data to the channel.

		// ⚡ Bolt: Send "OK" ACK as required by ChangeReplicationModeClient
		conn.Write([]byte("OK"))
	}()

	// Act: Call the function under test.
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{ChangeReplication: serverAddr}, // Configure the daemon to connect to our mock server.
		},
	}
	factory := transport.NewProtocolFactory(cfg)
	jsonString := `{"New":5,"TimeStamp":1662756600}`

	ChangeReplicationModeClient(factory, []byte(jsonString), 0)

	// Assert: Verify the mock server received the correct data.
	select {
	case auth := <-receivedAuth:
		expectedPeerToken := common.DerivePeerToken([]byte(common.PadString(authToken, common.AuthTokenLength)))
		if subtle.ConstantTimeCompare(auth, expectedPeerToken) != 1 {
			t.Errorf("Expected peer token (derived from '%s'), but got '%s'", authToken, string(auth))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out, no AuthToken received by the server.")
	}

	select {
	case data := <-receivedJSON:
		trimmedData := strings.TrimRight(string(data), "\x00\n")
		if trimmedData != jsonString {
			t.Errorf("Expected to receive '%s', but got '%s'", jsonString, trimmedData)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Test timed out, no data received by the server.")
	}
}

// TestGetCurrentReplicationMode verifies that GetCurrentReplicationMode
// returns the expected current replication mode.
func TestGetCurrentReplicationMode(t *testing.T) {
	// Arrange
	expectedMode := common.ReplicationChain
	timestamp := time.Now().Unix()

	// Set the replication state to a known value
	SetReplicationState(expectedMode, timestamp)

	// Act
	currentMode := GetCurrentReplicationMode()

	// Assert
	if currentMode != expectedMode {
		t.Errorf("Expected replication mode %d, got %d", expectedMode, currentMode)
	}
}

// recordingPool satisfies payloadPoolc and records the capacity of every
// buffer handed back to the pool, so tests can assert only fixed-capacity
// buffers are ever returned.
type recordingPool struct {
	putCaps []int
}

func (p *recordingPool) Get() interface{} { return make([]byte, payloadPoolCapacity) }

func (p *recordingPool) Put(b interface{}) { p.putCaps = append(p.putCaps, cap(b.([]byte))) }

// TestReleasePayloadOnlyReturnsFixedCapacity verifies that buffers are only
// returned to payloadPool when their capacity is still the pool's fixed size
// (fix #667). A payload that grew past 1024 bytes must not put an oversized
// slice back into the pool.
func TestReleasePayloadOnlyReturnsFixedCapacity(t *testing.T) {
	orig := payloadPool
	rp := &recordingPool{}
	payloadPool = rp
	defer func() { payloadPool = orig }()

	// A grown buffer (append reallocated past the fixed capacity) must not be
	// returned to the pool.
	buf := payloadPool.Get().([]byte)
	grown := append(buf, []byte(strings.Repeat("x", payloadPoolCapacity*2))...)
	if cap(grown) <= payloadPoolCapacity {
		t.Fatalf("test setup: expected grown buffer to exceed capacity, got cap=%d", cap(grown))
	}
	releasePayload(grown)

	// A fixed-capacity buffer must be returned normally.
	releasePayload(buf[:0])

	if len(rp.putCaps) != 1 {
		t.Fatalf("expected exactly 1 Put of a fixed-capacity buffer, got %d puts (caps=%v)", len(rp.putCaps), rp.putCaps)
	}
	if rp.putCaps[0] != payloadPoolCapacity {
		t.Errorf("expected Put cap=%d (fixed), got cap=%d", payloadPoolCapacity, rp.putCaps[0])
	}

	// ChangeReplicationModeClient must use the guarded release path: exercising
	// it with both a small and an oversized payload must never put an oversized
	// buffer (the fixed allocation can still be returned).
	rp.putCaps = nil

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()
	serverAddr := listener.Addr().String()

	acceptOnce := func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
		io.ReadFull(conn, handshakeBuf[:])
		conn.Write([]byte("0"))
		// Read the payload, then ACK so the client returns promptly.
		var payloadBuf [payloadPoolCapacity * 3]byte
		conn.Read(payloadBuf[:])
		conn.Write([]byte("OK"))
	}

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-tcp",
		},
		Daemons: []*common.Daemon{
			{ChangeReplication: serverAddr},
		},
	}
	factory := transport.NewProtocolFactory(cfg)

	// Small payload: fits in the fixed buffer, must be returned to the pool.
	go acceptOnce()
	ChangeReplicationModeClient(factory, []byte(`{"New":1,"TimeStamp":1}`), 0)

	// Oversized payload: exceeds the fixed buffer, must NOT be pooled.
	bigJSON := `{"New":2,"TimeStamp":1,"payload":"` + strings.Repeat("x", payloadPoolCapacity*2) + `"}`
	go acceptOnce()
	ChangeReplicationModeClient(factory, []byte(bigJSON), 0)

	for i, c := range rp.putCaps {
		if c != payloadPoolCapacity {
			t.Errorf("client put cap=%d (idx %d), want fixed %d", c, i, payloadPoolCapacity)
		}
	}
}

// TestAcquireConnectionSlot_ContextCancel verifies issue #659: acquiring a
// semaphore slot must not block forever when the context is canceled and the
// semaphore is full.
func TestAcquireConnectionSlot_ContextCancel(t *testing.T) {
	sem := make(chan struct{}, 1)
	sem <- struct{}{} // fill the single slot

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan bool, 1)
	go func() { done <- acquireConnectionSlot(ctx, sem) }()

	select {
	case ok := <-done:
		if ok {
			t.Fatal("expected acquireConnectionSlot to return false on canceled context")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquireConnectionSlot blocked forever despite canceled context")
	}
}

// TestAcquireConnectionSlot_Success verifies a free slot is acquired and
// released correctly.
func TestAcquireConnectionSlot_Success(t *testing.T) {
	sem := make(chan struct{}, 1)
	ok := acquireConnectionSlot(context.Background(), sem)
	if !ok {
		t.Fatal("expected slot to be acquired when the semaphore is empty")
	}
	// Release so the test doesn't leak the slot.
	select {
	case <-sem:
	default:
		t.Fatal("expected acquired slot to be releasable")
	}
}
