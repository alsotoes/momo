package transport

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"syscall"
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
		if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
			t.Errorf("SendMetadataStatus failed: %v", err)
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

	mode, err := comm.HandshakeClient(authToken, timestamp, 0)
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
	if _, err := comm.SendMetadata(testMeta); err != nil {
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

func TestMomoTCPCommunicator_EdgeCases(t *testing.T) {
	// 1. Panic recovery tests (Rule 4) via nil communicator
	var nilComm *MomoTCPCommunicator
	
	_, _, err := nilComm.HandshakeServer([]byte("token"))
	if err == nil {
		t.Errorf("Expected HandshakeServer on nilComm to fail")
	}

	_, err = nilComm.HandshakeClient("token", 12345, 1)
	if err == nil {
		t.Errorf("Expected HandshakeClient on nilComm to fail")
	}

	err = nilComm.SendReplicationMode(1)
	if err == nil {
		t.Errorf("Expected SendReplicationMode on nilComm to fail")
	}

	_, err = nilComm.SendMetadata(&common.FileMetadata{})
	if err == nil {
		t.Errorf("Expected SendMetadata on nilComm to fail")
	}

	_, err = nilComm.ReceiveMetadata()
	if err == nil {
		t.Errorf("Expected ReceiveMetadata on nilComm to fail")
	}

	err = nilComm.SendMetadataStatus(1)
	if err == nil {
		t.Errorf("Expected SendMetadataStatus on nilComm to fail")
	}

	err = nilComm.SendACK(0)
	if err == nil {
		t.Errorf("Expected SendACK on nilComm to fail")
	}

	err = nilComm.ReceiveACK()
	if err == nil {
		t.Errorf("Expected ReceiveACK on nilComm to fail")
	}
}

func runNativeTCPTest(t *testing.T, requestedMode int, clientFn func(net.Conn), mock *mockStore) {
	authToken := "test-token-1234567890123456789012345678901234567890123456789012345"
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	addr := "127.0.0.1:0"

	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	actualAddr := l.Addr().String()
	errChan := make(chan error, 1)

	// Server side
	go func() {
		conn, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		comm := NewMomoTCPCommunicator(conn)
		comm.SetStore(mock)

		_, _, err = comm.HandshakeServer(expectedAuthToken)
		errChan <- err
	}()

	// Client side
	conn, err := net.Dial("tcp", actualAddr)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Write Handshake
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(authToken, common.AuthTokenLength))
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString("1557906926566451195", common.TimestampLength))
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(requestedMode)

	if _, err := conn.Write(handshakeBuf[:]); err != nil {
		t.Fatalf("Failed to write client handshake: %v", err)
	}

	clientFn(conn)

	serverErr := <-errChan
	if serverErr != ErrRequestHandled {
		t.Fatalf("Server expected ErrRequestHandled, got: %v", serverErr)
	}
}

func TestMomoTCPCommunicator_NativeList(t *testing.T) {
	mock := &mockStore{
		listFunc: func() ([]common.FileMetadata, error) {
			return []common.FileMetadata{
				{Name: "native-file.txt", Hash: "hash456", Size: 700},
			}, nil
		},
	}

	clientFn := func(conn net.Conn) {
		// Read 4-byte big-endian file count
		var fileCount int32
		if err := binary.Read(conn, binary.BigEndian, &fileCount); err != nil {
			t.Fatalf("Failed to read file count: %v", err)
		}
		if fileCount != 1 {
			t.Fatalf("Expected file count 1, got %d", fileCount)
		}

		// Read 192-byte file metadata packet
		var packet [192]byte
		if _, err := io.ReadFull(conn, packet[:]); err != nil {
			t.Fatalf("Failed to read metadata packet: %v", err)
		}
		hash := string(bytes.TrimRight(packet[0:64], "\x00"))
		name := string(bytes.TrimRight(packet[64:128], "\x00"))
		sizeStr := string(bytes.TrimRight(packet[128:192], "\x00"))

		if hash != "hash456" {
			t.Errorf("Expected hash 'hash456', got %q", hash)
		}
		if name != "native-file.txt" {
			t.Errorf("Expected name 'native-file.txt', got %q", name)
		}
		if sizeStr != "700" {
			t.Errorf("Expected size '700', got %q", sizeStr)
		}
	}

	runNativeTCPTest(t, common.ModeList, clientFn, mock)
}

func TestMomoTCPCommunicator_NativeDelete(t *testing.T) {
	deletedKey := ""
	mock := &mockStore{
		deleteFunc: func(name string) error {
			deletedKey = name
			return nil
		},
	}

	clientFn := func(conn net.Conn) {
		// Write target delete file (64 bytes padded)
		target := common.PadString("target-to-delete.txt", 64)
		if _, err := conn.Write([]byte(target)); err != nil {
			t.Fatalf("Failed to write target: %v", err)
		}

		// Read 1-byte status
		var resp [1]byte
		if _, err := io.ReadFull(conn, resp[:]); err != nil {
			t.Fatalf("Failed to read status: %v", err)
		}
		if resp[0] != '0' {
			t.Errorf("Expected status '0' (success), got %q", resp[0])
		}
	}

	runNativeTCPTest(t, common.ModeDelete, clientFn, mock)

	if deletedKey != "target-to-delete.txt" {
		t.Errorf("Expected store.Delete to be called with 'target-to-delete.txt', got %q", deletedKey)
	}
}

func TestMomoTCPCommunicator_NativeGet(t *testing.T) {
	fileContent := []byte("download native payload!")
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "native-get.txt" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name: "native-get.txt",
				Size: int64(len(fileContent)),
			}, nil
		},
	}

	clientFn := func(conn net.Conn) {
		// Write target get file (64 bytes padded)
		target := common.PadString("native-get.txt", 64)
		if _, err := conn.Write([]byte(target)); err != nil {
			t.Fatalf("Failed to write target: %v", err)
		}

		// Read 1-byte status + 64-byte size
		var respBuf [65]byte
		if _, err := io.ReadFull(conn, respBuf[:]); err != nil {
			t.Fatalf("Failed to read header: %v", err)
		}
		if respBuf[0] != '0' {
			t.Fatalf("Expected status '0' (success), got %q", respBuf[0])
		}

		sizeStr := string(bytes.TrimRight(respBuf[1:65], "\x00"))
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		if size != int64(len(fileContent)) {
			t.Errorf("Expected size %d, got %d", len(fileContent), size)
		}

		// Read payload
		payloadBuf := make([]byte, size)
		if _, err := io.ReadFull(conn, payloadBuf); err != nil {
			t.Fatalf("Failed to read payload: %v", err)
		}
		if string(payloadBuf) != "download native payload!" {
			t.Errorf("Expected content 'download native payload!', got %q", string(payloadBuf))
		}
	}

	runNativeTCPTest(t, common.ModeGet, clientFn, mock)
}
