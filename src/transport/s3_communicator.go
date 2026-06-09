package transport

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

type S3Communicator struct {
	conn       net.Conn
	reader     *bufio.Reader
	remoteAddr net.Addr

	// Client state
	clientAuthToken string
	clientTimestamp int64

	// Server state
	meta common.FileMetadata
}

func NewS3Communicator(conn net.Conn) *S3Communicator {
	return &S3Communicator{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		remoteAddr: conn.RemoteAddr(),
	}
}

func (m *S3Communicator) Read(p []byte) (n int, err error) {
	return m.reader.Read(p)
}

func (m *S3Communicator) Write(p []byte) (n int, err error) {
	return m.conn.Write(p)
}

func (m *S3Communicator) Close() error {
	return m.conn.Close()
}

func (m *S3Communicator) SetAbsoluteDeadline(t interface{}) error {
	deadline, ok := t.(time.Time)
	if !ok {
		return fmt.Errorf("invalid deadline type: expected time.Time")
	}
	return m.conn.SetDeadline(deadline)
}

func (m *S3Communicator) HandshakeClient(authToken string, timestamp int64) (int, error) {
	m.clientAuthToken = authToken
	m.clientTimestamp = timestamp

	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}

	reqStr := fmt.Sprintf("OPTIONS / HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nX-Momo-Timestamp: %d\r\n\r\n", host, authToken, timestamp)
	if _, err := m.conn.Write([]byte(reqStr)); err != nil {
		return 0, err
	}

	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	modeStr := resp.Header.Get("X-Momo-Replication-Mode")
	if modeStr == "" {
		return 4, nil // Default to ReplicationNone
	}

	// 🛡️ Zero-Crash: Defensive parsing of external headers
	mode, err := strconv.Atoi(modeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid replication mode header: %w", err)
	}
	return mode, nil
}

func (m *S3Communicator) HandshakeServer(expectedAuthToken []byte) (int, int64, error) {
	req, err := http.ReadRequest(m.reader)
	if err != nil {
		return 0, 0, err
	}

	authHeader := req.Header.Get("Authorization")
	var token string
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else if strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		// Extract Credential
		parts := strings.Split(authHeader, "Credential=")
		if len(parts) > 1 {
			credPart := strings.Split(parts[1], "/")[0]
			token = credPart
		}
	}

	tokenBuf := []byte(common.PadString(token, common.AuthTokenLength))
	if subtle.ConstantTimeCompare(tokenBuf, expectedAuthToken) != 1 {
		return 0, 0, syscall.EACCES
	}

	timestampStr := req.Header.Get("X-Momo-Timestamp")
	if timestampStr == "" {
		timestampStr = req.Header.Get("X-Amz-Date")
	}

	var timestamp int64
	if timestampStr != "" {
		// Handle Momo timestamp (int64) or Amz-Date (ISO8601)
		t, err := strconv.ParseInt(timestampStr, 10, 64)
		if err == nil {
			timestamp = t
		} else {
			parsedTime, err := time.Parse("20060102T150405Z", timestampStr)
			if err == nil {
				timestamp = parsedTime.UnixNano()
			}
		}
	}

	// Parse Metadata if it's a PUT request
	if req.Method == "PUT" {
		m.meta.Name = strings.TrimPrefix(req.URL.Path, "/")
		m.meta.Size = req.ContentLength
		m.meta.Hash = req.Header.Get("X-Amz-Content-Sha256")
		if m.meta.Hash == "" {
			m.meta.Hash = req.Header.Get("Content-SHA256") // Fallback
		}
	}

	// Wait, we need to handle OPTIONS / for handshake.
	if req.Method == "OPTIONS" {
		// We shouldn't process metadata.
		// HandshakeServer returns timestamp. The caller will then call SendReplicationMode.
	}

	return 0, timestamp, nil
}

func (m *S3Communicator) SendReplicationMode(mode int) error {
	// SendReplicationMode is called by the server after HandshakeServer.
	// Since HTTP requests expect an HTTP response, we write an HTTP response.
	resp := http.Response{
		StatusCode: 200,
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     make(http.Header),
	}
	resp.Header.Set("X-Momo-Replication-Mode", strconv.Itoa(mode))
	resp.Header.Set("Content-Length", "0")
	resp.Header.Set("Connection", "keep-alive")
	return resp.Write(m.conn)
}

func (m *S3Communicator) SendMetadata(meta *common.FileMetadata) error {
	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}

	reqStr := fmt.Sprintf("PUT /%s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nX-Momo-Timestamp: %d\r\nX-Amz-Content-Sha256: %s\r\nContent-Length: %d\r\n\r\n",
		strings.TrimRight(meta.Name, "\x00"), host, m.clientAuthToken, m.clientTimestamp, strings.TrimRight(meta.Hash, "\x00"), meta.Size)

	_, err := m.conn.Write([]byte(reqStr))
	return err
}

func (m *S3Communicator) ReceiveMetadata() (common.FileMetadata, error) {
	// If HandshakeServer already parsed the PUT request (e.g., from AWS CLI),
	// we just return it.
	// But wait! If the client used OPTIONS for handshake, then the PUT request
	// is the NEXT HTTP request on the same connection!
	// Let's read the next request if we haven't got metadata yet.
	if m.meta.Name == "" {
		req, err := http.ReadRequest(m.reader)
		if err != nil {
			return common.FileMetadata{}, fmt.Errorf("ReceiveMetadata ReadRequest failed: %w", err)
		}
		m.meta.Name = strings.TrimPrefix(req.URL.Path, "/")
		m.meta.Size = req.ContentLength
		hash := req.Header.Get("X-Amz-Content-Sha256")
		if hash == "" {
			hash = req.Header.Get("Content-SHA256")
		}
		m.meta.Hash = hash
	}
	return m.meta, nil
}

func (m *S3Communicator) SendACK(serverId int) error {
	// If the server is sending an ACK after receiving the payload.
	resp := http.Response{
		StatusCode:    200,
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        make(http.Header),
		Body:          io.NopCloser(bytes.NewBufferString(fmt.Sprintf("ACK%d", serverId))),
		ContentLength: int64(3 + len(strconv.Itoa(serverId))),
	}
	return resp.Write(m.conn)
}

func (m *S3Communicator) ReceiveACK() error {
	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 🛡️ Zero-Crash: Use LimitReader to prevent unbounded memory allocation
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if !strings.HasPrefix(string(body), "ACK") {
		return fmt.Errorf("unexpected ACK: %s", string(body))
	}
	return nil
}

func (m *S3Communicator) RemoteAddr() net.Addr {
	return m.remoteAddr
}
