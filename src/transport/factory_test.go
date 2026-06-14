package transport

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestProtocolFactory_Listen_TCP(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol: "momo-tcp",
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	if _, ok := l.(*TCPListener); !ok {
		t.Errorf("Expected *TCPListener, got %T", l)
	}
}

func TestProtocolFactory_Listen_QUIC(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol: "momo-quic",
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	if _, ok := l.(*QUICListener); !ok {
		t.Errorf("Expected *QUICListener, got %T", l)
	}
}

func TestProtocolFactory_Dial_TCP_Error(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol: "momo-tcp",
		},
	}
	factory := NewProtocolFactory(cfg)

	_, err := factory.Dial("127.0.0.1:1") // Should fail to connect
	if err == nil {
		t.Error("Expected error when dialing invalid address, got nil")
	}
}

func TestMomoQUICCommunicator_Handshake(t *testing.T) {
	authToken := "test-token"
	timestamp := time.Now().UnixNano()
	addr := "127.0.0.1:45684"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-quic",
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()

	errChan := make(chan error, 1)
	go func() {
		comm, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer comm.Close()

		_, _, err = comm.HandshakeServer([]byte(common.PadString(authToken, common.AuthTokenLength)))
		if err != nil {
			errChan <- err
			return
		}

		if tc, ok := comm.(*MomoQUICCommunicator); ok {
			if err := tc.SendReplicationMode(1); err != nil {
				errChan <- err
				return
			}
		}
		errChan <- nil
	}()

	clientComm, err := factory.Dial(addr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()

	mode, err := clientComm.HandshakeClient(authToken, timestamp, 0)
	if err != nil {
		t.Fatalf("Client handshake failed: %v", err)
	}

	if mode != 1 {
		t.Errorf("Expected mode 1, got %d", mode)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Server error: %v", err)
	}
}

func TestMomoQUICCommunicator_Metadata_And_Payload(t *testing.T) {
	authToken := "test-token"
	addr := "127.0.0.1:45685"
	testMeta := &common.FileMetadata{
		Name: "quic-test.txt",
		Hash: "abc123hash",
		Size: 10,
	}
	testPayload := []byte("quicdata12")

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken: authToken,
			Protocol:  "momo-quic",
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()

	errChan := make(chan error, 1)
	go func() {
		comm, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer comm.Close()

		_, _, err = comm.HandshakeServer([]byte(common.PadString(authToken, common.AuthTokenLength)))
		if err != nil {
			errChan <- err
			return
		}

		if tc, ok := comm.(*MomoQUICCommunicator); ok {
			tc.SendReplicationMode(0)
		}

		receivedMeta, err := comm.ReceiveMetadata()
		if err != nil {
			errChan <- err
			return
		}

		if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
			errChan <- err
			return
		}

		if receivedMeta.Name != testMeta.Name || receivedMeta.Hash != testMeta.Hash || receivedMeta.Size != testMeta.Size {
			errChan <- fmt.Errorf("metadata mismatch")
			return
		}

		buf := make([]byte, testMeta.Size)
		if _, err := io.ReadFull(comm, buf); err != nil {
			errChan <- err
			return
		}

		if string(buf) != string(testPayload) {
			errChan <- fmt.Errorf("payload mismatch")
			return
		}

		if err := comm.SendACK(0); err != nil {
			errChan <- err
			return
		}

		errChan <- nil
	}()

	clientComm, err := factory.Dial(addr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()

	if _, err := clientComm.HandshakeClient(authToken, 0, 0); err != nil {
		t.Fatalf("Client handshake failed: %v", err)
	}

	status, err := clientComm.SendMetadata(testMeta)
	if err != nil {
		t.Fatalf("Client failed to send metadata: %v", err)
	}
	if status != MetadataStatusSendPayload {
		t.Fatalf("Expected status %d, got %d", MetadataStatusSendPayload, status)
	}

	if _, err := clientComm.Write(testPayload); err != nil {
		t.Fatalf("Client failed to send payload: %v", err)
	}

	if err := clientComm.ReceiveACK(); err != nil {
		t.Fatalf("Client failed to receive ACK: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("Server error: %v", err)
	}
}
