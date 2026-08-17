package transport

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
)

// s3ReadHeaderTimeout is the maximum time to read an HTTP request header on
// the S3 gateway. It prevents slowloris attacks where a client sends headers
// one byte at a time to keep the connection alive indefinitely (issue #592).
var s3ReadHeaderTimeout = 10 * time.Second

type LimitedConnReader struct {
	r     net.Conn
	limit int64
	read  int64
}

func (l *LimitedConnReader) Read(p []byte) (n int, err error) {
	if l.limit > 0 && l.read >= l.limit {
		return 0, fmt.Errorf("read limit exceeded: %w", syscall.ENOBUFS)
	}
	if l.limit > 0 && int64(len(p)) > (l.limit-l.read) {
		p = p[:l.limit-l.read]
	}
	n, err = l.r.Read(p)
	l.read += int64(n)
	return n, err
}

func (l *LimitedConnReader) SetLimit(limit int64) {
	l.limit = limit
	l.read = 0
}

func (l *LimitedConnReader) ClearLimit() {
	l.limit = 0
	l.read = 0
}

type S3Communicator struct {
	conn       net.Conn
	connReader *LimitedConnReader
	reader     *bufio.Reader
	remoteAddr net.Addr

	// Client state
	clientAuthToken string
	clientTimestamp int64

	// Server state
	meta common.FileMetadata
	// isExternalClient is true when the client did not send X-Momo-Requested-Mode,
	// indicating it is an external S3 client (e.g., aws-cli) that cannot perform
	// client-side replication.
	isExternalClient bool

	// Storage store for list, get, and delete operations
	store storage.Store

	// configuredBucket is the single bucket name exposed by the S3 gateway for
	// bucket-management operations (issue #767). Empty means legacy flat
	// namespace mode where no bucket semantics are enforced.
	configuredBucket string

	// GlobalLister for scatter-gather list queries (optional)
	globalLister GlobalLister
	// LeaseAcquirer for lease-based consensus on deletes (optional)
	leaseAcquirer LeaseAcquirer
	// DeletePropagator for P2P delete fan-out (optional)
	deletePropagator DeletePropagator
	// OPRFService for threshold-OPRF evaluation (optional). Configured by the
	// server daemon so the dedicated /?momo-oprf-eval endpoint can gather peer
	// evaluations over P2P. Nil disables OPRF on this connection (issue #817).
	oprfService OPRFService
	// MetricsHook for instrumentation (optional)
	metricsHook MetricsHook
	// isPeer is always false for S3 connections (S3 clients are never peers).
	isPeer bool

	// Streaming (aws-chunked) upload state (issue #773). When a PUT arrives
	// with an aws-chunked body, the gateway decodes the framing at the
	// transport boundary, spills the de-framed content to a temp file, and
	// resolves meta.Hash/Size to the decoded content hash/size. The spill is
	// then replayed through Read() so the standard server pipeline sees only
	// de-framed content with the real content-addressed hash.
	streamingPayload bool
	streamingSpill   *os.File
	streamingReader  io.Reader
	// sigV4 holds the per-chunk verification context derived during SigV4
	// verification for signed streaming PUTs (issue #773).
	sigV4 *streamingSigningCtx
}

func NewS3Communicator(conn net.Conn) *S3Communicator {
	connReader := &LimitedConnReader{r: conn}
	return &S3Communicator{
		conn:       conn,
		connReader: connReader,
		reader:     bufio.NewReader(connReader),
		remoteAddr: conn.RemoteAddr(),
	}
}

func (m *S3Communicator) SetStore(store storage.Store) {
	m.store = store
}

// SetConfiguredBucket sets the single bucket name exposed by the S3 gateway for
// bucket-management operations (issue #767). An empty value disables bucket
// semantics (legacy flat namespace mode).
func (m *S3Communicator) SetConfiguredBucket(bucket string) {
	m.configuredBucket = bucket
}

// validBucket reports whether the addressed bucket name is acceptable under the
// single-bucket policy. When no bucket is configured (legacy flat mode), any
// bucket name is accepted.
func (m *S3Communicator) validBucket(bucket string) bool {
	return m.configuredBucket == "" || bucket == m.configuredBucket
}

// listFiles returns the files in the store, using scatter-gather when available
// so DeleteBucket emptiness checks agree with ListObjectsV2.
func (m *S3Communicator) listFiles(timeout time.Duration) ([]common.FileMetadata, error) {
	if m.globalLister != nil {
		return m.globalLister.GlobalList(timeout)
	}
	return m.store.List()
}

// SetGlobalLister sets the scatter-gather list capability.
func (m *S3Communicator) SetGlobalLister(gl GlobalLister) {
	m.globalLister = gl
}

// SetLeaseAcquirer sets the lease-based consensus capability.
func (m *S3Communicator) SetLeaseAcquirer(la LeaseAcquirer) {
	m.leaseAcquirer = la
}

// SetDeletePropagator sets the P2P delete propagation capability.
func (m *S3Communicator) SetDeletePropagator(dp DeletePropagator) {
	m.deletePropagator = dp
}

// SetMetricsHook sets the metrics instrumentation hook.
func (m *S3Communicator) SetMetricsHook(hook MetricsHook) {
	m.metricsHook = hook
}

// SetOPRFService stores the threshold-OPRF evaluation service so the dedicated
// POST /?momo-oprf-eval endpoint can serve confidential-dedup evaluations on
// the S3 gateway (issue #817). Nil disables OPRF on this connection.
func (m *S3Communicator) SetOPRFService(s OPRFService) {
	m.oprfService = s
}

// s3OPRFEvalTimeout bounds the threshold-OPRF evaluation performed server-side
// when serving a /?momo-oprf-eval request (issue #817). It must be generous
// enough for the P2P round-trips to peers, while preventing a hung evaluation
// from pinning the connection.
const s3OPRFEvalTimeout = 10 * time.Second

// SendOPRFEval implements the Communicator interface for the S3 gateway over a
// dedicated HTTP endpoint. It sends the 32-byte blinded dedup tag and reads the
// evaluation records back in the same wire layout the native protocols use, so
// confidential dedup works uniformly across all four transports (issue #817).
func (m *S3Communicator) SendOPRFEval(authToken string, timestamp int64, blinded []byte, threshold int) (results []OPRFEvalResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 SendOPRFEval: %v", r)
			err = fmt.Errorf("internal S3 OPRF protocol panic: %w", syscall.EIO)
		}
	}()

	if len(blinded) != 32 {
		return nil, fmt.Errorf("oprf: blinded tag must be 32 bytes: %w", syscall.EINVAL)
	}
	if strings.ContainsAny(authToken, "\r\n") {
		return nil, fmt.Errorf("auth token contains CRLF characters: %w", syscall.EINVAL)
	}

	host := "127.0.0.1"
	if m.remoteAddr != nil {
		host = m.remoteAddr.String()
	}
	if strings.ContainsAny(host, "\r\n") {
		return nil, fmt.Errorf("host contains CRLF characters: %w", syscall.EINVAL)
	}

	// ⚡ Bolt: stack-allocated request header buffer.
	var buf [384]byte
	b := buf[:0]
	b = append(b, "POST /?momo-oprf-eval HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nAuthorization: Bearer "...)
	b = append(b, authToken...)
	b = append(b, "\r\nX-Momo-Timestamp: "...)
	b = strconv.AppendInt(b, timestamp, 10)
	b = append(b, "\r\nContent-Length: 32\r\nConnection: close\r\n\r\n"...)
	if len(b) > len(buf) {
		return nil, fmt.Errorf("oprf: request header exceeds stack buffer: %w", syscall.ENOBUFS)
	}

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer m.conn.SetWriteDeadline(time.Time{})
	if _, werr := m.conn.Write(b); werr != nil {
		return nil, fmt.Errorf("oprf: failed to write request header: %v: %w", werr, syscall.EPIPE)
	}
	if _, werr := m.conn.Write(blinded); werr != nil {
		return nil, fmt.Errorf("oprf: failed to write blinded tag: %v: %w", werr, syscall.EPIPE)
	}

	m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout))
	resp, rerr := http.ReadResponse(m.reader, nil)
	m.conn.SetReadDeadline(time.Time{})
	if rerr != nil {
		return nil, fmt.Errorf("oprf: failed to read response: %v: %w", rerr, syscall.EBADMSG)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oprf: server returned HTTP %d: %w", resp.StatusCode, syscall.EBADMSG)
	}

	// Parse the eval body in the same wire layout as the native protocols:
	// [4-byte BE count] then count × (4-byte BE ShareIndex + 4-byte BE EvalLen + EvalLen bytes).
	var countBuf [4]byte
	if _, err := io.ReadFull(resp.Body, countBuf[:]); err != nil {
		return nil, fmt.Errorf("oprf: failed to read result count: %v: %w", err, syscall.EBADMSG)
	}
	count := int(binary.BigEndian.Uint32(countBuf[:]))
	if count == 0 {
		return nil, fmt.Errorf("oprf: no evaluations returned (quorum not met): %w", syscall.EAGAIN)
	}
	if count > 255 {
		return nil, fmt.Errorf("oprf: implausible evaluation count %d: %w", count, syscall.EBADMSG)
	}

	results = make([]OPRFEvalResult, 0, count)
	for i := 0; i < count; i++ {
		var idxBuf [4]byte
		if _, err := io.ReadFull(resp.Body, idxBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to read share index: %v: %w", err, syscall.EBADMSG)
		}
		var lenBuf [4]byte
		if _, err := io.ReadFull(resp.Body, lenBuf[:]); err != nil {
			return nil, fmt.Errorf("oprf: failed to read eval length: %v: %w", err, syscall.EBADMSG)
		}
		evalLen := int(binary.BigEndian.Uint32(lenBuf[:]))
		if evalLen > 32 {
			return nil, fmt.Errorf("oprf: implausible eval length %d: %w", evalLen, syscall.EBADMSG)
		}
		eval := make([]byte, evalLen)
		if _, err := io.ReadFull(resp.Body, eval); err != nil {
			return nil, fmt.Errorf("oprf: failed to read eval: %v: %w", err, syscall.EBADMSG)
		}
		results = append(results, OPRFEvalResult{
			ShareIndex: int(binary.BigEndian.Uint32(idxBuf[:])),
			Eval:       eval,
		})
	}

	if len(results) < threshold {
		return nil, fmt.Errorf("oprf: only %d evaluations, need %d: %w", len(results), threshold, syscall.EAGAIN)
	}
	return results, nil
}

