package transport

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestS3Communicator_FullFlow(t *testing.T) {
	authToken := "test-token-1234567890123456789012345678901234567890123456789012345"
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	addr := "127.0.0.1:45698"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:  "s3-tcp",
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
	testPayload := []byte("s3 test payload")

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
			t.Errorf("HandshakeServer timestamp mismatch")
		}

		if err := comm.SendReplicationMode(1); err != nil {
			t.Errorf("SendReplicationMode failed: %v", err)
		}

		meta, err := comm.ReceiveMetadata()
		if err != nil {
			t.Errorf("ReceiveMetadata failed: %v", err)
		}
		if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
			t.Errorf("SendMetadataStatus failed: %v", err)
		}
		if meta.Name != "test-s3.txt" {
			t.Errorf("Metadata name mismatch: got %q", meta.Name)
		}

		buf := make([]byte, len(testPayload))
		if _, err := io.ReadFull(comm, buf); err != nil {
			t.Errorf("Failed to read payload: %v", err)
		}

		if string(buf) != string(testPayload) {
			t.Errorf("Payload mismatch: got %q", buf)
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
	comm := NewS3Communicator(conn)
	defer comm.Close()

	mode, err := comm.HandshakeClient(authToken, timestamp, 0)
	if err != nil {
		t.Fatalf("HandshakeClient failed: %v", err)
	}
	if mode != 1 {
		t.Errorf("HandshakeClient mode mismatch")
	}

	testMeta := &common.FileMetadata{
		Name: "test-s3.txt",
		Hash: "s3hash",
		Size: int64(len(testPayload)),
	}
	status, err := comm.SendMetadata(testMeta)
	if err != nil {
		t.Fatalf("SendMetadata failed: %v", err)
	}
	if status != MetadataStatusSendPayload {
		t.Errorf("Expected status %d, got %d", MetadataStatusSendPayload, status)
	}

	if _, err := comm.Write(testPayload); err != nil {
		t.Fatalf("Failed to write payload: %v", err)
	}

	if err := comm.ReceiveACK(); err != nil {
		t.Fatalf("ReceiveACK failed: %v", err)
	}
}

func TestS3Communicator_Methods(t *testing.T) {
	conn, _ := net.Pipe()
	defer conn.Close()
	comm := NewS3Communicator(conn)

	if addr := comm.RemoteAddr(); addr == nil {
		t.Errorf("RemoteAddr returned nil")
	}

	err := comm.SetAbsoluteDeadline(time.Now().Add(time.Second))
	if err != nil {
		t.Errorf("SetAbsoluteDeadline failed: %v", err)
	}
}
