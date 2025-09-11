package momo

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
func handleReplicationChange(t *testing.T, connection net.Conn, wg *sync.WaitGroup) {
	defer wg.Done()
	defer connection.Close()

	bufferReplicationMode := make([]byte, momo_common.FileInfoLength)
	_, err := connection.Read(bufferReplicationMode)
	if err != nil {
		t.Logf("connection read error: %v", err)
		return
	}

	replicationJSON := momo_common.ReplicationData{}
	trimmedBytes := bytes.TrimRight(bufferReplicationMode, "\x00")
	if err := json.NewDecoder(bytes.NewReader(trimmedBytes)).Decode(&replicationJSON); err != nil {
		t.Errorf("JSON decode error: %v", err)
		return
	}

	CurrentReplicationMode = replicationJSON.New
}

// TestChangeReplicationModeServerLogic tests the internal logic of the server's connection handler.
func TestChangeReplicationModeServerLogic(t *testing.T) {
	CurrentReplicationMode = 1

	client, server := net.Pipe()

	var wg sync.WaitGroup
	wg.Add(1)
	go handleReplicationChange(t, server, &wg)

	expectedMode := 8
	data := momo_common.ReplicationData{
		New:       expectedMode,
		TimeStamp: time.Now().Unix(),
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Failed to marshal json: %v", err)
	}

	buffer := make([]byte, momo_common.FileInfoLength)
	copy(buffer, jsonBytes)

	_, err = client.Write(buffer)
	if err != nil {
		t.Fatalf("Client write failed: %v", err)
	}
	client.Close()

	wg.Wait()

	if CurrentReplicationMode != expectedMode {
		t.Errorf("Expected replication mode to be %d, but got %d", expectedMode, CurrentReplicationMode)
	}
}

// TestChangeReplicationModeClient tests the client function for replication changes.
func TestChangeReplicationModeClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()
	received := make(chan []byte, 1)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, momo_common.FileInfoLength)
		n, _ := conn.Read(buf)
		received <- buf[:n]
	}()

	daemons := []*momo_common.Daemon{
		{Chrep: serverAddr},
	}
	jsonString := `{"New":5,"TimeStamp":1662756600}`

	changeReplicationModeClient(daemons, jsonString, 0)

	data := <-received
	trimmedData := strings.TrimRight(string(data), "\x00")

	if trimmedData != jsonString {
		t.Errorf("Expected to receive '%s', but got '%s'", jsonString, trimmedData)
	}
}