func (m *S3Communicator) Read(p []byte) (n int, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 Read: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	// A decoded aws-chunked payload is replayed from the spill file so the
	// server pipeline (getFile/store.Put) consumes only de-framed content.
	if m.streamingPayload && m.streamingReader != nil {
		return m.streamingReader.Read(p)
	}
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
	if m.streamingSpill != nil {
		name := m.streamingSpill.Name()
		m.streamingSpill.Close()
		os.Remove(name)
		m.streamingSpill = nil
		m.streamingReader = nil
		m.streamingPayload = false
	}
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

	if strings.ContainsAny(authToken, "\r\n") {
		return 0, fmt.Errorf("auth token contains CRLF characters: %w", syscall.EINVAL)
	}
	if strings.ContainsAny(host, "\r\n") {
		return 0, fmt.Errorf("host contains CRLF characters: %w", syscall.EINVAL)
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

	// 🛡️ Zero-Crash: Set a read deadline before reading the handshake response to
	// prevent the client from blocking indefinitely on an unresponsive server (issue #620).
	m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout))
	resp, err := http.ReadResponse(m.reader, nil)
	m.conn.SetReadDeadline(time.Time{})
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

	m.connReader.SetLimit(65536)                                // 🛡️ Bounded Network Loop/Read (Rule 24)
	m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout)) // 🛡️ Slowloris mitigation (issue #592)
	req, err := http.ReadRequest(m.reader)
	m.conn.SetReadDeadline(time.Time{})
	m.connReader.ClearLimit()
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read handshake request: %v: %w", err, syscall.EBADMSG)
	}
	// Keep the body open for PUT (upload), for POST ?delete (batch DeleteObjects),
	// and for the POST /?momo-oprf-eval endpoint (issue #817), whose bodies must
	// be read inside their handlers.
	if req.Method != "PUT" {
		_, hasDelete := req.URL.Query()["delete"]
		_, isOPRFEval := req.URL.Query()["momo-oprf-eval"]
		if req.Method != "POST" || (!hasDelete && !isOPRFEval) {
			req.Body.Close()
		}
	}

	authHeader := req.Header.Get("Authorization")
	var token string
	isSigV4 := false
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	} else if strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		isSigV4 = true
		components, ok := parseSigV4AuthHeader(authHeader)
		if !ok {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusForbidden, "AuthorizationHeaderMalformed", "The authorization header is malformed.", "")
			return 0, 0, syscall.EACCES
		}
		token = components.AccessKey
	} else if p, ok := parseSigV4QueryAuth(req); ok {
		isSigV4 = true
		token = p.AccessKey
	} else if isPresignedSigV4(req) {
		// X-Amz-Algorithm present but required presigned params incomplete → 400.
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "AuthorizationQueryParametersError", "Query-string authentication parameters are missing or malformed.", "")
		return 0, 0, fmt.Errorf("incomplete presigned auth query parameters: %w", syscall.EBADMSG)
	}

	tokenBuf := []byte(common.PadString(token, common.AuthTokenLength))
	if subtle.ConstantTimeCompare(tokenBuf, expectedAuthToken) == 1 {
		m.isPeer = false
	} else if peerToken := common.DerivePeerToken(expectedAuthToken); subtle.ConstantTimeCompare(tokenBuf, peerToken) == 1 {
		m.isPeer = true
	} else {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusForbidden, "AccessDenied", "Access Denied.", "")
		return 0, 0, syscall.EACCES
	}

	if isSigV4 {
		secretKey := common.TrimNullBytesString(expectedAuthToken)
		if !verifySigV4Signature(req, authHeader, secretKey) {
			log.Printf("AUDIT: SigV4 signature verification failed from %s", m.conn.RemoteAddr())
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var code, msg string
			if isPresignedSigV4(req) && !verifySigV4Expiry(req) {
				code, msg = "AccessDenied", "Request has expired."
			} else {
				code, msg = "SignatureDoesNotMatch", "The request signature we calculated does not match the signature you provided. Check your AWS secret access key and signing method."
			}
			writeS3Error(m.conn, http.StatusForbidden, code, msg, "")
			return 0, 0, syscall.EACCES
		}

		// issue #773: derive the per-chunk verification context for signed
		// streaming PUTs from the (already verified) request signature. The
		// seed signature is the request signature; chunk signatures chain from
		// it with the same derived signing key/date/scope.
		if req.Method == "PUT" && isStreamingLiteral(req.Header.Get("X-Amz-Content-Sha256")) {
			if comps, ok := parseSigV4AuthHeader(authHeader); ok {
				amzDate := req.Header.Get("X-Amz-Date")
				if amzDate == "" {
					amzDate = comps.AmzDate
				}
				m.sigV4 = &streamingSigningCtx{
					signingKey: deriveSigningKey(secretKey, comps.DateStamp, comps.Region),
					amzDate:    amzDate,
					scope:      comps.DateStamp + "/" + comps.Region + "/s3/aws4_request",
					seedSig:    comps.Signature,
				}
			} else if comps, ok := parseSigV4QueryAuth(req); ok {
				m.sigV4 = &streamingSigningCtx{
					signingKey: deriveSigningKey(secretKey, comps.DateStamp, comps.Region),
					amzDate:    comps.AmzDate,
					scope:      comps.DateStamp + "/" + comps.Region + "/s3/aws4_request",
					seedSig:    comps.Signature,
				}
			}
			// No SigV4 auth (e.g. Bearer momo peer): m.sigV4 stays nil and the
			// body is de-framed in unsigned mode.
		}
	}

	// 🛡️ Sentinel: Reject requests containing directory traversal characters (".." or "\") to prevent path traversal attacks.
	if strings.Contains(req.URL.Path, "..") || strings.Contains(req.URL.Path, "\\") {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid key path.", req.URL.Path)
		return 0, 0, fmt.Errorf("invalid key path traversal: %s: %w", req.URL.Path, syscall.EBADMSG)
	}

	// 🛡️ Dedicated threshold-OPRF evaluation endpoint (issue #817): POST
	// /?momo-oprf-eval carries a 32-byte blinded dedup tag and returns the peer
	// evaluations. It is intercepted before S3 REST routing so the momo client
	// gets confidential dedup uniformly across all four transports. Only
	// authenticated callers reach here (auth validated above).
	_, isOPRFEval := req.URL.Query()["momo-oprf-eval"]
	if req.Method == "POST" && isOPRFEval {
		if m.oprfService == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented", "Threshold-OPRF confidential dedup is not enabled on this node.", "")
			return 0, 0, fmt.Errorf("oprf evaluation not enabled: %w", syscall.ENOTSUP)
		}
		return m.handleOPRFEval(req)
	}

	bucket, key := extractS3BucketAndKey(req)

	// Intercept POST for multipart operations (issue #764)
	if req.Method == "POST" {
		q := req.URL.Query()

		// POST /{bucket}/{key}?uploads -> CreateMultipartUpload
		if _, hasUploads := q["uploads"]; hasUploads && key != "" {
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			return m.handleCreateMultipartUpload(bucket, key)
		}

		// POST /{bucket}/{key}?uploadId=X -> CompleteMultipartUpload
		if _, hasUploadID := q["uploadId"]; hasUploadID && key != "" {
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			if m.store == nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
				return 0, 0, fmt.Errorf("storage store not initialized")
			}
			return m.handleCompleteMultipartUpload(req, bucket, key)
		}

		// POST /{bucket}?delete (DeleteObjects batch)
		if _, hasDelete := q["delete"]; hasDelete {
			if m.store == nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
				return 0, 0, fmt.Errorf("storage store not initialized")
			}
			return m.handleBatchDelete(bucket, req)
		}
	}

	// Intercept GET requests (for ListObjectsV2, ListBuckets, GetBucketLocation, or GetObject)
	if req.Method == "GET" {
		if m.store == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
			return 0, 0, fmt.Errorf("storage store not initialized")
		}

		q := req.URL.Query()

		// GET / -> ListBuckets (root, no bucket addressed, no list-type).
		// ?list-type=2 on root keeps the legacy flat-namespace list-all path.
		if bucket == "" && q.Get("list-type") == "" {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var buf [256]byte
			b := buf[:0]
			xmlBytes := FormatListBucketsXML(m.configuredBucket)
			b = append(b, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
			b = strconv.AppendInt(b, int64(len(xmlBytes)), 10)
			b = append(b, "\r\nConnection: close\r\n\r\n"...)
			if _, err := m.conn.Write(b); err != nil {
				return 0, 0, fmt.Errorf("failed to write ListBuckets headers: %v: %w", err, syscall.EPIPE)
			}
			if _, err := m.conn.Write(xmlBytes); err != nil {
				return 0, 0, fmt.Errorf("failed to write ListBuckets body: %v: %w", err, syscall.EPIPE)
			}
			return 0, 0, ErrRequestHandled
		}

		// GET /bucket?location -> GetBucketLocation (bucket root, location param present).
		if key == "" {
			if _, hasLocation := q["location"]; hasLocation {
				if !m.validBucket(bucket) {
					m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
					return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
				}
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				var buf [256]byte
				b := buf[:0]
				xmlBytes := FormatGetBucketLocationXML("")
				b = append(b, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
				b = strconv.AppendInt(b, int64(len(xmlBytes)), 10)
				b = append(b, "\r\nConnection: close\r\n\r\n"...)
				if _, err := m.conn.Write(b); err != nil {
					return 0, 0, fmt.Errorf("failed to write GetBucketLocation headers: %v: %w", err, syscall.EPIPE)
				}
				if _, err := m.conn.Write(xmlBytes); err != nil {
					return 0, 0, fmt.Errorf("failed to write GetBucketLocation body: %v: %w", err, syscall.EPIPE)
				}
				return 0, 0, ErrRequestHandled
			}
		}

		// ListObjectsV2 (is list if key is empty, or if list-type query is 2)
		isList := (key == "") || (q.Get("list-type") == "2")

		if isList {
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}

			files, err := m.listFiles(5 * time.Second)
			if err != nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to list files.", "")
				return 0, 0, fmt.Errorf("failed to list files: %w", err)
			}

			prefix := q.Get("prefix")
			delimiter := q.Get("delimiter")
			// Pagination: continuation-token resumes after the last key of the
			// previous page; start-after is an informational starting hint.
			startAfter := q.Get("continuation-token")
			if startAfter == "" {
				startAfter = q.Get("start-after")
			}
			// fetch-owner=true (default false) includes the Owner element per key.
			fetchOwner := strings.EqualFold(q.Get("fetch-owner"), "true")
			maxKeys := 1000
			if maxKeysStr := q.Get("max-keys"); maxKeysStr != "" {
				if mk, err := strconv.Atoi(maxKeysStr); err == nil && mk > 0 {
					// 🛡️ Sentinel: Clamp max-keys to 1000 to prevent DoS via memory exhaustion in XML generation
					maxKeys = min(mk, 1000)
				}
			}

			xmlBytes, _, formatErr := FormatListObjectsV2XML(bucket, prefix, delimiter, maxKeys, startAfter, fetchOwner, files)
			if formatErr != nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid list request parameters.", "")
				return 0, 0, formatErr
			}

			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			// ⚡ Bolt: Eliminate http.Response allocation and bytes.Buffer using stack buffer direct write
			var buf [256]byte
			b := buf[:0]
			b = append(b, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
			b = strconv.AppendInt(b, int64(len(xmlBytes)), 10)
			b = append(b, "\r\nConnection: close\r\n\r\n"...)

			if _, err := m.conn.Write(b); err != nil {
				return 0, 0, fmt.Errorf("failed to write XML list response headers: %v: %w", err, syscall.EPIPE)
			}
			if _, err := m.conn.Write(xmlBytes); err != nil {
				return 0, 0, fmt.Errorf("failed to write XML list response: %v: %w", err, syscall.EPIPE)
			}

			return 0, 0, ErrRequestHandled
		}

		// GET /{bucket}/{key}?uploadId=X -> ListParts (issue #764).
		if _, hasUploadID := q["uploadId"]; hasUploadID && key != "" {
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			return m.handleListParts(bucket, key, q.Get("uploadId"))
		}

		// GET /?uploads -> ListMultipartUploads (issue #764).
		if key == "" && q.Get("list-type") == "" {
			if _, hasUploads := q["uploads"]; hasUploads {
				if !m.validBucket(bucket) {
					m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
					return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
				}
				return m.handleListMultipartUploads(bucket)
			}
		}

		// GetObject (file download)
		if !m.validBucket(bucket) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
			return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
		}
		rc, meta, err := m.store.Get(key)
		if err != nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err == syscall.ENOENT || os.IsNotExist(err) {
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
				return 0, 0, ErrRequestHandled
			}
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The request failed due to an internal error.", key)
			return 0, 0, fmt.Errorf("failed to get file %q: %w", key, err)
		}
		defer rc.Close()

		// ETag/Last-Modified are derived from the same metadata HEAD uses, so
		// conditional and range semantics stay consistent per object (issue #771).
		etag := meta.Hash
		lastModifiedStr := formatHTTPLastModified(meta.ModTime)
		// Preserved S3 object metadata (Content-Type, x-amz-meta-*, cache headers)
		// stored at rest on PUT and echoed here (issue #772).
		s3Headers := m.storedS3Meta(key)
		contentType := s3Headers["content-type"]
		rangeHeader := req.Header.Get("Range")

		// If-Match: any listed entity-tag must match, otherwise 412.
		if im := req.Header.Get("If-Match"); im != "" && !etagMatches(im, etag) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusPreconditionFailed, "PreconditionFailed", "At least one of the pre-conditions you specified did not hold.", key)
			return 0, 0, ErrRequestHandled
		}

		// If-None-Match (with If-Modified-Since as its fallback): 304 when the
		// object has not changed since the client's cached representation.
		write304 := func() {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var buf [16384]byte
			b := buf[:0]
			b = append(b, "HTTP/1.1 304 Not Modified\r\nETag: \""...)
			b = append(b, etag...)
			b = append(b, "\"\r\nLast-Modified: "...)
			b = append(b, lastModifiedStr...)
			b = append(b, "\r\nContent-Length: 0\r\n"...)
			b = appendS3MetaHeaders(b, s3Headers, contentType)
			b = append(b, "Connection: close\r\n\r\n"...)
			// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
			if len(b) > 16384 {
				log.Printf("WARNING: 304 response buffer overflow: %v", syscall.ENOBUFS)
				return
			}
			if _, err := m.conn.Write(b); err != nil {
				log.Printf("WARNING: failed to write 304 response: %v", err)
			}
		}
		if inm := req.Header.Get("If-None-Match"); inm != "" {
			if etagMatches(inm, etag) {
				write304()
				return 0, 0, ErrRequestHandled
			}
		} else if ims := req.Header.Get("If-Modified-Since"); ims != "" {
			if t, perr := http.ParseTime(ims); perr == nil && !time.Unix(0, meta.ModTime).After(t.Truncate(time.Second)) {
				write304()
				return 0, 0, ErrRequestHandled
			}
		}

		// If-Range: only meaningful alongside a Range header. A non-matching
		// entity-tag or a stale date falls back to the full 200 response.
		if ir := req.Header.Get("If-Range"); ir != "" && rangeHeader != "" && !ifRangeMatches(ir, etag, meta.ModTime) {
			rangeHeader = ""
		}

		// Resolve the requested byte span against the object size.
		var start, end int64
		serveRange := false
		if rangeHeader != "" {
			var unsatisfiable bool
			start, end, serveRange, unsatisfiable = parseS3Range(rangeHeader, meta.Size)
			if unsatisfiable {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeRangeNotSatisfiable(m.conn, meta.Size, key)
				return 0, 0, ErrRequestHandled
			}
		}

		// Body length served on the wire (the full object or the range span).
		bodyLen := meta.Size
		if serveRange {
			bodyLen = end - start + 1
		}

		// 🛡️ Sentinel: Set a progressive write deadline proportional to the file size
		// to prevent long-running connection stalls while supporting large objects.
		copyTimeout := 5 * time.Second
		mb := bodyLen / (1024 * 1024)
		if mb > 0 {
			maxMB := int64(math.MaxInt64 / int64(time.Second))
			if mb > maxMB {
				mb = maxMB
			}
			copyTimeout += time.Duration(mb) * time.Second
		}
		m.conn.SetWriteDeadline(time.Now().Add(copyTimeout))

		// ⚡ Bolt: Eliminate http.Response allocation and bytes.Buffer using stack buffer direct write
		var buf [16384]byte
		b := buf[:0]
		if serveRange {
			b = append(b, "HTTP/1.1 206 Partial Content\r\nContent-Range: bytes "...)
			b = strconv.AppendInt(b, start, 10)
			b = append(b, "-"...)
			b = strconv.AppendInt(b, end, 10)
			b = append(b, "/"...)
			b = strconv.AppendInt(b, meta.Size, 10)
			b = append(b, "\r\nContent-Length: "...)
		} else {
			b = append(b, "HTTP/1.1 200 OK\r\nContent-Length: "...)
		}
		b = strconv.AppendInt(b, bodyLen, 10)
		b = append(b, "\r\nETag: \""...)
		b = append(b, etag...)
		b = append(b, "\"\r\nLast-Modified: "...)
		b = append(b, lastModifiedStr...)
		b = append(b, "\r\n"...)
		b = appendS3MetaHeaders(b, s3Headers, contentType)
		b = append(b, "Connection: close\r\n\r\n"...)

		// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
		if len(b) > 16384 {
			return 0, 0, fmt.Errorf("GET response buffer overflow: %w", syscall.ENOBUFS)
		}

		if _, err := m.conn.Write(b); err != nil {
			return 0, 0, fmt.Errorf("failed to write GET headers: %v: %w", err, syscall.EPIPE)
		}

		if serveRange {
			// Skip to the range start without buffering the object in memory,
			// then stream exactly the requested span (bounded-memory design).
			if _, err := io.CopyN(io.Discard, rc, start); err != nil {
				return 0, 0, fmt.Errorf("failed to skip to range start: %v: %w", err, syscall.EPIPE)
			}
			if _, err := io.CopyN(m.conn, rc, bodyLen); err != nil {
				return 0, 0, fmt.Errorf("failed to stream range body: %v: %w", err, syscall.EPIPE)
			}
		} else {
			if _, err := io.Copy(m.conn, rc); err != nil {
				return 0, 0, fmt.Errorf("failed to stream GET body: %v: %w", err, syscall.EPIPE)
			}
		}

		if m.metricsHook != nil {
			m.metricsHook.IncDownloads()
			m.metricsHook.AddBytesDownloaded(uint64(bodyLen))
		}

		return 0, 0, ErrRequestHandled
	}

	// Intercept HEAD requests (HeadObject or HeadBucket). HEAD is a read-only
	// metadata operation and must bypass momo framing (issue #765).
	if req.Method == "HEAD" {
		// 🛡️ Rule 37: Zero-Crash recovery for the HEAD interception block.
		defer func() {
			if r := recover(); r != nil {
				log.Printf("CRITICAL: Panic recovered in S3 HEAD interceptor: %v", r)
				err = fmt.Errorf("internal S3 HEAD panic: %w", syscall.EIO)
			}
		}()

		if m.store == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
			return 0, 0, fmt.Errorf("storage store not initialized")
		}

		if key == "" {
			// HEAD / -> endpoint/liveness check; HEAD /bucket -> HeadBucket.
			// momo uses a bucket-less model (no bucket registry yet, issue #767),
			// so a configured store implies the bucket exists.
			if bucket != "" && !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
			return 0, 0, ErrRequestHandled
		}

		// HeadObject: reuse store.Get metadata so HEAD and GET agree on ETag/Last-Modified.
		if !m.validBucket(bucket) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
			return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
		}
		rc, meta, err := m.store.Get(key)
		if err != nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err == syscall.ENOENT || os.IsNotExist(err) {
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.", key)
				return 0, 0, ErrRequestHandled
			}
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The request failed due to an internal error.", key)
			return 0, 0, fmt.Errorf("failed to head file %q: %w", key, err)
		}
		rc.Close()

		// ⚡ Bolt: stack-buffer direct write for the header-only response (no body).
		// Preserved S3 object metadata echoed here (issue #772): HEAD mut always
		// agree with GET on Content-Type and x-amz-meta-*.
		s3Headers := m.storedS3Meta(key)
		contentType := s3Headers["content-type"]
		var buf [16384]byte
		b := buf[:0]
		b = append(b, "HTTP/1.1 200 OK\r\nETag: \""...)
		b = append(b, meta.Hash...)
		b = append(b, "\"\r\nContent-Length: "...)
		b = strconv.AppendInt(b, meta.Size, 10)
		b = append(b, "\r\nLast-Modified: "...)
		b = append(b, formatHTTPLastModified(meta.ModTime)...)
		b = append(b, "\r\n"...)
		b = appendS3MetaHeaders(b, s3Headers, contentType)
		b = append(b, "Connection: close\r\n\r\n"...)

		// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
		if len(b) > 16384 {
			return 0, 0, fmt.Errorf("HEAD response buffer overflow: %w", syscall.ENOBUFS)
		}

		if _, err := m.conn.Write(b); err != nil {
			return 0, 0, fmt.Errorf("failed to write HEAD response: %v: %w", err, syscall.EPIPE)
		}

		return 0, 0, ErrRequestHandled
	}

	// Intercept DELETE requests (also AbortMultipartUpload, issue #764)
	if req.Method == "DELETE" {
		// DELETE /{bucket}/{key}?uploadId=X -> AbortMultipartUpload.
		if uploadID := req.URL.Query().Get("uploadId"); uploadID != "" && key != "" {
			return m.handleAbortMultipartUpload(uploadID)
		}

		if m.store == nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
			return 0, 0, fmt.Errorf("storage store not initialized")
		}

		if key == "" {
			if m.configuredBucket == "" {
				// Legacy flat mode: no bucket semantics, preserve existing behavior.
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Missing key in DELETE request.", "")
				return 0, 0, fmt.Errorf("missing key in DELETE request")
			}

			// Bucket mode: DeleteBucket (204, or 409 BucketNotEmpty if the
			// store still holds objects). Honest single-bucket semantics.
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			files, err := m.listFiles(5 * time.Second)
			if err != nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to list files.", "")
				return 0, 0, fmt.Errorf("failed to list files for DeleteBucket: %w", err)
			}
			if len(files) > 0 {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusConflict, "BucketNotEmpty", "The bucket you tried to delete is not empty.", bucket)
				return 0, 0, fmt.Errorf("bucket %q is not empty: %w", bucket, syscall.ENOTEMPTY)
			}
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			m.conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
			return 0, 0, ErrRequestHandled
		}

		if !m.validBucket(bucket) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
			return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
		}

		if m.leaseAcquirer != nil {
			if err := m.leaseAcquirer.AcquireLease(key, 10*time.Second); err != nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusServiceUnavailable, "ServiceUnavailable", "The request could not be completed because the resource is locked by another request.", key)
				return 0, 0, fmt.Errorf("failed to acquire lease for delete %q: %w", key, err)
			}
			defer m.leaseAcquirer.ReleaseLease(key)
		}

		err := m.store.Delete(key)
		if err != nil {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The request failed due to an internal error.", key)
			return 0, 0, fmt.Errorf("failed to delete file %q: %w", key, err)
		}

		// Propagate delete to all peers via scatter-gather (best-effort).
		if m.deletePropagator != nil {
			if pErr := m.deletePropagator.PropagateDelete(key, 5*time.Second); pErr != nil {
				log.Printf("P2P delete propagation for %s partially failed: %v", common.SanitizeLog(key), pErr)
			}
		}

		if m.metricsHook != nil {
			m.metricsHook.IncDeletes()
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
	} else {
		// External S3 client (e.g., aws-cli) — no X-Momo-Requested-Mode header.
		// Force DummyEpoch so the server treats this as a direct client connection
		// and uses its configured replication mode, then downgrades if needed.
		m.isExternalClient = true
		timestamp = common.DummyEpoch
	}

	// Parse Metadata if it's a PUT request
	if req.Method == "PUT" {
		// 🛡️ issue #776: enforce the honest SSE boundary before any PUT variant is
		// processed. AES256 is honored (persisted + echoed on GET/HEAD); SSE-C/KMS
		// and unknown algorithms are rejected instead of being silently downgraded.
		if err := m.validateSSEHeaders(req); err != nil {
			return 0, 0, err
		}

		// UploadPart: PUT /{bucket}/{key}?uploadId=X&partNumber=N (issue #764).
		// Intercepted before CopyObject/CreateBucket because the query params
		// are distinct from those operations.
		q := req.URL.Query()
		if _, hasUploadID := q["uploadId"]; hasUploadID && key != "" {
			if _, hasPartNum := q["partNumber"]; hasPartNum {
				if !m.validBucket(bucket) {
					m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
					writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
					return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
				}
				return m.handleUploadPart(req, bucket, key)
			}
		}

		// CopyObject: PUT with x-amz-copy-source copies an existing object within
		// the store (a namespace alias via store.Get -> store.Put), preserving its
		// S3 metadata, and answers with CopyObjectResult XML entirely here.
		if copySource := req.Header.Get("X-Amz-Copy-Source"); copySource != "" {
			if m.store == nil {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
				return 0, 0, fmt.Errorf("storage store not initialized")
			}
			return m.handleCopyObject(bucket, key, copySource)
		}

		// CreateBucket: PUT to a bucket root (key empty) in bucket mode.
		// Legacy flat mode (no configured bucket) keeps the upload behavior.
		if m.configuredBucket != "" && key == "" {
			if !m.validBucket(bucket) {
				m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
				return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
			}
			// CreateBucket succeeds for the configured bucket: 200 + LocationConstraint.
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			var buf [256]byte
			b := buf[:0]
			xmlBytes := FormatGetBucketLocationXML("")
			b = append(b, "HTTP/1.1 200 OK\r\nLocation: /"...)
			b = append(b, m.configuredBucket...)
			b = append(b, "\r\nContent-Type: application/xml\r\nContent-Length: "...)
			b = strconv.AppendInt(b, int64(len(xmlBytes)), 10)
			b = append(b, "\r\nConnection: close\r\n\r\n"...)
			if _, err := m.conn.Write(b); err != nil {
				return 0, 0, fmt.Errorf("failed to write CreateBucket headers: %v: %w", err, syscall.EPIPE)
			}
			if _, err := m.conn.Write(xmlBytes); err != nil {
				return 0, 0, fmt.Errorf("failed to write CreateBucket body: %v: %w", err, syscall.EPIPE)
			}
			return 0, 0, ErrRequestHandled
		}

		// Object upload: enforce the single-bucket policy when configured.
		if !m.validBucket(bucket) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
			return 0, 0, fmt.Errorf("unknown bucket %q: %w", bucket, syscall.ENOENT)
		}

		// 🛡️ Sentinel: Sanitize S3 path to prevent traversal attacks.
		rawPath := req.URL.Path
		cleanPath := path.Clean(rawPath)
		if cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") || cleanPath == "/" {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid S3 path.", rawPath)
			return 0, 0, fmt.Errorf("invalid S3 path: %s: %w", rawPath, syscall.EBADMSG)
		}

		// Object name: in bucket mode store the bucket-relative key so GET/HEAD/DELETE
		// (which extract the key without the bucket) resolve the same object. Legacy
		// flat mode keeps the full cleaned path as the object name.
		if m.configuredBucket != "" {
			m.meta.Name = key
		} else {
			m.meta.Name = strings.TrimPrefix(cleanPath, "/")
		}
		m.meta.Size = req.ContentLength
		m.meta.Hash = req.Header.Get("X-Amz-Content-Sha256")
		if m.meta.Hash == "" {
			m.meta.Hash = req.Header.Get("Content-SHA256") // Fallback
		}

		// S3 object metadata (Content-Type, x-amz-meta-*, cache/encoding headers):
		// captured here so server.go persists it at rest and GET/HEAD echo it.
		m.meta.S3Headers = collectS3Headers(req)
		if s3metaStr := req.Header.Get("X-Momo-S3-Meta"); s3metaStr != "" {
			// Peer-forwarded PUT (base64 JSON); overrides direct-client headers
			// so forwarded objects carry the original S3 metadata.
			if data, decErr := base64.StdEncoding.DecodeString(s3metaStr); decErr == nil {
				if headers, jsonErr := common.UnmarshalS3MetaJSON(data); jsonErr == nil && len(headers) > 0 {
					m.meta.S3Headers = headers
				} else if jsonErr != nil {
					log.Printf("WARNING: ignoring malformed X-Momo-S3-Meta header: %v", jsonErr)
				}
			}
		}

		// 🛡️ Sentinel: Sanitize S3 hash to prevent directory traversal via malicious metadata.
		if m.meta.Hash != "" && common.HasPathTraversalChars(m.meta.Hash) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid hash value.", "")
			return 0, 0, fmt.Errorf("invalid hash: %s: %w", m.meta.Hash, syscall.EBADMSG)
		}

		// issue #773: aws-chunked streaming payload. Decode the framing at the
		// gateway boundary so de-framed content and the real content hash flow
		// through the standard momo PUT/replication pipeline.
		lit := req.Header.Get("X-Amz-Content-Sha256")
		if isStreamingLiteral(lit) || strings.Contains(req.Header.Get("Content-Encoding"), s3StreamingContentEncoding) {
			if err := m.decodeStreamingPayload(req); err != nil {
				return 0, 0, err
			}
		}
	}

	return requestedMode, timestamp, nil
}

