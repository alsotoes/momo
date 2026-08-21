package transport

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"go.uber.org/goleak"
)

func TestProtocolFactory_Listen_S3TCP_TLSEnforcement(t *testing.T) {
	t.Run("s3-tcp without TLS rejected by default", func(t *testing.T) {
		cfg := common.Configuration{
			Global: common.ConfigurationGlobal{
				Protocol: "s3-tcp",
			},
		}
		factory := NewProtocolFactory(cfg)
		_, err := factory.Listen("127.0.0.1:0")
		if err == nil {
			t.Fatalf("s3-tcp without TLS must be rejected unless tls_insecure=true")
		}
		if !errors.Is(err, syscall.EINVAL) {
			t.Errorf("expected EINVAL, got: %v", err)
		}
	})

	t.Run("s3-tcp with TLSInsecure accepted", func(t *testing.T) {
		cfg := common.Configuration{
			Global: common.ConfigurationGlobal{
				Protocol:    "s3-tcp",
				TLSInsecure: true,
			},
		}
		factory := NewProtocolFactory(cfg)
		l, err := factory.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatalf("s3-tcp with tls_insecure must be accepted, got: %v", err)
		}
		defer l.Close()
		if _, ok := l.(*TCPListener); !ok {
			t.Errorf("Expected *TCPListener, got %T", l)
		}
	})

	t.Run("momo-tcp without TLS still accepted", func(t *testing.T) {
		cfg := common.Configuration{
			Global: common.ConfigurationGlobal{
				Protocol: "momo-tcp",
			},
		}
		factory := NewProtocolFactory(cfg)
		l, err := factory.Listen("127.0.0.1:0")
		if err != nil {
			t.Fatalf("momo-tcp without TLS must remain accepted, got: %v", err)
		}
		defer l.Close()
	})
}

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
	authToken := "test-token" // notsecret
	timestamp := time.Now().UnixNano()
	addr := "127.0.0.1:45684"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:   authToken,
			Protocol:    "momo-quic",
			TLSInsecure: true,
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
	authToken := "test-token" // notsecret
	addr := "127.0.0.1:45685"
	testMeta := &common.FileMetadata{
		Name: "quic-test.txt",
		Hash: "abc123hash",
		Size: 10,
	}
	testPayload := []byte("quicdata12")

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:   authToken,
			Protocol:    "momo-quic",
			TLSInsecure: true,
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

	if _, err := clientComm.HandshakeClient(authToken, time.Now().UnixNano(), 0); err != nil {
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

func TestMomoQUICCommunicator_RejectsEmptyHash(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).runSendQueue"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Transport).listen"),
		goleak.IgnoreAnyFunction("github.com/quic-go/quic-go.(*Conn).run"),
	)

	authToken := "test-token" // notsecret
	addr := "127.0.0.1:0"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:   authToken,
			Protocol:    "momo-quic",
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()

	actualAddr := l.Addr().String()
	errChan := make(chan error, 1)
	serverOK := make(chan bool, 1)

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

		_, err = comm.ReceiveMetadata()
		if err == nil {
			errChan <- fmt.Errorf("expected ReceiveMetadata to reject empty hash")
			return
		}
		if !errors.Is(err, syscall.EBADMSG) {
			errChan <- fmt.Errorf("expected EBADMSG for empty hash, got: %v", err)
			return
		}

		// Send a status so the client's SendMetadata returns cleanly.
		if err := comm.SendMetadataStatus(MetadataStatusSendPayload); err != nil {
			errChan <- err
			return
		}

		serverOK <- true
	}()

	clientComm, err := factory.Dial(actualAddr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()

	if _, err := clientComm.HandshakeClient(authToken, time.Now().UnixNano(), 0); err != nil {
		t.Fatalf("Client handshake failed: %v", err)
	}

	// Send metadata with an empty hash — must be rejected by the server.
	emptyHashMeta := &common.FileMetadata{
		Name: "empty-hash.txt",
		Hash: "",
		Size: 5,
	}
	_, err = clientComm.SendMetadata(emptyHashMeta)
	if err != nil {
		t.Fatalf("SendMetadata should not fail client-side: %v", err)
	}

	select {
	case <-serverOK:
	case err := <-errChan:
		t.Fatalf("Server error: %v", err)
	}
}

