package transport

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// signedStreamingPUT builds the request header + aws-chunked body for a signed
// streaming PUT under the given bearer token/secret, returning them as a
// single wire-bytes slice plus the decoded content for assertions.
func signedStreamingPUT(authToken string, content []byte, decodedLenHeader string, corrupted bool) (wire []byte, decodedHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	payloadLiteral := s3StreamingSignedPayload
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	sum := sha256.Sum256(content)
	decodedHash = hex.EncodeToString(sum[:])

	canonicalRequest := "PUT\n/examplebucket/chunkObject.txt\n\n" +
		"host:127.0.0.1:4440\n" +
		"x-amz-content-sha256:" + payloadLiteral + "\n" +
		"x-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadLiteral
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	seedSig := computeSignature(signingKey, stringToSign)

	body := clientStreamingBody(content, seedSig, amzDate, scope, signingKey)
	if corrupted {
		idx := strings.Index(string(body), ";chunk-signature=")
		declared := string(body)[idx+len(";chunk-signature=") : idx+len(";chunk-signature=")+64]
		body = []byte(strings.Replace(string(body), declared, strings.Repeat("0", 64), 1))
	}

	if decodedLenHeader == "" {
		decodedLenHeader = fmt.Sprintf("%d", len(content))
	}

	hdr := "PUT /examplebucket/chunkObject.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + seedSig + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadLiteral + "\r\n" +
		"Content-Encoding: aws-chunked\r\n" +
		"x-amz-decoded-content-length: " + decodedLenHeader + "\r\n" +
		"Content-Length: " + fmt.Sprintf("%d", len(body)) + "\r\n\r\n"
	return append([]byte(hdr), body...), decodedHash
}

// clientStreamingBody builds an aws-chunked body for data split into two
// chunks and returns the framed wire bytes. Chunk signatures are computed the
// way AWS SDKs do: chained from the seed (request) signature using the derived
// signing key.
func clientStreamingBody(data []byte, seedSig, amzDate, scope string, signingKey []byte) []byte {
	var buf bytes.Buffer
	chunks := [][]byte{data[:65536], data[65536:]}
	prev := seedSig
	for _, chunk := range chunks {
		sum := sha256.Sum256(chunk)
		hashHex := hex.EncodeToString(sum[:])
		sig := stringToSignAndSign("AWS4-HMAC-SHA256-PAYLOAD", amzDate, scope, prev, awsChunkEmptySHA256, hashHex, signingKey)
		fmt.Fprintf(&buf, "%x;chunk-signature=%s\r\n", len(chunk), sig)
		buf.Write(chunk)
		buf.WriteString("\r\n")
		prev = sig
	}
	// terminating chunk
	sig := stringToSignAndSign("AWS4-HMAC-SHA256-PAYLOAD", amzDate, scope, prev, awsChunkEmptySHA256, awsChunkEmptySHA256, signingKey)
	fmt.Fprintf(&buf, "0;chunk-signature=%s\r\n\r\n", sig)
	return buf.Bytes()
}

func stringToSignAndSign(alg, amzDate, scope, prevSig, emptyHash, dataHash string, signingKey []byte) string {
	sts := alg + "\n" + amzDate + "\n" + scope + "\n" + prevSig + "\n" + emptyHash + "\n" + dataHash
	return hex.EncodeToString(hmacSHA256(signingKey, sts))
}