// handleOPRFEval serves the dedicated threshold-OPRF evaluation endpoint
// (POST /?momo-oprf-eval, issue #817). It reads the client's 32-byte blinded
// dedup tag, gathers peer evaluations via the configured OPRFService, and
// writes them back in the native wire layout so the client-side decoder is
// shared across all four transports. Returns ErrRequestHandled so the daemon
// closes the connection after the exchange.
func (m *S3Communicator) handleOPRFEval(req *http.Request) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleOPRFEval: %v", r)
			err = fmt.Errorf("internal S3 OPRF protocol panic: %w", syscall.EIO)
		}
	}()

	// 🛡️ Sentinel: bounded read of the 32-byte blinded tag from the request
	// body (Rule 24). Reject a wrong-length body instead of trusting any
	// Content-Length, preventing framing confusion.
	blinded := make([]byte, 32)
	if _, rerr := io.ReadFull(req.Body, blinded); rerr != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest", "OPRF evaluation requires a 32-byte blinded tag.", "")
		return 0, 0, fmt.Errorf("oprf: failed to read blinded tag: %v: %w", rerr, syscall.EBADMSG)
	}

	results, evalErr := m.oprfService.EvaluateOPRF(blinded, s3OPRFEvalTimeout)
	if evalErr != nil {
		log.Printf("OPRF evaluation failed: %v", evalErr)
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "OPRF evaluation failed.", "")
		return 0, 0, fmt.Errorf("oprf: evaluation failed: %w", evalErr)
	}

	// Serialize the evaluations in the native wire layout. The daemon's
	// OPRFService fails closed (returns 0 results) when the quorum is unmet, so
	// the client-side decoder detects the same EAGAIN as on native transports.
	var body [1024]byte
	b := body[:0]
	var countBuf [4]byte
	binary.BigEndian.PutUint32(countBuf[:], uint32(len(results)))
	b = append(b, countBuf[:]...)
	for _, r := range results {
		binary.BigEndian.PutUint32(countBuf[:], uint32(r.ShareIndex))
		b = append(b, countBuf[:]...)
		binary.BigEndian.PutUint32(countBuf[:], uint32(len(r.Eval)))
		b = append(b, countBuf[:]...)
		b = append(b, r.Eval...)
	}
	if len(b) > len(body) {
		return 0, 0, fmt.Errorf("oprf: eval response exceeds stack buffer: %w", syscall.ENOBUFS)
	}

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var hbuf [128]byte
	h := hbuf[:0]
	h = append(h, "HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: "...)
	h = strconv.AppendInt(h, int64(len(b)), 10)
	h = append(h, "\r\nConnection: close\r\n\r\n"...)
	if _, werr := m.conn.Write(h); werr != nil {
		return 0, 0, fmt.Errorf("oprf: failed to write response header: %v: %w", werr, syscall.EPIPE)
	}
	if _, werr := m.conn.Write(b); werr != nil {
		return 0, 0, fmt.Errorf("oprf: failed to write eval body: %v: %w", werr, syscall.EPIPE)
	}

	return 0, 0, ErrRequestHandled
}

