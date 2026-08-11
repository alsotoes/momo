package transport

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

// buildPresignedRequest assembly helper: builds the raw HTTP/1.1 request bytes
// carrying SigV4 query-string credentials (presigned URL), with the signature
// computed over the canonical request that includes all X-Amz-* params except
// X-Amz-Signature.
func buildPresignedHTTPRequest(req *http.Request, payloadHash, secretKey string) string {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"

	signedHeaders := "host"
	presign := &url.Values{}
	presign.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	presign.Set("X-Amz-Credential", secretKey+"/"+dateStamp+"/"+region+"/s3/aws4_request")
	presign.Set("X-Amz-Date", amzDate)
	presign.Set("X-Amz-Expires", "3600")
	presign.Set("X-Amz-SignedHeaders", signedHeaders)

	req.URL = &url.URL{Scheme: "http", Host: "127.0.0.1:4440", Path: req.URL.Path, RawQuery: presign.Encode()}
	req.Host = "127.0.0.1:4440"

	canonicalRequest, err := buildCanonicalRequest(req, signedHeaders, payloadHash)
	if err != nil {
		panic(err)
	}
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(secretKey, dateStamp, region)
	q := req.URL.Query()
	q.Set("X-Amz-Signature", computeSignature(signingKey, stringToSign))
	req.URL.RawQuery = q.Encode()

	var sb strings.Builder
	sb.WriteString(req.Method + " " + req.URL.RequestURI() + " HTTP/1.1\r\n")
	sb.WriteString("Host: " + req.Host + "\r\n")
	if payloadHash != "" {
		sb.WriteString("X-Amz-Content-Sha256: " + payloadHash + "\r\n")
	}
	if req.ContentLength > 0 {
		sb.WriteString("Content-Length: " + strconv.FormatInt(req.ContentLength, 10) + "\r\n")
	}
	sb.WriteString("\r\n")
	return sb.String()
}

func TestS3Communicator_PresignedGET(t *testing.T) {
	defer verifyNoLeaks(t)

	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expected := []byte(common.PadString(token, common.AuthTokenLength))

	req := &http.Request{Method: "GET", URL: &url.URL{Path: "/bucket/hello.txt"}}
	rawReq := buildPresignedHTTPRequest(req, emptyStringSHA256, token)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(rawReq))
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	mock := &mockStore{
		getFunc: func(name string) (io.ReadCloser, common.FileMetadata, error) {
			return io.NopCloser(bytes.NewReader([]byte("data"))), common.FileMetadata{Name: "hello.txt", Hash: "hash1", Size: 4}, nil
		},
	}

	comm := NewS3Communicator(serverConn)
	comm.SetStore(mock)
	if _, _, err := comm.HandshakeServer(expected); err != ErrRequestHandled {
		t.Fatalf("expected ErrRequestHandled for presigned GET, got: %v", err)
	}
}

func TestS3Communicator_PresignedPUT(t *testing.T) {
	defer verifyNoLeaks(t)

	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expected := []byte(common.PadString(token, common.AuthTokenLength))

	req := &http.Request{Method: "PUT", ContentLength: 5, URL: &url.URL{Path: "/bucket/upload.txt"}}
	rawReq := buildPresignedHTTPRequest(req, s3UnsignedPayload, token)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(rawReq + "hello"))
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	if _, _, err := comm.HandshakeServer(expected); err != nil {
		t.Fatalf("presigned PUT should pass auth and accept upload, got: %v", err)
	}
}

func TestS3Communicator_PresignedExpired(t *testing.T) {
	defer verifyNoLeaks(t)

	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expected := []byte(common.PadString(token, common.AuthTokenLength))

	req := &http.Request{Method: "GET", URL: &url.URL{Path: "/bucket/hello.txt"}}

	// Build a presigned URL signed 10 minutes in the past with a 60s window —
	// already outside the validity window even though the signature is correct.
	now := time.Now().UTC().Add(-10 * time.Minute)
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"
	signedHeaders := "host"
	payloadHash := emptyStringSHA256

	presign := &url.Values{}
	presign.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	presign.Set("X-Amz-Credential", token+"/"+dateStamp+"/"+region+"/s3/aws4_request")
	presign.Set("X-Amz-Date", amzDate)
	presign.Set("X-Amz-Expires", "60")
	presign.Set("X-Amz-SignedHeaders", signedHeaders)

	req.URL = &url.URL{Scheme: "http", Host: "127.0.0.1:4440", Path: req.URL.Path, RawQuery: presign.Encode()}
	req.Host = "127.0.0.1:4440"

	canonicalRequest, err := buildCanonicalRequest(req, signedHeaders, payloadHash)
	if err != nil {
		t.Fatalf("buildCanonicalRequest: %v", err)
	}
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(token, dateStamp, region)
	q := req.URL.Query()
	q.Set("X-Amz-Signature", computeSignature(signingKey, stringToSign))
	req.URL.RawQuery = q.Encode()

	rawReq := "GET " + req.URL.RequestURI() + " HTTP/1.1\r\nHost: 127.0.0.1:4440\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(rawReq))
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err = comm.HandshakeServer(expected)
	if err == nil {
		t.Fatal("expired presigned request should be rejected")
	}
	if !errors.Is(err, syscall.EACCES) {
		t.Fatalf("expected EACCES for expired presigned request, got: %v", err)
	}
}

func TestS3Communicator_PresignedMissingParam(t *testing.T) {
	defer verifyNoLeaks(t)

	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret
	expected := []byte(common.PadString(token, common.AuthTokenLength))

	// X-Amz-Algorithm present but no other query-string auth params → 400.
	rawReq := "GET /bucket/hello.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256 HTTP/1.1\r\nHost: 127.0.0.1:4440\r\n\r\n"

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	go func() {
		clientConn.Write([]byte(rawReq))
		buf := make([]byte, 4096)
		for {
			if _, err := clientConn.Read(buf); err != nil {
				return
			}
		}
	}()

	comm := NewS3Communicator(serverConn)
	_, _, err := comm.HandshakeServer(expected)
	if err == nil {
		t.Fatal("presigned request with missing auth params should be rejected")
	}
	if !errors.Is(err, syscall.EBADMSG) {
		t.Fatalf("expected EBADMSG for missing presigned params, got: %v", err)
	}
}

func TestS3Communicator_PresignedDELETE(t *testing.T) {
	defer verifyNoLeaks(t)

	// runS3RequestCapture uses this fixed token both as the client access key
	// and as the server secret, so presign over it matches the credential.
	token := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"

	deletedKey := ""
	mock := &mockStore{
		deleteFunc: func(name string) error {
			deletedKey = name
			return nil
		},
	}

	req := &http.Request{Method: "DELETE", URL: &url.URL{Path: "/bucket/presigndel.txt"}}
	rawReq := buildPresignedHTTPRequest(req, emptyStringSHA256, token)

	respStr, serverErr := runS3RequestCapture(t, rawReq, mock)
	if serverErr != ErrRequestHandled {
		t.Fatalf("expected ErrRequestHandled for presigned DELETE, got: %v", serverErr)
	}
	if deletedKey != "presigndel.txt" {
		t.Errorf("expected store.Delete('presigndel.txt'), got %q", deletedKey)
	}
	if !strings.Contains(respStr, "HTTP/1.1 204 No Content") {
		t.Errorf("expected 204 No Content, got: %s", respStr)
	}
}
