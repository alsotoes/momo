package transport

import (
	"bytes"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

type mockMetricsHook struct {
	downloads   atomic.Uint64
	deletes     atomic.Uint64
	bytesDL     atomic.Uint64
	replication atomic.Uint64
	errors      atomic.Uint64
}

func (m *mockMetricsHook) IncDownloads()         { m.downloads.Add(1) }
func (m *mockMetricsHook) IncDeletes()           { m.deletes.Add(1) }
func (m *mockMetricsHook) AddBytesDownloaded(n uint64) { m.bytesDL.Add(n) }
func (m *mockMetricsHook) IncReplication()       { m.replication.Add(1) }
func (m *mockMetricsHook) IncErrors()            { m.errors.Add(1) }

func runS3TestRequestWithMetrics(t *testing.T, reqStr string, mock storage.Store, hook *mockMetricsHook) string {
	expectedAuthToken := []byte(common.PadString("a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6", common.AuthTokenLength)) // notsecret

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer l.Close()

	addr := l.Addr().String()
	errChan := make(chan error, 1)

	go func() {
		conn, err := l.Accept()
		if err != nil {
			errChan <- err
			return
		}
		defer conn.Close()

		comm := NewS3Communicator(conn)
		comm.SetStore(mock)
		if hook != nil {
			comm.SetMetricsHook(hook)
		}

		_, _, err = comm.HandshakeServer(expectedAuthToken)
		errChan <- err
	}()

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("Failed to dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write([]byte(reqStr))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	buf := make([]byte, 4096)
	var resp bytes.Buffer
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			resp.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	<-errChan
	return resp.String()
}

func TestMetricsHook_DownloadIncremented(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "GET /bucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	fileContent := []byte("hello metrics download!")
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			if name != "hello.txt" {
				return nil, common.FileMetadata{}, syscall.ENOENT
			}
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name: "hello.txt",
				Size: int64(len(fileContent)),
			}, nil
		},
	}

	hook := &mockMetricsHook{}
	respStr := runS3TestRequestWithMetrics(t, reqStr, mock, hook)

	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK, got: %s", respStr)
	}
	if hook.downloads.Load() != 1 {
		t.Errorf("Expected downloads=1, got %d", hook.downloads.Load())
	}
	if hook.bytesDL.Load() != uint64(len(fileContent)) {
		t.Errorf("Expected bytesDownloaded=%d, got %d", len(fileContent), hook.bytesDL.Load())
	}
}

func TestMetricsHook_DeleteIncremented(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "DELETE /bucket/myfile.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	mock := &mockStore{
		deleteFunc: func(name string) error {
			return nil
		},
	}

	hook := &mockMetricsHook{}
	respStr := runS3TestRequestWithMetrics(t, reqStr, mock, hook)

	if !strings.Contains(respStr, "HTTP/1.1 204 No Content") {
		t.Errorf("Expected 204 No Content, got: %s", respStr)
	}
	if hook.deletes.Load() != 1 {
		t.Errorf("Expected deletes=1, got %d", hook.deletes.Load())
	}
}

func TestMetricsHook_NilHookNoPanic(t *testing.T) {
	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	reqStr := "GET /bucket/hello.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + authToken + "\r\n\r\n"

	fileContent := []byte("no hook test")
	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(bytes.NewReader(fileContent)), common.FileMetadata{
				Name: "hello.txt",
				Size: int64(len(fileContent)),
			}, nil
		},
	}

	respStr := runS3TestRequestWithMetrics(t, reqStr, mock, nil)

	if !strings.Contains(respStr, "HTTP/1.1 200 OK") {
		t.Errorf("Expected 200 OK with nil hook, got: %s", respStr)
	}
}