// decodeStreamingPayload fully consumes an aws-chunked body, verifies the
// per-chunk SigV4 signatures (signed modes), spills the de-framed content to a
// bounded temp file, and resolves m.meta.Hash/Size to the decoded content
// hash/size. On failure it writes the appropriate S3 error response and
// returns a POSIX-mapped error so the connection is torn down cleanly.
func (m *S3Communicator) decodeStreamingPayload(req *http.Request) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 decodeStreamingPayload: %v", r)
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()

	mode := streamingModeOf(req.Header.Get("X-Amz-Content-Sha256"), req.Header.Get("Content-Encoding"))
	if mode == streamingNone {
		return nil
	}

	// Expected decoded size as declared by the SDK (X-Amz-Decoded-Content-Length).
	// Used to pre-check bounds and to reject smuggling/mismatches at the end.
	var expected int64 = -1
	if v := req.Header.Get(s3StreamingDecodedContentLength); v != "" {
		ev, parseErr := strconv.ParseInt(v, 10, 64)
		if parseErr != nil || ev < 0 {
			m.writeStreamingError(http.StatusBadRequest, "InvalidArgument", "Invalid X-Amz-Decoded-Content-Length.", "")
			return fmt.Errorf("invalid decoded content length %q: %w", v, syscall.EBADMSG)
		}
		expected = ev
	}
	if expected > common.MaxFileSize {
		m.writeStreamingError(http.StatusRequestEntityTooLarge, "EntityTooLarge", "Your proposed upload exceeds the maximum allowed object size.", "")
		return fmt.Errorf("decoded content length %d exceeds maximum: %w", expected, syscall.EOVERFLOW)
	}

	// Body-read deadline proportional to the decoded size (mirrors the server's
	// size-based absolute deadline) to prevent stalled/hanging uploads.
	if expected >= 0 {
		m.conn.SetReadDeadline(time.Now().Add(5*time.Minute + time.Duration(expected/(10*1024*1024))*time.Minute))
	} else {
		m.conn.SetReadDeadline(time.Now().Add(15 * time.Minute))
	}
	defer m.conn.SetReadDeadline(time.Time{})

	// Expect: 100-continue — emit the interim response before reading the body.
	if strings.EqualFold(req.Header.Get("Expect"), "100-continue") {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, werr := m.conn.Write([]byte("HTTP/1.1 100 Continue\r\n\r\n")); werr != nil {
			m.conn.SetWriteDeadline(time.Time{})
			return fmt.Errorf("failed to send 100-continue: %v: %w", werr, syscall.EPIPE)
		}
		m.conn.SetWriteDeadline(time.Time{})
	}

	// Signing context: signed modes require the verification material derived
	// during SigV4 verification; unsigned modes de-frame without per-chunk
	// checks (documented security posture).
	var ctx *streamingSigningCtx
	if mode == streamingSigned || mode == streamingSignedTrailer {
		ctx = m.sigV4
		if ctx == nil || len(ctx.signingKey) == 0 {
			m.writeStreamingError(http.StatusBadRequest, "InvalidRequest", "Signed aws-chunked streaming requires SigV4 authentication.", "")
			return fmt.Errorf("no signing context for signed streaming upload: %w", syscall.EACCES)
		}
	}
	trailers := mode == streamingSignedTrailer || mode == streamingUnsignedTrailer ||
		req.Header.Get(s3StreamingTrailerHeader) != ""

	dechunker := newAWSChunkedReader(m.reader, ctx, trailers, common.MaxFileSize)

	spill, spillErr := os.CreateTemp("", "momo-aws-chunked-*")
	if spillErr != nil {
		m.writeStreamingError(http.StatusInternalServerError, "InternalError", "Failed to buffer streaming payload.", "")
		return fmt.Errorf("failed to create streaming spill file: %w", syscall.EIO)
	}
	spillName := spill.Name()
	success := false
	defer func() {
		if !success {
			spill.Close()
			os.Remove(spillName)
		}
	}()

	if _, copyErr := io.Copy(spill, dechunker); copyErr != nil {
		switch {
		case errors.Is(copyErr, errStreamingSignatureMismatch):
			m.writeStreamingError(http.StatusForbidden, "SignatureDoesNotMatch", "The chunk signature we calculated does not match the signature you provided.", "")
			return fmt.Errorf("%w: streaming chunk signature mismatch", syscall.EACCES)
		case errors.Is(copyErr, syscall.EOVERFLOW):
			m.writeStreamingError(http.StatusRequestEntityTooLarge, "EntityTooLarge", "Your proposed upload exceeds the maximum allowed object size.", "")
			return fmt.Errorf("streaming payload exceeds maximum size: %w", syscall.EOVERFLOW)
		default:
			m.writeStreamingError(http.StatusBadRequest, "InvalidArgument", "Malformed aws-chunked body.", "")
			return fmt.Errorf("streaming decode failed: %v: %w", copyErr, syscall.EBADMSG)
		}
	}

	decodedSize := dechunker.DecodedSize()
	if expected >= 0 && decodedSize != expected {
		m.writeStreamingError(http.StatusBadRequest, "InvalidArgument", "The decoded content length does not match X-Amz-Decoded-Content-Length.", "")
		return fmt.Errorf("decoded length %d != declared %d: %w", decodedSize, expected, syscall.EBADMSG)
	}

	// Rewind the spill and hand it to Read() so the server pipeline consumes
	// only de-framed content keyed by the real content hash.
	if _, seekErr := spill.Seek(0, io.SeekStart); seekErr != nil {
		m.writeStreamingError(http.StatusInternalServerError, "InternalError", "Failed to rewind buffered streaming payload.", "")
		return fmt.Errorf("failed to rewind streaming spill: %w", syscall.EIO)
	}

	m.meta.Hash = dechunker.ContentHash()
	m.meta.Size = decodedSize
	m.streamingSpill = spill
	m.streamingReader = spill
	m.streamingPayload = true
	success = true
	return nil
}

