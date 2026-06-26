package transport

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path"
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 Read: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	return m.reader.Read(p)
}

func (m *S3Communicator) Write(p []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 Write: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	return m.conn.Write(p)
}

func (m *S3Communicator) Close() (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 Close: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	return m.conn.Close()
}

func (m *S3Communicator) SetAbsoluteDeadline(t interface{}) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SetAbsoluteDeadline: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	deadline, ok := t.(time.Time)
	if !ok {
		return fmt.Errorf("invalid deadline type: expected time.Time")
	}
	return m.conn.SetDeadline(deadline)
}

func (m *S3Communicator) HandshakeClient(authToken string, timestamp int64, requestedMode int) (finalMode int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 HandshakeClient: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()

	m.clientAuthToken = authToken
	m.clientTimestamp = timestamp

	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}

	// ⚡ Bolt: Eliminate fmt.Sprintf and string allocations using byte slice appending
	b := make([]byte, 0, 256)
	b = append(b, "OPTIONS / HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nAuthorization: Bearer "...)
	b = append(b, authToken...)
	b = append(b, "\r\nX-Momo-Timestamp: "...)
	b = strconv.AppendInt(b, timestamp, 10)
	b = append(b, "\r\nX-Momo-Requested-Mode: "...)
	b = strconv.AppendInt(b, int64(requestedMode), 10)
	b = append(b, "\r\n\r\n"...)

	if _, err := m.conn.Write(b); err != nil {
		return 0, err
	}

	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	modeStr := resp.Header.Get("X-Momo-Replication-Mode")
	if modeStr == "" {
		// 🛡️ Rule 10: Map missing protocol headers to syscall.EBADMSG for consistent propagation.
		return 0, fmt.Errorf("missing replication mode header: %w", syscall.EBADMSG)
	}

	// 🛡️ Zero-Crash: Defensive parsing of external headers
	finalMode, err = strconv.Atoi(modeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid replication mode header: %s: %w", modeStr, syscall.EBADMSG)
	}
	return finalMode, nil
}

func (m *S3Communicator) HandshakeServer(expectedAuthToken []byte) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 HandshakeServer: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()

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

	if timestampStr != "" {
		// Handle Momo timestamp (int64) or Amz-Date (ISO8601)
		t, err := strconv.ParseInt(timestampStr, 10, 64)
		if err == nil {
			timestamp = t
		} else {
			parsedTime, err := time.Parse("20060102T150405Z", timestampStr)
			if err == nil {
				timestamp = parsedTime.UnixNano()
			} else {
				return 0, 0, fmt.Errorf("invalid timestamp header: %s: %w", timestampStr, syscall.EBADMSG)
			}
		}
	}

	requestedModeStr := req.Header.Get("X-Momo-Requested-Mode")
	requestedMode = 0
	if requestedModeStr != "" {
		requestedMode, err = strconv.Atoi(requestedModeStr)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid requested mode header: %s: %w", requestedModeStr, syscall.EBADMSG)
		}
	}

	// Parse Metadata if it's a PUT request
	if req.Method == "PUT" {
		// 🛡️ Sentinel: Sanitize S3 path to prevent traversal attacks.
		rawPath := req.URL.Path
		cleanPath := path.Clean(rawPath)
		if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || cleanPath == "/" {
			return 0, 0, fmt.Errorf("invalid S3 path: %s: %w", rawPath, syscall.EBADMSG)
		}

		m.meta.Name = strings.TrimPrefix(cleanPath, "/")
		m.meta.Size = req.ContentLength
		m.meta.Hash = req.Header.Get("X-Amz-Content-Sha256")
		if m.meta.Hash == "" {
			m.meta.Hash = req.Header.Get("Content-SHA256") // Fallback
		}
	}

	return requestedMode, timestamp, nil
}

func (m *S3Communicator) SendReplicationMode(mode int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendReplicationMode: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// ⚡ Bolt: Eliminate http.Response and header map allocations via direct byte response writing
	b := make([]byte, 0, 128)
	b = append(b, "HTTP/1.1 200 OK\r\nX-Momo-Replication-Mode: "...)
	b = strconv.AppendInt(b, int64(mode), 10)
	b = append(b, "\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n"...)

	_, err = m.conn.Write(b)
	return err
}