func TestMomoQUICCommunicator_EdgeCases(t *testing.T) {
	// 1. Panic recovery tests (Rule 4) via nil communicator
	var nilComm *MomoQUICCommunicator

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

func runNativeQUICTest(t *testing.T, requestedMode int, clientFn func(Communicator), mock *mockStore, usePeerToken bool) {
	authToken := "test-token" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	addr := "127.0.0.1:0"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:   authToken,
			Protocol:    "momo-quic",
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()

	actualAddr := l.Addr().String()
	errChan := make(chan error, 1)

	// Server side
	go func() {
		comm, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer comm.Close()

		if s3Comm, ok := comm.(interface{ SetStore(storage.Store) }); ok {
			s3Comm.SetStore(mock)
		}

		_, _, err = comm.HandshakeServer(expectedAuthToken)
		errChan <- err
	}()

	// Client side
	clientComm, err := factory.Dial(actualAddr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()

	// Write Handshake manually to bypass HandshakeClient's ACK expectation
	tokenToSend := authToken
	if usePeerToken {
		tokenToSend = string(common.DerivePeerToken(expectedAuthToken))
	}
	var handshakeBuf [common.AuthTokenLength + common.TimestampLength + 1]byte
	copy(handshakeBuf[0:common.AuthTokenLength], common.PadString(tokenToSend, common.AuthTokenLength))
	copy(handshakeBuf[common.AuthTokenLength:], common.PadString(strconv.FormatInt(time.Now().UnixNano(), 10), common.TimestampLength))
	handshakeBuf[common.AuthTokenLength+common.TimestampLength] = byte(requestedMode)

	if _, err := clientComm.Write(handshakeBuf[:]); err != nil {
		t.Fatalf("Failed to write client handshake: %v", err)
	}

	clientFn(clientComm)

	serverErr := <-errChan
	if serverErr != ErrRequestHandled {
		t.Fatalf("Server expected ErrRequestHandled, got: %v", serverErr)
	}
}

func TestMomoQUICCommunicator_NativeList(t *testing.T) {
	mock := &mockStore{
		listFunc: func() ([]common.FileMetadata, error) {
			return []common.FileMetadata{
				{Name: "native-quic-file.txt", Hash: "quichash456", Size: 800},
			}, nil
		},
	}

	clientFn := func(comm Communicator) {
		// Read 4-byte big-endian file count
		var fileCount int32
		if err := binary.Read(comm, binary.BigEndian, &fileCount); err != nil {
			t.Fatalf("Failed to read file count: %v", err)
		}
		if fileCount != 1 {
			t.Fatalf("Expected file count 1, got %d", fileCount)
		}

		// Read 192-byte file metadata packet
		var packet [192]byte
		if _, err := io.ReadFull(comm, packet[:]); err != nil {
			t.Fatalf("Failed to read metadata packet: %v", err)
		}
		hash := common.TrimNullBytesString(packet[0:64])
		name := common.TrimNullBytesString(packet[64:128])
		sizeStr := common.TrimNullBytesString(packet[128:192])

		if hash != "quichash456" {
			t.Errorf("Expected hash 'quichash456', got %q", hash)
		}
		if name != "native-quic-file.txt" {
			t.Errorf("Expected name 'native-quic-file.txt', got %q", name)
		}
		if sizeStr != "800" {
			t.Errorf("Expected size '800', got %q", sizeStr)
		}
	}

	runNativeQUICTest(t, common.ModeList, clientFn, mock, true)
}

func TestMomoQUICCommunicator_NativeListDeniedForClient(t *testing.T) {
	mock := &mockStore{
		listFunc: func() ([]common.FileMetadata, error) {
			return []common.FileMetadata{
				{Name: "secret-quic-file.txt", Hash: "secret-quic-hash", Size: 999},
			}, nil
		},
	}

	clientFn := func(comm Communicator) {
		var fileCount int32
		if err := binary.Read(comm, binary.BigEndian, &fileCount); err != nil {
			t.Fatalf("Failed to read file count: %v", err)
		}
		if fileCount != 0 {
			t.Fatalf("Expected file count 0 for non-peer client, got %d", fileCount)
		}
	}

	runNativeQUICTest(t, common.ModeList, clientFn, mock, false)
}

func TestMomoQUICCommunicator_NativeDelete(t *testing.T) {
	deletedKey := ""
	fileHash := common.HashBytes([]byte("delete-me-quic-content"))
	mock := &mockStore{
		deleteFunc: func(name string) error {
			deletedKey = name
			return nil
		},
		getHashForNameFunc: func(name string) (string, error) {
			if name != "target-to-delete-quic.txt" {
				return "", syscall.ENOENT
			}
			return fileHash, nil
		},
	}

	clientFn := func(comm Communicator) {
		// Write target delete file (64 bytes padded) + content hash (64 bytes padded)
		target := common.PadString("target-to-delete-quic.txt", 64)
		hash := common.PadString(fileHash, 64)
		if _, err := comm.Write([]byte(target + hash)); err != nil {
			t.Fatalf("Failed to write target: %v", err)
		}

		// Read 1-byte status
		var resp [1]byte
		if _, err := io.ReadFull(comm, resp[:]); err != nil {
			t.Fatalf("Failed to read status: %v", err)
		}
		if resp[0] != '0' {
			t.Errorf("Expected status '0' (success), got %q", resp[0])
		}
	}

	runNativeQUICTest(t, common.ModeDelete, clientFn, mock, false)

	if deletedKey != "target-to-delete-quic.txt" {
		t.Errorf("Expected store.Delete to be called with 'target-to-delete-quic.txt', got %q", deletedKey)
	}
}

func TestMomoQUICCommunicator_NativeGet(t *testing.T) {
	fileContent := []byte("download native quic payload!")
	fileHash := common.HashBytes(fileContent)
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "native-quic-get.txt" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name: "native-quic-get.txt",
				Hash: fileHash,
				Size: int64(len(fileContent)),
			}, nil
		},
		getHashForNameFunc: func(name string) (string, error) {
			if name != "native-quic-get.txt" {
				return "", syscall.ENOENT
			}
			return fileHash, nil
		},
	}

	clientFn := func(comm Communicator) {
		// Write target get file (64 bytes padded) + content hash (64 bytes padded)
		target := common.PadString("native-quic-get.txt", 64)
		hash := common.PadString(fileHash, 64)
		if _, err := comm.Write([]byte(target + hash)); err != nil {
			t.Fatalf("Failed to write target: %v", err)
		}

		// Read 1-byte status + 64-byte size
		var respBuf [65]byte
		if _, err := io.ReadFull(comm, respBuf[:]); err != nil {
			t.Fatalf("Failed to read header: %v", err)
		}
		if respBuf[0] != '0' {
			t.Fatalf("Expected status '0' (success), got %q", respBuf[0])
		}

		sizeStr := common.TrimNullBytesString(respBuf[1:65])
		size, _ := strconv.ParseInt(sizeStr, 10, 64)
		if size != int64(len(fileContent)) {
			t.Errorf("Expected size %d, got %d", len(fileContent), size)
		}

		// Read payload
		payloadBuf := make([]byte, size)
		if _, err := io.ReadFull(comm, payloadBuf); err != nil {
			t.Fatalf("Failed to read payload: %v", err)
		}
		if string(payloadBuf) != "download native quic payload!" {
			t.Errorf("Expected content 'download native quic payload!', got %q", string(payloadBuf))
		}
	}

	runNativeQUICTest(t, common.ModeGet, clientFn, mock, false)
}