// writeStreamingError writes an S3 error response with a bounded write deadline.
func (m *S3Communicator) writeStreamingError(status int, code, msg, resource string) {
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeS3Error(m.conn, status, code, msg, resource)
	m.conn.SetWriteDeadline(time.Time{})
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

	// 🛡️ Sentinel: Reject carriage returns or line feeds in the path to prevent HTTP Request Smuggling (CRLF Injection).
	if strings.ContainsAny(wireName, "\r\n") {
		return 0, fmt.Errorf("invalid characters in path: %w", syscall.EBADMSG)
	}

	for _, part := range strings.Split(wireName, "/") {
		if common.HasPathTraversalChars(part) {
			return 0, fmt.Errorf("path traversal in wireName: %w", syscall.EBADMSG)
		}
	}

	// 🛡️ Sentinel: Validate hash, auth token, and host for CRLF to prevent HTTP header injection.
	if strings.ContainsAny(meta.Hash, "\r\n") {
		return 0, fmt.Errorf("invalid characters in hash: %w", syscall.EBADMSG)
	}
	if strings.ContainsAny(m.clientAuthToken, "\r\n") {
		return 0, fmt.Errorf("invalid characters in auth token: %w", syscall.EBADMSG)
	}
	if strings.ContainsAny(host, "\r\n") {
		return 0, fmt.Errorf("invalid characters in host: %w", syscall.EBADMSG)
	}

	// ⚡ Bolt: Eliminate fmt.Sprintf and string allocations using stack-allocated buffer
	var buf [16384]byte
	b := buf[:0]
	b = append(b, "PUT /"...)
	if meta.RemotePath != "" {
		norm, _ := common.NormalizeVirtualPath(meta.RemotePath)
		b = append(b, norm...)
		b = append(b, '/')
	}
	b = append(b, common.TrimNullBytesFromString(meta.Name)...)
	b = append(b, " HTTP/1.1\r\nHost: "...)
	b = append(b, host...)
	b = append(b, "\r\nAuthorization: Bearer "...)
	b = append(b, m.clientAuthToken...)
	b = append(b, "\r\nX-Momo-Timestamp: "...)
	b = strconv.AppendInt(b, m.clientTimestamp, 10)
	b = append(b, "\r\nX-Amz-Content-Sha256: "...)
	b = append(b, common.TrimNullBytesFromString(meta.Hash)...)
	b = append(b, "\r\nContent-Length: "...)
	b = strconv.AppendInt(b, meta.Size, 10)

	// Carry preserved S3 object headers to forwarding peers as a single additive
	// base64-encoded JSON header. Additive only: the momo wire framing fields
	// (Name/Hash/Size/Timestamp) stay unchanged, and peers without support simply
	// ignore the header.
	if len(meta.S3Headers) > 0 {
		data, err := common.MarshalS3MetaJSON(meta.S3Headers)
		if err != nil {
			return 0, err
		}
		enc := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
		base64.StdEncoding.Encode(enc, data)
		b = append(b, "\r\nX-Momo-S3-Meta: "...)
		b = append(b, enc...)
	}

	b = append(b, "\r\n\r\n"...)

	// 🛡️ Zero-Crash: Defensive bounds check to verify the formatted content fits safely within the stack buffer
	if len(b) > 16384 {
		return 0, fmt.Errorf("buffer overflow: formatted data exceeds stack capacity: %w", syscall.ENOBUFS)
	}

	if _, err = m.conn.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write metadata request: %v: %w", err, syscall.EPIPE)
	}

	// ⚡ Bolt: Read the response immediately to get the metadata status.
	// 🛡️ Zero-Crash: Set a read deadline to prevent the client from blocking
	// indefinitely on an unresponsive server (issue #620).
	m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout))
	resp, err := http.ReadResponse(m.reader, nil)
	m.conn.SetReadDeadline(time.Time{})
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
		m.connReader.SetLimit(65536)                                // 🛡️ Bounded Network Loop/Read (Rule 24)
		m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout)) // 🛡️ Slowloris mitigation (issue #592)
		req, err := http.ReadRequest(m.reader)
		m.conn.SetReadDeadline(time.Time{})
		m.connReader.ClearLimit()
		if err != nil {
			return common.FileMetadata{}, fmt.Errorf("ReceiveMetadata ReadRequest failed: %v: %w", err, syscall.EBADMSG)
		}
		if req.Method != "PUT" {
			req.Body.Close()
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
		if hash != "" && common.HasPathTraversalChars(hash) {
			return common.FileMetadata{}, fmt.Errorf("invalid hash: %s: %w", hash, syscall.EBADMSG)
		}
		m.meta.Hash = hash

		// issue #773: the OPTIONS/ReceiveMetadata handshake path cannot decode
		// an aws-chunked body (the streaming frame is only supported on the
		// full server PUT path). Reject streaming uploads here explicitly.
		if isStreamingLiteral(req.Header.Get("X-Amz-Content-Sha256")) ||
			strings.Contains(req.Header.Get("Content-Encoding"), s3StreamingContentEncoding) {
			writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest", "aws-chunked streaming uploads require the metadata handshake.", "")
			return common.FileMetadata{}, fmt.Errorf("streaming upload on handshake path is unsupported: %w", syscall.EBADMSG)
		}

		// OPTIONS-handshake flow: capture S3 metadata from the PUT as well.
		m.meta.S3Headers = collectS3Headers(req)
		if s3metaStr := req.Header.Get("X-Momo-S3-Meta"); s3metaStr != "" {
			if data, decErr := base64.StdEncoding.DecodeString(s3metaStr); decErr == nil {
				if headers, jsonErr := common.UnmarshalS3MetaJSON(data); jsonErr == nil && len(headers) > 0 {
					m.meta.S3Headers = headers
				}
			}
		}
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
	var idBuf [32]byte
	idBytes := strconv.AppendInt(idBuf[:0], int64(serverId), 10)
	bodyLength := 3 + len(idBytes)

	b = strconv.AppendInt(b, int64(bodyLength), 10)
	b = append(b, "\r\nConnection: keep-alive\r\n\r\nACK"...)
	b = append(b, idBytes...)

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
	// 🛡️ Zero-Crash: Set a read deadline before reading the ACK response to
	// prevent the client from blocking indefinitely on an unresponsive server (issue #620).
	m.conn.SetReadDeadline(time.Now().Add(s3ReadHeaderTimeout))
	resp, err := http.ReadResponse(m.reader, nil)
	m.conn.SetReadDeadline(time.Time{})
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

func (m *S3Communicator) IsExternalClient() bool {
	return m.isExternalClient
}

func (m *S3Communicator) IsPeer() bool {
	return m.isPeer
}

// extractS3BucketAndKey parses the bucket name and key path from an S3 HTTP request.
// It supports both virtual-host style and path-style S3 URL schemas.
func extractS3BucketAndKey(req *http.Request) (bucket string, key string) {
	// req.Host may include an explicit port (e.g. "mybucket.localhost:9000").
	// Strip it before host parsing so virtual-host detection still matches.
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
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

// formatLastModified renders a Unix-nano modification time in the
// S3 XML LastModified format (UTC with millisecond precision).
// A zero timestamp (unknown/modern fallback) renders as the Unix epoch.
func formatLastModified(modTime int64) string {
	return time.Unix(0, modTime).UTC().Format("2006-01-02T15:04:05.000Z")
}

// formatHTTPLastModified renders a Unix-nano modification time as an
// HTTP IMF-fixdate header value (RFC 7231), which AWS SDKs and aws-cli
// parse for the Last-Modified response header. A zero timestamp renders
// as the Unix epoch.
func formatHTTPLastModified(modTime int64) string {
	return time.Unix(0, modTime).UTC().Format(http.TimeFormat)
}

// FormatListBucketsXML constructs an S3-compliant ListBuckets
// (ListAllMyBucketsResult) XML response listing the configured single bucket.
// An empty configuredBucket yields an empty bucket list (legacy flat mode).
func FormatListBucketsXML(configuredBucket string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>momo</ID><DisplayName>momo</DisplayName></Owner><Buckets>`)
	if configuredBucket != "" {
		buf.WriteString(`<Bucket><Name>`)
		xmlEscape(&buf, configuredBucket)
		buf.WriteString(`</Name><CreationDate>2024-01-01T00:00:00.000Z</CreationDate></Bucket>`)
	}
	buf.WriteString(`</Buckets></ListAllMyBucketsResult>`)
	return buf.Bytes()
}

// FormatGetBucketLocationXML constructs an S3-compliant GetBucketLocation
// response. An empty region (us-east-1) is represented by an empty
// LocationConstraint element.
func FormatGetBucketLocationXML(region string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	xmlEscape(&buf, region)
	buf.WriteString(`</LocationConstraint>`)
	return buf.Bytes()
}

// Continuation-token kinds. The token is base64(kind byte + last emitted key).
// 'K' means the page ended on a Contents key (resume after that key);
// 'P' means it ended on a CommonPrefix (resume after the whole prefix group).
// This is deterministic and stable across s3-tcp/s3-quic and server restarts.
const (
	continuationKindKey     byte = 'K'
	continuationKindPrefix  byte = 'P'
	continuationKindUnknown byte = 0
)

func encodeContinuationToken(kind byte, key string) string {
	return base64.StdEncoding.EncodeToString(append([]byte{kind}, []byte(key)...))
}

// decodeContinuationToken parses a continuation token produced by
// encodeContinuationToken. The second return is the plain resume key.
func decodeContinuationToken(token string) (kind byte, key string, ok bool) {
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil || len(raw) == 0 {
		return continuationKindUnknown, "", false
	}
	return raw[0], string(raw[1:]), true
}

// FormatListObjectsV2XML constructs an S3-compliant ListObjectsV2 XML response
// using a pre-allocated bytes.Buffer to avoid excessive heap allocations (⚡ Bolt pattern).
// startAfter is the raw value of either the continuation-token or start-after query
// parameter; it selects the page to resume from. When the page is truncated the
// returned nextToken encodes the last emitted element and must be passed back as
// continuation-token on the next request. fetchOwner mirrors the AWS S3
// fetch-owner parameter (default false): when true an Owner element is emitted
// per key.
func FormatListObjectsV2XML(bucketName, prefix, delimiter string, maxKeys int, startAfter string, fetchOwner bool, files []common.FileMetadata) (xmlBytes []byte, nextToken string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in FormatListObjectsV2XML: %v", r)
			err = fmt.Errorf("panic in FormatListObjectsV2XML: %v: %w", r, syscall.EIO)
		}
	}()

	// 🛡️ Rule 35: Validate input strings for length limits (64 bytes) before writing to the bytes.Buffer.
	if len(bucketName) > 64 || len(prefix) > 64 || len(delimiter) > 64 || len(startAfter) > 1024 {
		return nil, "", fmt.Errorf("FormatListObjectsV2XML input length exceeds limit: %w", syscall.EINVAL)
	}

	// Determine the resume position from a continuation token (kind-aware) or a
	// plain start-after key (S3 treats start-after as an informational hint).
	var resumeKey string
	var resumePrefixMode bool
	if startAfter != "" {
		if kind, key, ok := decodeContinuationToken(startAfter); ok {
			resumeKey = key
			resumePrefixMode = kind == continuationKindPrefix
		} else {
			resumeKey = startAfter
		}
	}

	// Sort by key so pagination is deterministic regardless of the list source
	// (store.List is sorted, but scatter-gather merges may not be).
	sort.SliceStable(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	var buf bytes.Buffer
	var intBuf [32]byte
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
	buf.Write(strconv.AppendInt(intBuf[:0], int64(maxKeys), 10))
	buf.WriteString(`</MaxKeys>`)

	commonPrefixes := make(map[string]bool)
	keyCount := 0
	truncated := false
	// last emitted element (used to build the continuation token on truncation).
	lastKind := byte(continuationKindKey)
	lastToken := ""

	emitContents := func(file common.FileMetadata, key string) {
		buf.WriteString(`<Contents>`)
		buf.WriteString(`<Key>`)
		xmlEscape(&buf, key)
		buf.WriteString(`</Key>`)
		buf.WriteString(`<LastModified>`)
		buf.WriteString(formatLastModified(file.ModTime))
		buf.WriteString(`</LastModified>`)
		buf.WriteString(`<ETag>"`)
		xmlEscape(&buf, file.Hash)
		buf.WriteString(`"</ETag>`)
		if fetchOwner {
			buf.WriteString(`<Owner>`)
			buf.WriteString(`<ID>momo</ID>`)
			buf.WriteString(`<DisplayName>momo</DisplayName>`)
			buf.WriteString(`</Owner>`)
		}
		buf.WriteString(`<Size>`)
		buf.Write(strconv.AppendInt(intBuf[:0], file.Size, 10))
		buf.WriteString(`</Size>`)
		buf.WriteString(`<StorageClass>STANDARD</StorageClass>`)
		buf.WriteString(`</Contents>`)
	}

	for _, file := range files {
		// 🛡️ Sentinel: Validate that the metadata fields conform to the project's strict size limits (64 bytes)
		// to protect the XML buffer against oversized payloads or corrupted database inputs (Rule 32).
		if len(file.Name) > 64 || len(file.Hash) > 64 {
			log.Printf("WARNING: Skipping malformed metadata entry in FormatListObjectsV2XML (Name length: %d, Hash length: %d)", len(file.Name), len(file.Hash))
			continue
		}

		key := file.Name

		// Resume-after: skip everything at or before the resume position.
		if resumeKey != "" {
			if resumePrefixMode {
				// Skip the whole already-emitted CommonPrefix group.
				if strings.HasPrefix(key, resumeKey) || key < resumeKey {
					continue
				}
			} else {
				if key <= resumeKey {
					continue
				}
			}
		}

		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}

		if delimiter != "" {
			relativeKey := key[len(prefix):]
			delimIdx := strings.Index(relativeKey, delimiter)
			if delimIdx != -1 {
				subPrefix := prefix + relativeKey[:delimIdx+1]
				if commonPrefixes[subPrefix] {
					continue
				}
				if maxKeys > 0 && keyCount >= maxKeys {
					truncated = true
					break
				}
				commonPrefixes[subPrefix] = true
				lastKind = continuationKindPrefix
				lastToken = subPrefix
				keyCount++
				continue
			}
		}

		if maxKeys > 0 && keyCount >= maxKeys {
			truncated = true
			break
		}

		emitContents(file, key)
		lastKind = continuationKindKey
		lastToken = key
		keyCount++
	}

	// Emit CommonPrefixes in stable (sorted) order for reproducible output.
	sortedPrefixes := make([]string, 0, len(commonPrefixes))
	for cp := range commonPrefixes {
		sortedPrefixes = append(sortedPrefixes, cp)
	}
	sort.Strings(sortedPrefixes)
	for _, cp := range sortedPrefixes {
		buf.WriteString(`<CommonPrefixes>`)
		buf.WriteString(`<Prefix>`)
		xmlEscape(&buf, cp)
		buf.WriteString(`</Prefix>`)
		buf.WriteString(`</CommonPrefixes>`)
	}

	buf.WriteString(`<IsTruncated>`)
	if truncated {
		buf.WriteString(`true`)
	} else {
		buf.WriteString(`false`)
	}
	buf.WriteString(`</IsTruncated>`)

	buf.WriteString(`<KeyCount>`)
	buf.Write(strconv.AppendInt(intBuf[:0], int64(keyCount), 10))
	buf.WriteString(`</KeyCount>`)

	if truncated && lastToken != "" {
		nextToken = encodeContinuationToken(lastKind, lastToken)
		buf.WriteString(`<NextContinuationToken>`)
		xmlEscape(&buf, nextToken)
		buf.WriteString(`</NextContinuationToken>`)
	}

	buf.WriteString(`</ListBucketResult>`)
	return buf.Bytes(), nextToken, nil
}