func (m *S3Communicator) SendMetadata(meta *common.FileMetadata) (status int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendMetadata: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}

	// ⚡ Bolt: Eliminate fmt.Sprintf and string allocations using byte slice appending
	b := make([]byte, 0, 256)
	b = append(b, "PUT /"...)
	b = append(b, strings.TrimRight(meta.Name, "\x00")...)
	b = append(b, " HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nAuthorization: Bearer "...)
	b = append(b, m.clientAuthToken...)
	b = append(b, "\r\nX-Momo-Timestamp: "...)
	b = strconv.AppendInt(b, m.clientTimestamp, 10)
	b = append(b, "\r\nX-Amz-Content-Sha256: "...)
	b = append(b, strings.TrimRight(meta.Hash, "\x00")...)
	b = append(b, "\r\nContent-Length: "...)
	b = strconv.AppendInt(b, meta.Size, 10)
	b = append(b, "\r\n\r\n"...)

	if _, err := m.conn.Write(b); err != nil {
		return 0, err
	}

	// ⚡ Bolt: Read the response immediately to get the metadata status.
	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to read metadata status response: %w", err)
	}
	defer resp.Body.Close()

	statusStr := resp.Header.Get("X-Momo-Metadata-Status")
	if statusStr == "" {
		return MetadataStatusSendPayload, nil
	}
	statusVal, _ := strconv.Atoi(statusStr)
	return statusVal, nil
}

func (m *S3Communicator) ReceiveMetadata() (meta common.FileMetadata, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 ReceiveMetadata: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()

	// If HandshakeServer already parsed the PUT request (e.g., from AWS CLI),
	// we just return it.
	// But wait! If the client used OPTIONS for handshake, then the PUT request
	// is the NEXT HTTP request on the same connection!
	// Let's read the next request if we haven't got metadata yet.
	if m.meta.Name == "" {
		req, err := http.ReadRequest(m.reader)
		if err != nil {
			return common.FileMetadata{}, fmt.Errorf("ReceiveMetadata ReadRequest failed: %v: %w", err, syscall.EBADMSG)
		}

		// 🛡️ Sentinel: Sanitize S3 path to prevent traversal attacks.
		rawPath := req.URL.Path
		cleanPath := path.Clean(rawPath)
		if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || cleanPath == "/" {
			return common.FileMetadata{}, fmt.Errorf("invalid S3 path: %s: %w", rawPath, syscall.EBADMSG)
		}

		m.meta.Name = strings.TrimPrefix(cleanPath, "/")
		m.meta.Size = req.ContentLength
		hash := req.Header.Get("X-Amz-Content-Sha256")
		if hash == "" {
			hash = req.Header.Get("Content-SHA256")
		}
		m.meta.Hash = hash
	}
	return m.meta, nil
}

func (m *S3Communicator) SendMetadataStatus(status int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendMetadataStatus: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// ⚡ Bolt: Eliminate http.Response and header map allocations via direct byte response writing
	b := make([]byte, 0, 128)
	b = append(b, "HTTP/1.1 200 OK\r\nX-Momo-Metadata-Status: "...)
	b = strconv.AppendInt(b, int64(status), 10)
	b = append(b, "\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n"...)

	_, err = m.conn.Write(b)
	return err
}

func (m *S3Communicator) SendACK(serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendACK: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// ⚡ Bolt: Eliminate http.Response allocation and fmt.Sprintf using stack buffer direct write
	b := make([]byte, 0, 128)
	b = append(b, "HTTP/1.1 200 OK\r\nContent-Length: "...)

	// serverId string length calculation
	idStr := strconv.Itoa(serverId)
	bodyLength := 3 + len(idStr)

	b = strconv.AppendInt(b, int64(bodyLength), 10)
	b = append(b, "\r\nConnection: keep-alive\r\n\r\nACK"...)
	b = append(b, idStr...)

	_, err = m.conn.Write(b)
	return err
}

func (m *S3Communicator) ReceiveACK() (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 ReceiveACK: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// 🛡️ Zero-Crash: Use LimitReader to prevent unbounded memory allocation
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return fmt.Errorf("failed to read ACK body: %w", err)
	}
	if !bytes.HasPrefix(body, []byte("ACK")) {
		return fmt.Errorf("unexpected ACK: %s: %w", string(body), syscall.EBADMSG)
	}
	return nil
}

func (m *S3Communicator) RemoteAddr() net.Addr {
	return m.remoteAddr
}