func TestS3Communicator_StreamingSignedPUT(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	content := bytes.Repeat([]byte{'a'}, 66560)

	wire, decodedHash := signedStreamingPUT(authToken, content, "", false)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write(wire)
		buf := make([]byte, 1024)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err != nil {
		t.Fatalf("HandshakeServer failed for signed streaming PUT: %v", err)
	}

	if !comm.streamingPayload {
		t.Fatal("streaming payload flag should be set after de-framing")
	}
	if comm.meta.Hash != decodedHash {
		t.Errorf("meta.Hash = %s, want %s", comm.meta.Hash, decodedHash)
	}
	if comm.meta.Size != int64(len(content)) {
		t.Errorf("meta.Size = %d, want %d", comm.meta.Size, len(content))
	}

	got, err := io.ReadAll(comm)
	if err != nil {
		t.Fatalf("reading de-framed content: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("de-framed content mismatch: got %d bytes want %d", len(got), len(content))
	}
}

func TestS3Communicator_StreamingPUT_BadChunkSig(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	content := bytes.Repeat([]byte{'a'}, 66560)

	wire, _ := signedStreamingPUT(authToken, content, "", true)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write(wire)
		buf := make([]byte, 1024)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err == nil {
		t.Fatal("expected HandshakeServer to reject corrupted chunk signature")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("expected EACCES for signature mismatch, got: %v", err)
	}
}

func TestS3Communicator_StreamingPUT_Expect100Continue(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	content := bytes.Repeat([]byte{'a'}, 66560)

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	scope := dateStamp + "/" + region + "/s3/aws4_request"
	payloadLiteral := s3StreamingSignedPayload
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := "PUT\n/examplebucket/chunkObject.txt\n\n" +
		"host:127.0.0.1:4440\n" +
		"x-amz-content-sha256:" + payloadLiteral + "\n" +
		"x-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadLiteral
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	seedSig := computeSignature(signingKey, stringToSign)
	body := clientStreamingBody(content, seedSig, amzDate, scope, signingKey)

	// Send headers with Expect: 100-continue, then wait for the interim
	// response before streaming the body (real SDK behavior).
	hdr := "PUT /examplebucket/chunkObject.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + seedSig + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadLiteral + "\r\n" +
		"Content-Encoding: aws-chunked\r\n" +
		"Expect: 100-continue\r\n" +
		"x-amz-decoded-content-length: " + fmt.Sprintf("%d", len(content)) + "\r\n" +
		"Content-Length: " + fmt.Sprintf("%d", len(body)) + "\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	gotContinue := make(chan bool, 1)
	go func() {
		clientConn.Write([]byte(hdr))
		buf := make([]byte, 64)
		n, _ := clientConn.Read(buf)
		gotContinue <- strings.Contains(string(buf[:n]), "100 Continue")
		clientConn.Write(body)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	if _, _, err := comm.HandshakeServer(expectedAuthToken); err != nil {
		t.Fatalf("HandshakeServer failed for streaming PUT with Expect: 100-continue: %v", err)
	}
	if !<-gotContinue {
		t.Fatal("client did not receive HTTP/1.1 100 Continue before the body")
	}
}

func TestS3Communicator_StreamingPUT_DecodedLengthMismatch(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	content := bytes.Repeat([]byte{'a'}, 66560)

	wire, _ := signedStreamingPUT(authToken, content, "66559", false)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write(wire)
		buf := make([]byte, 1024)
		for {
			_, err := clientConn.Read(buf)
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err == nil {
		t.Fatal("expected HandshakeServer to reject decoded-length mismatch")
	}
	if !errors.Is(err, syscall.EBADMSG) {
		t.Fatalf("expected EBADMSG for decoded-length mismatch, got: %v", err)
	}
}

func TestS3Communicator_StreamingPUT_Oversized(t *testing.T) {
	defer verifyNoLeaks(t)

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))
	content := bytes.Repeat([]byte{'a'}, 66560)

	// Declare a decoded length that exceeds common.MaxFileSize. The gateway
	// rejects before reading the body, so only the header block is sent.
	wire, _ := signedStreamingPUT(authToken, content, fmt.Sprintf("%d", common.MaxFileSize+1), false)
	header := wire[:bytes.Index(wire, []byte("\r\n\r\n"))+4]

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var resp bytes.Buffer
	go func() {
		clientConn.Write(header)
		buf := make([]byte, 2048)
		for {
			n, err := clientConn.Read(buf)
			resp.Write(buf[:n])
			if err != nil {
				break
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expectedAuthToken)
	if err == nil {
		t.Fatal("expected HandshakeServer to reject oversized upload")
	}
	if !errors.Is(err, syscall.EOVERFLOW) {
		t.Fatalf("expected EOVERFLOW for oversized upload, got: %v", err)
	}
	if !strings.Contains(resp.String(), "413") || !strings.Contains(resp.String(), "EntityTooLarge") {
		t.Fatalf("expected 413 EntityTooLarge in S3 error response, got: %s", resp.String())
	}
}

// TestS3Communicator_StreamingPUT_RejectedOnHandshakePath verifies that a
// streaming PUT via the OPTIONS/ReceiveMetadata handshake path (where chunk
// signatures cannot be authenticated) is rejected with 400 InvalidRequest.
func TestS3Communicator_StreamingPUT_RejectedOnHandshakePath(t *testing.T) {
	defer verifyNoLeaks(t)

	reqBody := "PUT /examplebucket/chunkObject.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer placeholder\r\n" +
		"X-Amz-Date: 20260604T120000Z\r\n" +
		"X-Amz-Content-Sha256: " + s3StreamingSignedPayload + "\r\n" +
		"Transfer-Encoding: chunked\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(reqBody))
		buf := make([]byte, 1024)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				break
			}
		}
	}()

	// Call ReceiveMetadata directly with no prior HandshakeServer PUT parsing,
	// simulating the OPTIONS-handshake flow where the PUT is the next request.
	comm := NewS3Communicator(serverConn)
	_, err := comm.ReceiveMetadata()
	if err == nil {
		t.Fatal("expected ReceiveMetadata to reject streaming PUT on the handshake path")
	}
	if !errors.Is(err, syscall.EBADMSG) {
		t.Fatalf("expected EBADMSG for streaming PUT on handshake path, got: %v", err)
	}
}