// TestMomoQUICCommunicator_CloseIdempotent verifies that Close() on a real
// QUIC connection is safe to call multiple times (issue #623). The underlying
// connection teardown must run exactly once under a sync.Once guard, without
// spawning a fire-and-forget goroutine per call.
func TestMomoQUICCommunicator_CloseIdempotent(t *testing.T) {
	addr := "127.0.0.1:45697"

	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			Protocol:    "momo-quic",
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen(addr)
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()

	serverDone := make(chan error, 1)
	go func() {
		comm, err := l.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer comm.Close()
		if _, _, err := comm.HandshakeServer([]byte(common.PadString("test-token", common.AuthTokenLength))); err != nil {
			serverDone <- err
			return
		}
		if err := comm.SendReplicationMode(1); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	clientComm, err := factory.Dial(addr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}

	mode, err := clientComm.HandshakeClient("test-token", time.Now().UnixNano(), 0)
	if err != nil {
		t.Fatalf("Client handshake failed: %v", err)
	}
	if mode != 1 {
		t.Fatalf("Expected replication mode 1, got %d", mode)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("Server handshake failed: %v", err)
	}

	// Client side double-close: the first call tears the connection down
	// (synchronously, with the grace period); the second must be a no-op that
	// returns immediately without spawning another teardown.
	if err := clientComm.Close(); err != nil {
		t.Errorf("First client Close failed: %v", err)
	}
	start := time.Now()
	if err := clientComm.Close(); err != nil {
		t.Errorf("Second client Close should be idempotent, got error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > quicCloseGracePeriod {
		t.Errorf("Second Close took %v, expected no-op (grace period %v)", elapsed, quicCloseGracePeriod)
	}
}

func TestMomoQUICCommunicator_IdleTimeoutApplied(t *testing.T) {
	cfg := common.Configuration{
		Global: common.ConfigurationGlobal{
			AuthToken:   "test-token", // notsecret
			Protocol:    "momo-quic",
			TLSInsecure: true,
		},
	}
	factory := NewProtocolFactory(cfg)

	l, err := factory.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("Server failed to listen: %v", err)
	}
	defer l.Close()
	addr := l.Addr().String()

	accepted := make(chan *MomoQUICCommunicator, 1)
	acceptErr := make(chan error, 1)
	go func() {
		comm, err := l.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		if tc, ok := comm.(*MomoQUICCommunicator); ok {
			// Drain the client's probe byte so the QUIC handshake completes and
			// the connection is fully established before we test the deadline.
			probe := make([]byte, 1)
			if _, err := tc.Read(probe); err != nil {
				comm.Close()
				acceptErr <- err
				return
			}
			accepted <- tc
			return
		}
		comm.Close()
		acceptErr <- fmt.Errorf("expected *MomoQUICCommunicator, got %T", comm)
	}()

	clientComm, err := factory.Dial(addr)
	if err != nil {
		t.Fatalf("Client failed to dial: %v", err)
	}
	defer clientComm.Close()
	// A single write forces the QUIC handshake packets to flow so the server's
	// Accept/read can complete on an otherwise idle connection.
	if _, err := clientComm.Write([]byte{1}); err != nil {
		t.Fatalf("Client probe write failed: %v", err)
	}

	var tc *MomoQUICCommunicator
	select {
	case tc = <-accepted:
	case err := <-acceptErr:
		t.Fatalf("Server accept failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server-side accept")
	}
	defer tc.Close()

	if tc.timeoutConn == nil {
		t.Fatal("momo-quic communicator must wrap its stream with an IdleTimeoutConn")
	}

	// A read with no incoming data must honor an absolute deadline set through
	// the IdleTimeoutConn rather than blocking for the full rolling 30s window.
	if err := tc.SetAbsoluteDeadline(time.Now().Add(150 * time.Millisecond)); err != nil {
		t.Fatalf("SetAbsoluteDeadline failed: %v", err)
	}
	buf := make([]byte, 1)
	start := time.Now()
	_, rerr := tc.Read(buf)
	if rerr == nil {
		t.Fatal("expected an error when reading from an idle connection past its deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("idle read took %v, expected the deadline to fire well under the 30s rolling timeout", elapsed)
	}
}
