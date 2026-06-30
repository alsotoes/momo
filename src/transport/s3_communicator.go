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
	"os"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
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

	// Storage store for list, get, and delete operations
	store storage.Store
}

func NewS3Communicator(conn net.Conn) *S3Communicator {
	return &S3Communicator{
		conn:       conn,
		reader:     bufio.NewReader(conn),
		remoteAddr: conn.RemoteAddr(),
	}
}

func (m *S3Communicator) SetStore(store storage.Store) {
	m.store = store
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

	// ⚡ Bolt: Eliminate fmt.Sprintf and string allocations using stack-allocated buffer
	var buf [256]byte
	b := buf[:0]
	b = append(b, "OPTIONS / HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nAuthorization: Bearer "...)
	b = append(b, authToken...)
	b = append(b, "\r\nX-Momo-Timestamp: "...)
	b = strconv.AppendInt(b, timestamp, 10)
	b = append(b, "\r\nX-Momo-Requested-Mode: "...)
	b = strconv.AppendInt(b, int64(requestedMode), 10)
	b = append(b, "\r\n\r\n"...)

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 256 {
		return 0, fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	// 🛡️ Zero-Crash: Set a short write deadline to prevent stalled socket hanging
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})

	if _, err := m.conn.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write handshake request: %v: %w", err, syscall.EPIPE)
	}

	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to read handshake response: %v: %w", err, syscall.EBADMSG)
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
		return 0, 0, fmt.Errorf("failed to read handshake request: %v: %w", err, syscall.EBADMSG)
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
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		m.conn.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		return 0, 0, syscall.EACCES
	}

	// 🛡️ Sentinel: Reject requests containing directory traversal characters (".." or "\") to prevent path traversal attacks.
	if strings.Contains(req.URL.Path, "..") || strings.Contains(req.URL.Path, "\\") {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		m.conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		return 0, 0, fmt.Errorf("invalid key path traversal: %s: %w", req.URL.Path, syscall.EBADMSG)
	}

	bucket, key := extractS3BucketAndKey(req)

	// Intercept GET requests (for ListObjectsV2 or GetObject)
	if req.Method == "GET" {
		if m.store == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, fmt.Errorf("storage store not initialized")
		}

		// ListObjectsV2 (is list if key is empty, or if list-type query is 2)
		q := req.URL.Query()
		isList := (key == "") || (q.Get("list-type") == "2")

		if isList {
			files, err := m.store.List()
			if err != nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				m.conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
				return 0, 0, fmt.Errorf("failed to list files: %w", err)
			}

			prefix := q.Get("prefix")
			delimiter := q.Get("delimiter")
			maxKeys := 1000
			if maxKeysStr := q.Get("max-keys"); maxKeysStr != "" {
				if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 {
					maxKeys = mk
				}
			}

			xmlBytes := FormatListObjectsV2XML(bucket, prefix, delimiter, maxKeys, files)

			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var respBuf bytes.Buffer
			respBuf.WriteString("HTTP/1.1 200 OK\r\n")
			respBuf.WriteString("Content-Type: application/xml\r\n")
			respBuf.WriteString("Content-Length: " + strconv.Itoa(len(xmlBytes)) + "\r\n")
			respBuf.WriteString("Connection: close\r\n\r\n")
			respBuf.Write(xmlBytes)

			if _, err := m.conn.Write(respBuf.Bytes()); err != nil {
				return 0, 0, fmt.Errorf("failed to write XML list response: %w", err)
			}

			return 0, 0, ErrRequestHandled
		}

		// GetObject (file download)
		rc, meta, err := m.store.Get(key)
		if err != nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err == syscall.ENOENT || os.IsNotExist(err) {
				m.conn.Write([]byte("HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
				return 0, 0, ErrRequestHandled
			}
			m.conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, fmt.Errorf("failed to get file %q: %w", key, err)
		}
		defer rc.Close()

		// 🛡️ Sentinel: Set a progressive write deadline proportional to the file size
		// to prevent long-running connection stalls while supporting large objects.
		copyTimeout := 5 * time.Second
		mb := meta.Size / (1024 * 1024)
		if mb > 0 {
			copyTimeout += time.Duration(mb) * time.Second
		}
		m.conn.SetWriteDeadline(time.Now().Add(copyTimeout))

		var respBuf bytes.Buffer
		respBuf.WriteString("HTTP/1.1 200 OK\r\n")
		respBuf.WriteString("Content-Length: " + strconv.FormatInt(meta.Size, 10) + "\r\n")
		respBuf.WriteString("Content-Type: application/octet-stream\r\n")
		respBuf.WriteString("Connection: close\r\n\r\n")

		if _, err := m.conn.Write(respBuf.Bytes()); err != nil {
			return 0, 0, fmt.Errorf("failed to write GET headers: %w", err)
		}

		if _, err := io.Copy(m.conn, rc); err != nil {
			return 0, 0, fmt.Errorf("failed to stream GET body: %w", err)
		}

		return 0, 0, ErrRequestHandled
	}

	// Intercept DELETE requests
	if req.Method == "DELETE" {
		if m.store == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, fmt.Errorf("storage store not initialized")
		}

		if key == "" {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, fmt.Errorf("missing key in DELETE request")
		}

		err := m.store.Delete(key)
		if err != nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, fmt.Errorf("failed to delete file %q: %w", key, err)
		}

		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		m.conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))

		return 0, 0, ErrRequestHandled
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
			return 0, 0, fmt.Errorf("invalid requested mode: %s: %w", requestedModeStr, syscall.EBADMSG)
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

		// 🛡️ Sentinel: Sanitize S3 hash to prevent directory traversal via malicious metadata.
		if m.meta.Hash != "" && hasPathTraversalChars(m.meta.Hash) {
			return 0, 0, fmt.Errorf("invalid hash: %s: %w", m.meta.Hash, syscall.EBADMSG)
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
	// 🛡️ Zero-Crash: Set a short write deadline to prevent stalled socket hanging
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})

	// ⚡ Bolt: Eliminate http.Response and header map allocations via direct byte response writing
	var buf [256]byte
	b := buf[:0]
	b = append(b, "HTTP/1.1 200 OK\r\nX-Momo-Replication-Mode: "...)
	b = strconv.AppendInt(b, int64(mode), 10)
	b = append(b, "\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n"...)

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 256 {
		return fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	if _, err = m.conn.Write(b); err != nil {
		return fmt.Errorf("failed to write replication mode response: %v: %w", err, syscall.EPIPE)
	}
	return nil
}

