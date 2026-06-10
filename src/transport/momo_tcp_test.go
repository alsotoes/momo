package transport

import (
	"net"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestMomoTCPCommunicator_Handshake_And_Metadata(t *testing.T) {
	authToken := "test-token-1234567890123456789012345678901234567890123456789012345"
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	addr := "127.0.0.1:45699"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:  "momo-tcp",
			AuthToken: authToken,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	timestamp := time.Now().UnixNano()

	// Server side
	go func() {
		comm, err := l.Accept()
		if err != nil {
			return
		}
		defer comm.Close()

		_, ts, err := comm.HandshakeServer(expectedAuthToken)
		if err != nil {
			t.Errorf("HandshakeServer failed: %v", err)
			return
		}
		if ts != timestamp {
			t.Errorf("HandshakeServer timestamp mismatch: got %v, want %v", ts, timestamp)
		}

		if err := comm.SendReplicationMode(1); err != nil {
			t.Errorf("SendReplicationMode failed: %v", err)
		}

		meta, err := comm.ReceiveMetadata()
		if err != nil {
			t.Errorf("ReceiveMetadata failed: %v", err)
		}
		if meta.Name != "test.txt" {
			t.Errorf("Metadata name mismatch: got %q, want %q", meta.Name, "test.txt")
		}

		if err := comm.SendACK(0); err != nil {
			t.Errorf("SendACK failed: %v", err)
		}
	}()

	// Client side
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	comm := NewMomoTCPCommunicator(conn)
	defer comm.Close()

	mode, err := comm.HandshakeClient(authToken, timestamp)
	if err != nil {
		t.Fatalf("HandshakeClient failed: %v", err)
	}
	if mode != 1 {
		t.Errorf("HandshakeClient mode mismatch: got %v, want 1", mode)
	}

	testMeta := &common.FileMetadata{
		Name: "test.txt",
		Hash: "hash123",
		Size: 100,
	}
	if err := comm.SendMetadata(testMeta); err != nil {
		t.Fatalf("SendMetadata failed: %v", err)
	}

	if err := comm.ReceiveACK(); err != nil {
		t.Fatalf("ReceiveACK failed: %v", err)
	}
}

func TestMomoTCPCommunicator_Deadline(t *testing.T) {
	conn, _ := net.Pipe()
	defer conn.Close()
	comm := NewMomoTCPCommunicator(conn)
	
	err := comm.SetAbsoluteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Errorf("SetAbsoluteDeadline failed: %v", err)
	}
}