// ⚡ Bolt: Optimize XML escaping by replacing byte-by-byte iteration with fast-path
// block writes using strings.IndexAny. This reduces loop overhead and leverages
// optimized standard library routines for finding target characters, improving performance.
func xmlEscape(buf *bytes.Buffer, s string) {
	for len(s) > 0 {
		i := strings.IndexAny(s, "&<>\"'")
		if i == -1 {
			buf.WriteString(s)
			break
		}
		buf.WriteString(s[:i])
		switch s[i] {
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
		}
		s = s[i+1:]
	}
}

// s3ErrorCode maps an HTTP status to the S3 XML error Code used in error bodies.
// https://docs.aws.amazon.com/AmazonS3/latest/API/ErrorResponses.html
func s3ErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "InvalidArgument"
	case http.StatusForbidden:
		return "AccessDenied"
	case http.StatusNotFound:
		return "NoSuchKey"
	case http.StatusMethodNotAllowed:
		return "MethodNotAllowed"
	case http.StatusConflict:
		return "BucketNotEmpty"
	case http.StatusRequestEntityTooLarge:
		return "EntityTooLarge"
	case http.StatusUnsupportedMediaType:
		return "InvalidRequest"
	case http.StatusInternalServerError:
		return "InternalError"
	case http.StatusServiceUnavailable:
		return "ServiceUnavailable"
	default:
		return "InternalError"
	}
}

// writeS3Error writes an S3-compliant XML <Error> response body with the given
// HTTP status, Code, Message, and optional Resource (bucket/key), followed by a
// Content-Type: application/xml header and Content-Length matching the body.
// It returns the number of bytes written. Callers set the write deadline first.
// s3standardHeaders lists the standard S3 object headers captured on PUT and
// echoed on GET/HEAD (AWS S3 semantics). x-amz-meta-* user headers are captured
// separately. x-amz-server-side-encryption is only ever present here with the
// value AES256 (validateSSEHeaders rejects everything else before capture).
var s3standardHeaders = []string{
	"Content-Type",
	"Cache-Control",
	"Content-Disposition",
	"Content-Encoding",
	"Expires",
	"X-Amz-Server-Side-Encryption",
}

// sanitizeS3HeaderValue bounds and strips CR/LF from a header value to prevent
// HTTP response splitting and oversized metadata (Rule 24).
func sanitizeS3HeaderValue(v string) string {
	if len(v) > 1024 {
		v = v[:1024]
	}
	v = common.ReplaceCRLF(v)
	return strings.TrimSpace(v)
}

// collectS3Headers captures S3 object metadata from a PUT request: the standard
// headers plus every x-amz-meta-* user header. Keys are stored canonicalized
// (lowercase); appendS3MetaHeaders emits them back on GET/HEAD.
func collectS3Headers(req *http.Request) map[string]string {
	var headers map[string]string
	addHeader := func(key, value string) {
		value = sanitizeS3HeaderValue(value)
		if value == "" {
			return
		}
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[key] = value
	}
	for _, h := range s3standardHeaders {
		if v := req.Header.Get(h); v != "" {
			addHeader(strings.ToLower(h), v)
		}
	}
	for name, values := range req.Header {
		if !strings.HasPrefix(name, "X-Amz-Meta-") {
			continue
		}
		key := "x-amz-meta-" + strings.ToLower(strings.TrimPrefix(name, "X-Amz-Meta-"))
		for _, v := range values {
			addHeader(key, v)
		}
	}
	return headers
}

// validateSSEHeaders enforces momo's honest SSE boundary (issue #776). The
// gateway honors the AWS S3 SSE contract at the surface: AES256 is accepted
// (momo encrypts objects at rest with its own AES-256-GCM envelope) and echoed
// on GET/HEAD; SSE-C customer keys and SSE-KMS are rejected with clear errors
// rather than silently ignored -- accepting them would claim a guarantee momo
// cannot provide. No customer-provided key is ever stored. On rejection the
// S3 error is written to the connection and a POSIX-mapped error returned.
func (m *S3Communicator) validateSSEHeaders(req *http.Request) error {
	// SSE-C: customer-provided keys are never accepted nor stored.
	for _, h := range []string{
		"X-Amz-Server-Side-Encryption-Customer-Algorithm",
		"X-Amz-Server-Side-Encryption-Customer-Key",
		"X-Amz-Server-Side-Encryption-Customer-Key-MD5",
	} {
		if v := req.Header.Get(h); v != "" {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest",
				"Server-side encryption with customer-provided keys (SSE-C) is not supported.", "")
			return fmt.Errorf("SSE-C request rejected: %w", syscall.EINVAL)
		}
	}

	sse := req.Header.Get("X-Amz-Server-Side-Encryption")
	switch {
	case sse == "" && req.Header.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id") == "":
		// No SSE requested: nothing to honor or echo.
		return nil
	case strings.EqualFold(sse, "aws:kms") || req.Header.Get("X-Amz-Server-Side-Encryption-Aws-Kms-Key-Id") != "":
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotImplemented, "NotImplemented",
			"Server-side encryption with AWS KMS (aws:kms) is not supported.", "")
		return fmt.Errorf("SSE-KMS request rejected: %w", syscall.ENOTSUP)
	case strings.EqualFold(sse, "AES256"):
		// Accepted: captured by collectS3Headers and echoed on GET/HEAD.
		return nil
	default:
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument",
			fmt.Sprintf("Unsupported server-side encryption algorithm %q.", sse), "")
		return fmt.Errorf("unsupported SSE algorithm %q: %w", sse, syscall.EINVAL)
	}
}