func (m *S3Communicator) SendMetadata(meta *common.FileMetadata) (status int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendMetadata: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// 🛡️ Zero-Crash: Set a short write deadline to prevent stalled socket hanging
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})

	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}

	// Validate wire name length to prevent protocol buffer overflow
	wireName := meta.Name
	if meta.RemotePath != "" {
		norm, err := common.NormalizeVirtualPath(meta.RemotePath)
		if err != nil {
			return 0, fmt.Errorf("invalid path: %w", err)
		}
		wireName = norm + "/" + meta.Name
	}
	if len(wireName) > common.FileInfoLength {
		return 0, fmt.Errorf("joined remote path exceeds maximum length of %d: %w", common.FileInfoLength, syscall.ENAMETOOLONG)
	}

	// ⚡ Bolt: Eliminate fmt.Sprintf and string allocations using stack-allocated buffer
	var buf [512]byte
	b := buf[:0]
	b = append(b, "PUT /"...)
	if meta.RemotePath != "" {
		norm, _ := common.NormalizeVirtualPath(meta.RemotePath)
		b = append(b, norm...)
		b = append(b, '/')
	}
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

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 512 {
		return 0, fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	if _, err = m.conn.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write metadata request: %v: %w", err, syscall.EPIPE)
	}

	// ⚡ Bolt: Read the response immediately to get the metadata status.
	resp, err := http.ReadResponse(m.reader, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to read metadata status response: %v: %w", err, syscall.EBADMSG)
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

		// 🛡️ Sentinel: Sanitize S3 hash to prevent directory traversal via malicious metadata.
		if hash != "" && hasPathTraversalChars(hash) {
			return common.FileMetadata{}, fmt.Errorf("invalid hash: %s: %w", hash, syscall.EBADMSG)
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
	// 🛡️ Zero-Crash: Set a short write deadline to prevent stalled socket hanging
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})

	// ⚡ Bolt: Eliminate http.Response and header map allocations via direct byte response writing
	var buf [256]byte
	b := buf[:0]
	b = append(b, "HTTP/1.1 200 OK\r\nX-Momo-Metadata-Status: "...)
	b = strconv.AppendInt(b, int64(status), 10)
	b = append(b, "\r\nContent-Length: 0\r\nConnection: keep-alive\r\n\r\n"...)

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 256 {
		return fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	if _, err = m.conn.Write(b); err != nil {
		return fmt.Errorf("failed to write metadata status response: %v: %w", err, syscall.EPIPE)
	}
	return nil
}

