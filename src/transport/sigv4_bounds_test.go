package transport

import (
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestParseSigV4AuthHeader_Valid(t *testing.T) {
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	c, err := parseSigV4AuthHeader("AWS4-HMAC-SHA256 Credential=AKID/" + dateStamp +
		"/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-date, Signature=abc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.AccessKey != "AKID" || c.DateStamp != dateStamp || c.Region != "us-east-1" {
		t.Fatalf("credential fields wrong: %+v", c)
	}
	if c.SignedHeaders != "host;x-amz-date" || c.Signature != "abc123" {
		t.Fatalf("signed fields wrong: %+v", c)
	}
}

func TestParseSigV4AuthHeader_Malformed(t *testing.T) {
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]

	tests := map[string]string{
		"wrong scheme":           "Bearer token",
		"empty":                  "",
		"missing scope slash":    "AWS4-HMAC-SHA256 Credential=AKID/" + dateStamp + "/s3/aws4_request, SignedHeaders=host, Signature=abc",
		"missing region slash":   "AWS4-HMAC-SHA256 Credential=AKID/" + dateStamp + "/us-east-1aws4_request, SignedHeaders=host, Signature=abc",
		"missing terminal slash": "AWS4-HMAC-SHA256 Credential=AKID/" + dateStamp + "/us-east-1s3_request, SignedHeaders=host, Signature=abc",
		"missing parts":          "AWS4-HMAC-SHA256 Credential=AKID/" + dateStamp + "/r/s3/aws4_request",
		"empty access key":       "AWS4-HMAC-SHA256 Credential=/" + dateStamp + "/r/s3/aws4_request, SignedHeaders=host, Signature=abc",
	}

	for name, header := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSigV4AuthHeader(header); err == nil || err.Error() != syscall.EBADMSG.Error() {
				t.Fatalf("got %v, want %v", err, syscall.EBADMSG)
			}
		})
	}
}

// TestHandshakeServer_AuthHeaderBound proves the Sentinel #1231 surface is
// bounded: HandshakeServer caps the whole handshake read at 64 KiB
// (LimitedConnReader, Rule 24), so no SigV4 parser ever scans an unbounded
// Authorization header.
func TestHandshakeServer_AuthHeaderBound(t *testing.T) {
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	dateStamp := amzDate[:8]
	region := "us-east-1"
	payloadHash := "dummyhash"
	signedHeaders := "host;x-amz-date"

	authToken := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expectedAuthToken := []byte(common.PadString(authToken, common.AuthTokenLength))

	canonicalRequest := "PUT\n/test-file.txt\n\nhost:127.0.0.1:4440\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(authToken, dateStamp, region)
	signature := computeSignature(signingKey, stringToSign)

	base := "AWS4-HMAC-SHA256 Credential=" + authToken + "/" + dateStamp + "/" + region +
		"/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature

	t.Run("oversized header rejected by handshake read cap", func(t *testing.T) {
		authHeader := base + strings.Repeat("x", 70000-len(base))
		err := runHandshake(t, handshakeRequest(amzDate, authHeader, payloadHash), expectedAuthToken)
		if err == nil {
			t.Fatal("expected error for oversized authorization header")
		}
		if !strings.Contains(err.Error(), "failed to read handshake request") || !strings.Contains(err.Error(), syscall.EBADMSG.Error()) {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid header accepted", func(t *testing.T) {
		if err := runHandshake(t, handshakeRequest(amzDate, base, payloadHash), expectedAuthToken); err != nil {
			t.Fatalf("HandshakeServer failed for valid header: %v", err)
		}
	})
}

func handshakeRequest(amzDate, authHeader, payloadHash string) string {
	return "PUT /test-file.txt HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: " + authHeader + "\r\n" +
		"X-Amz-Date: " + amzDate + "\r\n" +
		"X-Amz-Content-Sha256: " + payloadHash + "\r\n" +
		"Content-Length: 1024\r\n\r\n"
}

func runHandshake(t *testing.T, reqBody string, expectedAuthToken []byte) error {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()
	defer func() { _ = serverConn.Close() }()

	go func() {
		_, _ = clientConn.Write([]byte(reqBody))
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	_, _, err := NewS3Communicator(serverConn).HandshakeServer(expectedAuthToken)
	return err
}