// headerKeysWithout returns the sorted header keys, excluding "content-type"
// (emitted explicitly with the resolved value).
func headerKeysWithout(headers map[string]string, exclude string) []string {
	keys := make([]string, 0, len(headers))
	for k := range headers {
		if k != exclude {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// appendS3MetaHeaders appends Content-Type plus any preserved S3 metadata
// header lines to the response buffer. contentType defaults to
// application/octet-stream when the object carries no stored Content-Type.
func appendS3MetaHeaders(b []byte, headers map[string]string, contentType string) []byte {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	b = append(b, "Content-Type: "...)
	b = append(b, contentType...)
	b = append(b, "\r\n"...)
	for _, k := range headerKeysWithout(headers, "content-type") {
		v := headers[k]
		if v != "" {
			b = append(b, k...)
			b = append(b, ": "...)
			b = append(b, v...)
			b = append(b, "\r\n"...)
		}
	}
	return b
}

// storedS3Meta retrieves the S3 object headers persisted at rest for key, or
// nil when the store does not support S3 metadata or none was recorded.
func (m *S3Communicator) storedS3Meta(key string) map[string]string {
	sm, ok := m.store.(interface {
		GetS3Meta(string) map[string]string
	})
	if !ok {
		return nil
	}
	return sm.GetS3Meta(key)
}

// handleCopyObject implements S3 CopyObject (PUT with x-amz-copy-source):
// the destination key becomes a new namespace alias via store.Get -> store.Put
// (CAS dedup at the storage layer), preserving the source's S3 metadata, and
// answers with CopyObjectResult XML. Handled entirely within HandshakeServer.
func (m *S3Communicator) handleCopyObject(bucket, key, copySource string) (int, int64, error) {
	// Destination bucket and key validation (mirrors the single-DELETE path).
	if !m.validBucket(bucket) {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
		return 0, 0, ErrRequestHandled
	}
	if key == "" {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Copy source must have a destination object key.", "")
		return 0, 0, ErrRequestHandled
	}

	// Decode and validate the copy-source (may be URL-escaped).
	srcPath := copySource
	if decoded, decErr := url.PathUnescape(copySource); decErr == nil {
		srcPath = decoded
	}
	srcPath = strings.TrimPrefix(srcPath, "/")
	if srcPath == "" || strings.Contains(srcPath, "..") || strings.Contains(srcPath, "\\") {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid copy source.", copySource)
		return 0, 0, ErrRequestHandled
	}

	// Source key: in bucket mode the first segment is the source bucket; in
	// legacy flat mode it is ignored (consistent with GET/HEAD use of `key`).
	srcKey := ""
	parts := strings.SplitN(srcPath, "/", 2)
	if m.configuredBucket != "" {
		if !m.validBucket(parts[0]) {
			m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", parts[0])
			return 0, 0, ErrRequestHandled
		}
		if len(parts) > 1 {
			srcKey = parts[1]
		}
	} else if len(parts) > 1 {
		srcKey = parts[1]
	} else {
		srcKey = parts[0]
	}
	if srcKey == "" {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Invalid copy source.", copySource)
		return 0, 0, ErrRequestHandled
	}

	rc, srcMeta, err := m.store.Get(srcKey)
	if err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err == syscall.ENOENT || os.IsNotExist(err) {
			writeS3Error(m.conn, http.StatusNotFound, "NoSuchKey", "The specified source key does not exist.", srcKey)
			return 0, 0, ErrRequestHandled
		}
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The request failed due to an internal error.", srcKey)
		return 0, 0, fmt.Errorf("failed to copy source file %q: %w", srcKey, err)
	}
	defer rc.Close()

	// store.Put with the source blob: content-augmented CAS dedups under the
	// existing hash and creates the destination namespace alias.
	if err := m.store.Put(key, srcMeta.Hash, srcMeta.Size, "", rc); err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The request failed due to an internal error.", key)
		return 0, 0, fmt.Errorf("failed to copy file %q: %w", key, err)
	}

	// Preserve the source's S3 object metadata on the new alias (issue #772).
	if srcHeaders := m.storedS3Meta(srcKey); len(srcHeaders) > 0 {
		if ps, ok := m.store.(interface {
			PutS3Meta(string, map[string]string) error
		}); ok {
			if pErr := ps.PutS3Meta(key, srcHeaders); pErr != nil {
				log.Printf("AUDIT: Error persisting S3 metadata for copy %s: %v", key, pErr)
			}
		}
	}

	xmlBytes := FormatCopyObjectResultXML(srcMeta.Hash, srcMeta.ModTime)
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var hbuf [256]byte
	hb := hbuf[:0]
	hb = append(hb, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
	hb = strconv.AppendInt(hb, int64(len(xmlBytes)), 10)
	hb = append(hb, "\r\nConnection: close\r\n\r\n"...)
	if _, err := m.conn.Write(hb); err != nil {
		return 0, 0, fmt.Errorf("failed to write CopyObject headers: %v: %w", err, syscall.EPIPE)
	}
	if _, err := m.conn.Write(xmlBytes); err != nil {
		return 0, 0, fmt.Errorf("failed to write CopyObject body: %v: %w", err, syscall.EPIPE)
	}
	return 0, 0, ErrRequestHandled
}

// maxDeleteBodyBytes bounds the DeleteObjects XML payload to prevent XML DoS
// (Rule 24). maxDeleteKeys bounds the per-request object count (AWS limit is 1000).
const (
	maxDeleteBodyBytes = 1 << 20
	maxDeleteKeys      = 1000
)

type s3DeleteObject struct {
	Key string `xml:"Key"`
}

type s3DeletePayload struct {
	Object []s3DeleteObject `xml:"Object"`
}

type s3DeleteError struct {
	Key     string
	Code    string
	Message string
}

// parseDeleteObjectsBody parses an AWS DeleteObjects <Delete> XML payload into
// its keys, capping the number of keys to prevent XML DoS.
func parseDeleteObjectsBody(body []byte) ([]string, error) {
	var payload s3DeletePayload
	if err := xml.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if len(payload.Object) > maxDeleteKeys {
		return nil, fmt.Errorf("DeleteObjects count %d exceeds limit %d: %w", len(payload.Object), maxDeleteKeys, syscall.E2BIG)
	}
	keys := make([]string, 0, len(payload.Object))
	for _, o := range payload.Object {
		if k := strings.TrimSpace(o.Key); k != "" {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// handleBatchDelete implements S3 DeleteObjects (POST /{bucket}?delete): each
// key goes through the single-DELETE path (lease -> store.Delete -> scatter-gather)
// and the per-key results are aggregated into DeleteResult XML (AWS returns 200
// even when keys are missing). No momo framing is emitted for the batch itself.
func (m *S3Communicator) handleBatchDelete(bucket string, req *http.Request) (int, int64, error) {
	if !m.validBucket(bucket) {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.", bucket)
		return 0, 0, ErrRequestHandled
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, maxDeleteBodyBytes+1))
	if err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidRequest", "Error parsing request body:", "")
		return 0, 0, ErrRequestHandled
	}
	req.Body.Close()
	if len(body) > maxDeleteBodyBytes {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The request body is too large.", "")
		return 0, 0, ErrRequestHandled
	}

	keys, err := parseDeleteObjectsBody(body)
	if err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "MalformedXML", "The XML you provided was not well-formed or did not validate against our published schema.", "")
		return 0, 0, ErrRequestHandled
	}

	deleted := make([]string, 0, len(keys))
	var errs []s3DeleteError
	for _, k := range keys {
		if strings.Contains(k, "..") || strings.Contains(k, "\\") {
			errs = append(errs, s3DeleteError{Key: k, Code: "InvalidArgument", Message: "Invalid key."})
			continue
		}
		var leaseErr error
		if m.leaseAcquirer != nil {
			leaseErr = m.leaseAcquirer.AcquireLease(k, 10*time.Second)
			if leaseErr == nil {
				defer m.leaseAcquirer.ReleaseLease(k)
			}
		}
		if leaseErr != nil {
			errs = append(errs, s3DeleteError{Key: k, Code: "ServiceUnavailable", Message: "The request could not be completed because the resource is locked by another request."})
			continue
		}
		if delErr := m.store.Delete(k); delErr != nil {
			errs = append(errs, s3DeleteError{Key: k, Code: "InternalError", Message: "The request failed due to an internal error."})
			continue
		}
		// Propagate delete to all peers via scatter-gather (best-effort).
		if m.deletePropagator != nil {
			if pErr := m.deletePropagator.PropagateDelete(k, 5*time.Second); pErr != nil {
				log.Printf("P2P delete propagation for %s partially failed: %v", common.SanitizeLog(k), pErr)
			}
		}
		deleted = append(deleted, k)
	}
	if m.metricsHook != nil {
		m.metricsHook.IncDeletes()
	}

	xmlBytes := FormatDeleteObjectsResultXML(deleted, errs)
	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var hbuf [256]byte
	hb := hbuf[:0]
	hb = append(hb, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
	hb = strconv.AppendInt(hb, int64(len(xmlBytes)), 10)
	hb = append(hb, "\r\nConnection: close\r\n\r\n"...)
	if _, err := m.conn.Write(hb); err != nil {
		return 0, 0, fmt.Errorf("failed to write DeleteObjects headers: %v: %w", err, syscall.EPIPE)
	}
	if _, err := m.conn.Write(xmlBytes); err != nil {
		return 0, 0, fmt.Errorf("failed to write DeleteObjects body: %v: %w", err, syscall.EPIPE)
	}
	return 0, 0, ErrRequestHandled
}

// FormatCopyObjectResultXML constructs the S3 CopyObject Success result body.
func FormatCopyObjectResultXML(etag string, modTime int64) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><CopyObjectResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><ETag>"`)
	xmlEscape(&buf, etag)
	buf.WriteString(`"</ETag><LastModified>`)
	buf.WriteString(formatLastModified(modTime))
	buf.WriteString(`</LastModified></CopyObjectResult>`)
	return buf.Bytes()
}

// FormatDeleteObjectsResultXML constructs the S3 DeleteObjects (DeleteResult)
// response listing per-key Deleted and Error entries.
func FormatDeleteObjectsResultXML(deleted []string, errs []s3DeleteError) []byte {
	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	for _, k := range deleted {
		buf.WriteString(`<Deleted><Key>`)
		xmlEscape(&buf, k)
		buf.WriteString(`</Key></Deleted>`)
	}
	for _, e := range errs {
		buf.WriteString(`<Error><Key>`)
		xmlEscape(&buf, e.Key)
		buf.WriteString(`</Key><Code>`)
		xmlEscape(&buf, e.Code)
		buf.WriteString(`</Code><Message>`)
		xmlEscape(&buf, e.Message)
		buf.WriteString(`</Message></Error>`)
	}
	buf.WriteString(`</DeleteResult>`)
	return buf.Bytes()
}

// parseS3Range parses a single AWS S3 Range header value (e.g. "bytes=0-9",
// "bytes=100-", "bytes=-500") against the object size and returns the inclusive
// byte span [start,end]. serveRange=true means the caller must answer with
// 206 Partial Content for that span. unsatisfiable=true means the request must
// be answered with 416 (the span cannot be satisfied for the object size).
// Unknown units and multi-range requests (which AWS S3 answers with the full
// object) return serveRange=false, unsatisfiable=false.
func parseS3Range(rangeHeader string, size int64) (start, end int64, serveRange, unsatisfiable bool) {
	hv := strings.TrimSpace(rangeHeader)
	if !strings.HasPrefix(hv, "bytes=") {
		return 0, 0, false, false // unknown unit: ignore the header, 200 full body
	}
	spec := hv[len("bytes="):]
	// AWS S3 only supports a single range; multi-range requests receive the full object.
	if strings.Contains(spec, ",") {
		return 0, 0, false, false
	}

	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, false // malformed: ignore the header
	}
	startStr, endStr := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])

	parseInt := func(s string) (int64, bool) {
		if s == "" {
			return 0, false
		}
		v, err := strconv.ParseInt(s, 10, 63)
		if err != nil || v < 0 {
			return 0, false
		}
		return v, true
	}

	switch {
	case startStr == "" && endStr == "":
		// "bytes=-" carries no span and is unsatisfiable.
		return 0, 0, false, true
	case startStr == "":
		// Suffix range: the last endStr bytes of the object.
		n, ok := parseInt(endStr)
		if !ok || n == 0 || size == 0 {
			return 0, 0, false, true
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true, false
	case endStr == "":
		// Open range: from startStr to the end of the object.
		s, ok := parseInt(startStr)
		if !ok || size == 0 || s >= size {
			return 0, 0, false, true
		}
		return s, size - 1, true, false
	default:
		s, ok1 := parseInt(startStr)
		e, ok2 := parseInt(endStr)
		if !ok1 || !ok2 || s >= size || s > e {
			return 0, 0, false, true
		}
		if e >= size {
			e = size - 1
		}
		return s, e, true, false
	}
}

// etagMatches reports whether the comma-separated entity-tag list in header
// matches the given etag (a raw hash without quotes). "*" matches every object.
// Weak comparison prefixes (W/) and surrounding double quotes are tolerated.
func etagMatches(header, etag string) bool {
	for _, item := range strings.Split(header, ",") {
		item = strings.TrimSpace(item)
		if item == "*" {
			return true
		}
		item = strings.TrimPrefix(item, "W/")
		item = strings.TrimSpace(item)
		item = strings.Trim(item, `"`)
		if item == etag {
			return true
		}
	}
	return false
}

// ifRangeMatches reports whether an If-Range header value (an entity-tag or an
// HTTP-date) still matches the object, permitting a 206 Partial Content reply.
func ifRangeMatches(ifRange, etag string, modTime int64) bool {
	val := strings.TrimSpace(ifRange)
	if strings.HasPrefix(val, `"`) || strings.HasPrefix(val, "W/") {
		return etagMatches(val, etag)
	}
	if t, err := http.ParseTime(val); err == nil {
		// The object is fresh when Last-Modified is not after the If-Range date.
		return !time.Unix(0, modTime).After(t)
	}
	return false
}

// writeRangeNotSatisfiable writes a 416 Range Not Satisfiable response with the
// S3 InvalidRange XML error body and a Content-Range: bytes */size header.
func writeRangeNotSatisfiable(w io.Writer, size int64, key string) (int, error) {
	var bodyBuf bytes.Buffer
	bodyBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	bodyBuf.WriteString(`<Error><Code>InvalidRange</Code><Message>The requested range cannot be satisfied.</Message><Resource>`)
	xmlEscape(&bodyBuf, key)
	bodyBuf.WriteString(`</Resource></Error>`)

	var hdrBuf [256]byte
	b := hdrBuf[:0]
	b = append(b, "HTTP/1.1 416 Range Not Satisfiable\r\nContent-Type: application/xml\r\nContent-Length: "...)
	b = strconv.AppendInt(b, int64(bodyBuf.Len()), 10)
	b = append(b, "\r\nContent-Range: bytes */"...)
	b = strconv.AppendInt(b, size, 10)
	b = append(b, "\r\nConnection: close\r\n\r\n"...)
	if _, err := w.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write 416 headers: %v: %w", err, syscall.EPIPE)
	}
	if _, err := w.Write(bodyBuf.Bytes()); err != nil {
		return 0, fmt.Errorf("failed to write 416 body: %v: %w", err, syscall.EPIPE)
	}
	return len(b) + bodyBuf.Len(), nil
}