func (m *S3Communicator) SendACK(serverId int) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendACK: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// 🛡️ Zero-Crash: Set a short write deadline to prevent stalled socket hanging
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})

	// ⚡ Bolt: Eliminate http.Response allocation and fmt.Sprintf using stack buffer direct write
	var buf [256]byte
	b := buf[:0]
	b = append(b, "HTTP/1.1 200 OK\r\nContent-Length: "...)

	// serverId string length calculation
	idStr := strconv.Itoa(serverId)
	bodyLength := 3 + len(idStr)

	b = strconv.AppendInt(b, int64(bodyLength), 10)
	b = append(b, "\r\nConnection: keep-alive\r\n\r\nACK"...)
	b = append(b, idStr...)

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 256 {
		return fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	if _, err = m.conn.Write(b); err != nil {
		return fmt.Errorf("failed to write ACK response: %v: %w", err, syscall.EPIPE)
	}
	return nil
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
		return fmt.Errorf("failed to read ACK response: %v: %w", err, syscall.EBADMSG)
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

// hasPathTraversalChars returns true if the string contains '.', '/' or '\'.
// It is inlineable and operates directly on the string bytes without any heap allocation (Rule 19).
func hasPathTraversalChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '/' || c == '\\' {
			return true
		}
	}
	return false
}

// extractS3BucketAndKey parses the bucket name and key path from an S3 HTTP request.
// It supports both virtual-host style and path-style S3 URL schemas.
func extractS3BucketAndKey(req *http.Request) (bucket string, key string) {
	host := req.Host
	if strings.Contains(host, ".") {
		parts := strings.Split(host, ".")
		if len(parts) > 1 && parts[len(parts)-1] == "localhost" {
			bucket = parts[0]
		} else if strings.Contains(host, ".s3") {
			idx := strings.Index(host, ".s3")
			bucket = host[:idx]
		}
	}

	pathStr := req.URL.Path
	cleanPath := path.Clean(pathStr)
	cleanPath = strings.TrimPrefix(cleanPath, "/")

	if bucket == "" {
		if cleanPath != "" && cleanPath != "." {
			parts := strings.SplitN(cleanPath, "/", 2)
			bucket = parts[0]
			if len(parts) > 1 {
				key = parts[1]
			}
		}
	} else {
		key = cleanPath
	}

	if key == "." {
		key = ""
	}
	return bucket, key
}

// FormatListObjectsV2XML constructs an S3-compliant ListObjectsV2 XML response
// using a pre-allocated bytes.Buffer to avoid excessive heap allocations (⚡ Bolt pattern).
func FormatListObjectsV2XML(bucketName, prefix, delimiter string, maxKeys int, files []common.FileMetadata) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)

	buf.WriteString(`<Name>`)
	xmlEscape(&buf, bucketName)
	buf.WriteString(`</Name>`)

	buf.WriteString(`<Prefix>`)
	xmlEscape(&buf, prefix)
	buf.WriteString(`</Prefix>`)

	if delimiter != "" {
		buf.WriteString(`<Delimiter>`)
		xmlEscape(&buf, delimiter)
		buf.WriteString(`</Delimiter>`)
	}

	buf.WriteString(`<MaxKeys>`)
	buf.WriteString(strconv.Itoa(maxKeys))
	buf.WriteString(`</MaxKeys>`)

	buf.WriteString(`<IsTruncated>false</IsTruncated>`)

	commonPrefixes := make(map[string]bool)
	keyCount := 0

	for _, file := range files {
		key := file.Name
		if file.RemotePath != "" {
			key = file.RemotePath + "/" + file.Name
		}

		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}

		if delimiter != "" {
			relativeKey := key[len(prefix):]
			delimIdx := strings.Index(relativeKey, delimiter)
			if delimIdx != -1 {
				subPrefix := prefix + relativeKey[:delimIdx+1]
				if !commonPrefixes[subPrefix] {
					commonPrefixes[subPrefix] = true
				}
				continue
			}
		}

		buf.WriteString(`<Contents>`)
		buf.WriteString(`<Key>`)
		xmlEscape(&buf, key)
		buf.WriteString(`</Key>`)
		buf.WriteString(`<LastModified>2026-06-29T12:00:00.000Z</LastModified>`)
		buf.WriteString(`<ETag>"`)
		xmlEscape(&buf, file.Hash)
		buf.WriteString(`"</ETag>`)
		buf.WriteString(`<Size>`)
		buf.WriteString(strconv.FormatInt(file.Size, 10))
		buf.WriteString(`</Size>`)
		buf.WriteString(`<StorageClass>STANDARD</StorageClass>`)
		buf.WriteString(`</Contents>`)
		keyCount++

		if maxKeys > 0 && keyCount >= maxKeys {
			break
		}
	}

	for cp := range commonPrefixes {
		buf.WriteString(`<CommonPrefixes>`)
		buf.WriteString(`<Prefix>`)
		xmlEscape(&buf, cp)
		buf.WriteString(`</Prefix>`)
		buf.WriteString(`</CommonPrefixes>`)
		keyCount++
	}

	buf.WriteString(`<KeyCount>`)
	buf.WriteString(strconv.Itoa(keyCount))
	buf.WriteString(`</KeyCount>`)

	buf.WriteString(`</ListBucketResult>`)
	return buf.Bytes()
}

func xmlEscape(buf *bytes.Buffer, s string) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteByte(c)
		}
	}
}