func writeS3Error(w io.Writer, status int, code, message, resource string) (int, error) {
	// 🛡️ Sentinel: Bound attacker-controlled message/resource lengths and strip
	// CR/LF to prevent HTTP response splitting and oversized error bodies (Rule 24).
	if len(message) > 512 {
		message = message[:512]
	}
	message = common.ReplaceCRLF(message)
	if len(resource) > 1024 {
		resource = resource[:1024]
	}

	var bodyBuf bytes.Buffer
	bodyBuf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	bodyBuf.WriteString(`<Error>`)
	bodyBuf.WriteString(`<Code>`)
	xmlEscape(&bodyBuf, code)
	bodyBuf.WriteString(`</Code><Message>`)
	xmlEscape(&bodyBuf, message)
	bodyBuf.WriteString(`</Message>`)
	if resource != "" {
		bodyBuf.WriteString(`<Resource>`)
		xmlEscape(&bodyBuf, resource)
		bodyBuf.WriteString(`</Resource>`)
	}
	bodyBuf.WriteString(`</Error>`)

	var hdrBuf [256]byte
	b := hdrBuf[:0]
	b = append(b, "HTTP/1.1 "...)
	b = strconv.AppendInt(b, int64(status), 10)
	b = append(b, " "...)
	b = append(b, http.StatusText(status)...)
	b = append(b, "\r\nContent-Type: application/xml\r\nContent-Length: "...)
	b = strconv.AppendInt(b, int64(bodyBuf.Len()), 10)
	b = append(b, "\r\nConnection: close\r\n\r\n"...)

	if _, err := w.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write S3 error headers: %v: %w", err, syscall.EPIPE)
	}
	if _, err := w.Write(bodyBuf.Bytes()); err != nil {
		return 0, fmt.Errorf("failed to write S3 error body: %v: %w", err, syscall.EPIPE)
	}
	return len(b) + bodyBuf.Len(), nil
}

// ─── Multipart Upload Support (issue #764) ─────────────────────────────────

// multipartUpload tracks a single multipart upload session.
type multipartUpload struct {
	bucket    string
	key       string
	createdAt time.Time
	parts     []multipartPart
	mu        sync.Mutex
}

type multipartPart struct {
	partNumber int
	etag       string
	data       []byte
}

var (
	muUploads sync.Mutex
	uploads   = map[string]*multipartUpload{} // uploadId → session
)

// generateUploadID returns a unique upload ID string.
func generateUploadID() string {
	var buf [32]byte
	now := time.Now().UnixNano()
	binary.LittleEndian.PutUint64(buf[:8], uint64(now))
	rand.Read(buf[8:])
	return hex.EncodeToString(buf[:])
}

// WriteXMLResponse writes an HTTP 200 XML response to the connection.
func writeXMLResponse(w io.Writer, xmlBody []byte) (int, error) {
	var hdrBuf [256]byte
	b := hdrBuf[:0]
	b = append(b, "HTTP/1.1 200 OK\r\nContent-Type: application/xml\r\nContent-Length: "...)
	b = strconv.AppendInt(b, int64(len(xmlBody)), 10)
	b = append(b, "\r\nConnection: close\r\n\r\n"...)
	if _, err := w.Write(b); err != nil {
		return 0, fmt.Errorf("failed to write XML response headers: %v: %w", err, syscall.EPIPE)
	}
	if _, err := w.Write(xmlBody); err != nil {
		return 0, fmt.Errorf("failed to write XML response body: %v: %w", err, syscall.EPIPE)
	}
	return len(b) + len(xmlBody), nil
}

// ─── CreateMultipartUpload ─────────────────────────────────────────────────

func (m *S3Communicator) handleCreateMultipartUpload(bucket, key string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleCreateMultipartUpload: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	uploadID := generateUploadID()

	muUploads.Lock()
	uploads[uploadID] = &multipartUpload{
		bucket:    bucket,
		key:       key,
		createdAt: time.Now(),
		parts:     make([]multipartPart, 0),
	}
	muUploads.Unlock()

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<InitiateMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	writeXMLString(&buf, "Bucket", bucket)
	writeXMLString(&buf, "Key", key)
	writeXMLString(&buf, "UploadId", uploadID)
	buf.WriteString(`</InitiateMultipartUploadResult>`)

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeXMLResponse(m.conn, buf.Bytes())
	return 0, 0, ErrRequestHandled
}

// ─── UploadPart ────────────────────────────────────────────────────────────

func (m *S3Communicator) handleUploadPart(req *http.Request, bucket, key string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleUploadPart: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	q := req.URL.Query()
	uploadID := q.Get("uploadId")
	partStr := q.Get("partNumber")
	partNumber, err := strconv.Atoi(partStr)
	if err != nil || partNumber < 1 {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusBadRequest, "InvalidArgument", "Part number must be a positive integer.", "")
		return 0, 0, fmt.Errorf("invalid part number %q: %w", partStr, syscall.EINVAL)
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, 5<<30))
	if err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to read part body.", "")
		return 0, 0, fmt.Errorf("upload part read body: %w", err)
	}
	req.Body.Close()

	hasher := sha256.New()
	hasher.Write(body)
	etag := hex.EncodeToString(hasher.Sum(nil))

	muUploads.Lock()
	up, ok := uploads[uploadID]
	if !ok || up.bucket != bucket || up.key != key {
		muUploads.Unlock()
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.", "")
		return 0, 0, ErrRequestHandled
	}
	up.mu.Lock()
	up.parts = append(up.parts, multipartPart{partNumber: partNumber, etag: etag, data: body})
	up.mu.Unlock()
	muUploads.Unlock()

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var hdrBuf [256]byte
	b := hdrBuf[:0]
	b = append(b, "HTTP/1.1 200 OK\r\nETag: \""...)
	b = append(b, etag...)
	b = append(b, "\"\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"...)
	if _, err := m.conn.Write(b); err != nil {
		return 0, 0, fmt.Errorf("failed to write UploadPart response: %v: %w", err, syscall.EPIPE)
	}
	return 0, 0, ErrRequestHandled
}

// ─── CompleteMultipartUpload ───────────────────────────────────────────────

func (m *S3Communicator) handleCompleteMultipartUpload(req *http.Request, bucket, key string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleCompleteMultipartUpload: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	q := req.URL.Query()
	uploadID := q.Get("uploadId")

	muUploads.Lock()
	up, ok := uploads[uploadID]
	if !ok || up.bucket != bucket || up.key != key {
		muUploads.Unlock()
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.", "")
		return 0, 0, fmt.Errorf("upload %s not found: %w", uploadID, syscall.ENOENT)
	}
	delete(uploads, uploadID)
	muUploads.Unlock()

	up.mu.Lock()
	parts := up.parts
	up.mu.Unlock()

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].partNumber < parts[j].partNumber
	})

	var assembled bytes.Buffer
	for _, p := range parts {
		assembled.Write(p.data)
	}

	data := assembled.Bytes()
	hasher := sha256.New()
	hasher.Write(data)
	finalHash := hex.EncodeToString(hasher.Sum(nil))
	finalSize := int64(len(data))

	if m.store == nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "The storage store is not initialized.", "")
		return 0, 0, fmt.Errorf("storage store not initialized")
	}

	m.meta.Name = key
	m.meta.Hash = finalHash
	m.meta.Size = finalSize

	if err := m.store.Put(key, finalHash, finalSize, "", bytes.NewReader(data)); err != nil {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusInternalServerError, "InternalError", "Failed to store assembled object.", "")
		return 0, 0, fmt.Errorf("store.Put of assembled multipart object: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<CompleteMultipartUploadResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	writeXMLString(&buf, "Bucket", bucket)
	writeXMLString(&buf, "Key", key)
	buf.WriteString(`<ETag>"`)
	xmlEscape(&buf, finalHash)
	buf.WriteString(`"</ETag>`)
	buf.WriteString(`</CompleteMultipartUploadResult>`)

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeXMLResponse(m.conn, buf.Bytes())
	return 0, 0, ErrRequestHandled
}

// ─── AbortMultipartUpload ─────────────────────────────────────────────────

func (m *S3Communicator) handleAbortMultipartUpload(uploadID string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleAbortMultipartUpload: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	muUploads.Lock()
	delete(uploads, uploadID)
	muUploads.Unlock()

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	var hdrBuf [256]byte
	b := hdrBuf[:0]
	b = append(b, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"...)
	if _, err := m.conn.Write(b); err != nil {
		return 0, 0, fmt.Errorf("failed to write AbortMultipartUpload response: %v: %w", err, syscall.EPIPE)
	}
	return 0, 0, ErrRequestHandled
}

// ─── ListParts ─────────────────────────────────────────────────────────────

func (m *S3Communicator) handleListParts(bucket, key, uploadID string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleListParts: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	muUploads.Lock()
	up, ok := uploads[uploadID]
	muUploads.Unlock()
	if !ok || up.bucket != bucket || up.key != key {
		m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		writeS3Error(m.conn, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.", "")
		return 0, 0, ErrRequestHandled
	}

	up.mu.Lock()
	parts := make([]multipartPart, len(up.parts))
	copy(parts, up.parts)
	up.mu.Unlock()

	sort.Slice(parts, func(i, j int) bool {
		return parts[i].partNumber < parts[j].partNumber
	})

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<ListPartsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	writeXMLString(&buf, "Bucket", bucket)
	writeXMLString(&buf, "Key", key)
	writeXMLString(&buf, "UploadId", uploadID)
	writeXMLString(&buf, "StorageClass", "STANDARD")
	buf.WriteString(`<IsTruncated>false</IsTruncated>`)
	buf.WriteString(`<PartNumberMarker>0</PartNumberMarker>`)
	buf.WriteString(`<NextPartNumberMarker>0</NextPartNumberMarker>`)
	buf.WriteString(`<MaxParts>1000</MaxParts>`)

	// ⚡ Bolt: Eliminate dynamic string allocations and repeated formatting overhead
	var intBuf [32]byte
	tstr := time.Now().UTC().Format(time.RFC3339)
	for _, p := range parts {
		buf.WriteString(`<Part>`)
		buf.WriteString(`<PartNumber>`)
		buf.Write(strconv.AppendInt(intBuf[:0], int64(p.partNumber), 10))
		buf.WriteString(`</PartNumber>`)
		buf.WriteString(`<ETag>"`)
		xmlEscape(&buf, p.etag)
		buf.WriteString(`"</ETag>`)
		buf.WriteString(`<Size>`)
		buf.Write(strconv.AppendInt(intBuf[:0], int64(len(p.data)), 10))
		buf.WriteString(`</Size>`)
		buf.WriteString(`<LastModified>`)
		buf.WriteString(tstr)
		buf.WriteString(`</LastModified>`)
		buf.WriteString(`</Part>`)
	}
	buf.WriteString(`</ListPartsResult>`)

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeXMLResponse(m.conn, buf.Bytes())
	return 0, 0, ErrRequestHandled
}

// ─── ListMultipartUploads ─────────────────────────────────────────────────

func (m *S3Communicator) handleListMultipartUploads(bucket string) (requestedMode int, timestamp int64, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3 handleListMultipartUploads: %v", r)
			m.conn.Close()
			err = fmt.Errorf("internal S3 protocol panic: %w", syscall.EIO)
		}
	}()
	muUploads.Lock()
	type uploadEntry struct {
		id  string
		key string
		ts  time.Time
	}
	var entries []uploadEntry
	for id, up := range uploads {
		if up.bucket == bucket {
			entries = append(entries, uploadEntry{id: id, key: up.key, ts: up.createdAt})
		}
	}
	muUploads.Unlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].ts.Before(entries[j].ts)
	})

	var buf bytes.Buffer
	buf.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	buf.WriteString(`<ListMultipartUploadsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	writeXMLString(&buf, "Bucket", bucket)
	buf.WriteString(`<IsTruncated>false</IsTruncated>`)
	for _, e := range entries {
		buf.WriteString(`<Upload>`)
		writeXMLString(&buf, "Key", e.key)
		buf.WriteString(`<UploadId>`)
		xmlEscape(&buf, e.id)
		buf.WriteString(`</UploadId>`)
		buf.WriteString(`</Upload>`)
	}
	buf.WriteString(`</ListMultipartUploadsResult>`)

	m.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	writeXMLResponse(m.conn, buf.Bytes())
	return 0, 0, ErrRequestHandled
}

// writeXMLString writes an XML element with the given name and escaped value.
func writeXMLString(w *bytes.Buffer, name, value string) {
	w.WriteByte('<')
	w.WriteString(name)
	w.WriteByte('>')
	xmlEscape(w, value)
	w.WriteString("</")
	w.WriteString(name)
	w.WriteByte('>')
}
