package transport

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func buildSigV4Request(t *testing.T, amzDate, dateStamp, region, secretKey string) (*http.Request, string) {
	t.Helper()
	payloadHash := "dummyhash"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := "PUT\n/test-file.txt\n\nhost:127.0.0.1:4440\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n\n" + signedHeaders + "\n" + payloadHash
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(secretKey, dateStamp, region)
	signature := computeSignature(signingKey, stringToSign)

	authHeader := "AWS4-HMAC-SHA256 Credential=" + secretKey + "/" + dateStamp + "/" + region + "/s3/aws4_request, SignedHeaders=" + signedHeaders + ", Signature=" + signature

	req := &http.Request{
		Method: "PUT",
		Host:   "127.0.0.1:4440",
		Header: http.Header{},
		URL:    &url.URL{Path: "/test-file.txt"},
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	return req, authHeader
}

func TestVerifySigV4Timestamp_Fresh(t *testing.T) {
	amzDate := time.Now().UTC().Format("20060102T150405Z")
	if !verifySigV4Timestamp(amzDate) {
		t.Fatal("fresh timestamp should be accepted")
	}
}

func TestVerifySigV4Timestamp_Stale(t *testing.T) {
	amzDate := time.Now().UTC().Add(-30 * time.Minute).Format("20060102T150405Z")
	if verifySigV4Timestamp(amzDate) {
		t.Fatal("stale timestamp (30m old) should be rejected")
	}
}

func TestVerifySigV4Timestamp_Future(t *testing.T) {
	amzDate := time.Now().UTC().Add(30 * time.Minute).Format("20060102T150405Z")
	if verifySigV4Timestamp(amzDate) {
		t.Fatal("future timestamp (30m ahead) should be rejected")
	}
}

func TestVerifySigV4Timestamp_Malformed(t *testing.T) {
	if verifySigV4Timestamp("not-a-date") {
		t.Fatal("malformed timestamp should be rejected")
	}
}

func TestVerifySigV4Timestamp_Boundary(t *testing.T) {
	original := sigV4MaxSkew
	defer func() { sigV4MaxSkew = original }()
	sigV4MaxSkew = 5 * time.Minute

	amzDate := time.Now().UTC().Add(-4 * time.Minute).Format("20060102T150405Z")
	if !verifySigV4Timestamp(amzDate) {
		t.Fatal("timestamp within skew boundary should be accepted")
	}

	amzDate = time.Now().UTC().Add(-6 * time.Minute).Format("20060102T150405Z")
	if verifySigV4Timestamp(amzDate) {
		t.Fatal("timestamp beyond skew boundary should be rejected")
	}
}

func TestVerifySigV4Signature_ReplayAttack(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	staleTime := time.Now().UTC().Add(-1 * time.Hour)
	amzDate := staleTime.Format("20060102T150405Z")
	dateStamp := staleTime.Format("20060102")
	region := "us-east-1"

	req, authHeader := buildSigV4Request(t, amzDate, dateStamp, region, secretKey)
	if verifySigV4Signature(req, authHeader, secretKey) {
		t.Fatal("replay attack: stale signed request should be rejected")
	}
}

func TestVerifySigV4Signature_FreshRequest(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"

	req, authHeader := buildSigV4Request(t, amzDate, dateStamp, region, secretKey)
	if !verifySigV4Signature(req, authHeader, secretKey) {
		t.Fatal("fresh signed request should be accepted")
	}
}

// TestVerifySigV4Signature_RequiresAmzDateHeader ensures the header-signed
// SigV4 path rejects a request without X-Amz-Date instead of falling back to a
// field that parseSigV4AuthHeader never populates (issue #654).
func TestVerifySigV4Signature_RequiresAmzDateHeader(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"

	req, authHeader := buildSigV4Request(t, amzDate, dateStamp, region, secretKey)
	req.Header.Del("X-Amz-Date")
	if verifySigV4Signature(req, authHeader, secretKey) {
		t.Fatal("header-signed request without X-Amz-Date should be rejected")
	}
}

// buildPresignedSigV4Request constructs a presigned (query-string SigV4) request
// for the given method/path with a validity window and a payload hash. The
// signature is computed over the canonical query that already contains all
// X-Amz-* params, then X-Amz-Signature is appended (and must be excluded from
// the server-side canonical request).
func buildPresignedSigV4Request(t *testing.T, method, rawPath string, signedHeaders []string, payloadHash, secretKey string, expires int) *http.Request {
	t.Helper()

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"

	signedHeadersStr := strings.Join(signedHeaders, ";")

	presign := &url.Values{}
	presign.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	presign.Set("X-Amz-Credential", secretKey+"/"+dateStamp+"/"+region+"/s3/aws4_request")
	presign.Set("X-Amz-Date", amzDate)
	presign.Set("X-Amz-Expires", strconv.Itoa(expires))
	presign.Set("X-Amz-SignedHeaders", signedHeadersStr)

	rawQuery := presign.Encode()

	req := &http.Request{
		Method: method,
		Host:   "127.0.0.1:4440",
		Header: http.Header{},
		URL:    &url.URL{Scheme: "http", Host: "127.0.0.1:4440", Path: rawPath, RawQuery: rawQuery},
	}
	for _, h := range signedHeaders {
		switch h {
		case "host":
			// host is derived from req.Host
		case "x-amz-content-sha256":
			req.Header.Set("X-Amz-Content-Sha256", payloadHash)
		case "x-amz-date":
			req.Header.Set("X-Amz-Date", amzDate)
		default:
			req.Header.Set(h, "value")
		}
	}

	canonicalRequest, err := buildCanonicalRequest(req, signedHeadersStr, payloadHash)
	if err != nil {
		t.Fatalf("buildCanonicalRequest: %v", err)
	}
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(secretKey, dateStamp, region)
	sig := computeSignature(signingKey, stringToSign)

	q := req.URL.Query()
	q.Set("X-Amz-Signature", sig)
	req.URL.RawQuery = q.Encode()

	return req
}

func TestVerifySigV4Signature_PresignedGET(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	req := buildPresignedSigV4Request(t, "GET", "/test-file.txt", []string{"host"}, emptyStringSHA256, secretKey, 3600)
	if !isPresignedSigV4(req) {
		t.Fatal("request should be detected as presigned")
	}
	if !verifySigV4Signature(req, "", secretKey) {
		t.Fatal("valid presigned GET should be accepted")
	}
}

func TestVerifySigV4Signature_PresignedPUT(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	req := buildPresignedSigV4Request(t, "PUT", "/test-file.txt", []string{"host"}, s3UnsignedPayload, secretKey, 3600)
	if !verifySigV4Signature(req, "", secretKey) {
		t.Fatal("valid presigned PUT should be accepted")
	}
}

func TestVerifySigV4Signature_PresignedWrongSecret(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	wrongKey := "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"
	req := buildPresignedSigV4Request(t, "GET", "/test-file.txt", []string{"host"}, emptyStringSHA256, secretKey, 3600)
	if verifySigV4Signature(req, "", wrongKey) {
		t.Fatal("presigned request signed with a different secret should be rejected")
	}
}

func TestVerifySigV4Signature_PresignedExpired(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	req := buildPresignedSigV4Request(t, "GET", "/test-file.txt", []string{"host"}, emptyStringSHA256, secretKey, 1)

	// Rewrite the signature date to the past so the URL is already outside its
	// X-Amz-Date + X-Amz-Expires validity window.
	buildPresigned := buildPresignedSigV4Request
	_ = buildPresigned
	now := time.Now().UTC().Add(-10 * time.Second)
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	region := "us-east-1"

	presign := &url.Values{}
	presign.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	presign.Set("X-Amz-Credential", secretKey+"/"+dateStamp+"/"+region+"/s3/aws4_request")
	presign.Set("X-Amz-Date", amzDate)
	presign.Set("X-Amz-Expires", "1")
	presign.Set("X-Amz-SignedHeaders", "host")

	req = &http.Request{
		Method: "GET",
		Host:   "127.0.0.1:4440",
		Header: http.Header{},
		URL:    &url.URL{Scheme: "http", Host: "127.0.0.1:4440", Path: "/test-file.txt", RawQuery: presign.Encode()},
	}
	canonicalRequest, err := buildCanonicalRequest(req, "host", emptyStringSHA256)
	if err != nil {
		t.Fatalf("buildCanonicalRequest: %v", err)
	}
	stringToSign := buildStringToSign(canonicalRequest, amzDate, dateStamp, region)
	signingKey := deriveSigningKey(secretKey, dateStamp, region)
	q := req.URL.Query()
	q.Set("X-Amz-Signature", computeSignature(signingKey, stringToSign))
	req.URL.RawQuery = q.Encode()

	if verifySigV4Signature(req, "", secretKey) {
		t.Fatal("expired presigned request should be rejected")
	}
	if verifySigV4Expiry(req) {
		t.Fatal("request outside its validity window should be reported as expired")
	}
}

func TestVerifySigV4Signature_PresignedRejectedOnTamper(t *testing.T) {
	secretKey := "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6"
	req := buildPresignedSigV4Request(t, "GET", "/test-file.txt", []string{"host"}, emptyStringSHA256, secretKey, 3600)

	// Tamper with a signed query param: the signature must no longer match.
	req.URL.RawQuery = req.URL.RawQuery + "&X-Amz-Expires=99999"
	if verifySigV4Signature(req, "", secretKey) {
		t.Fatal("tampered presigned request should be rejected")
	}
}

func TestBuildCanonicalQueryString_ExcludesSignature(t *testing.T) {
	values := url.Values{}
	values.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	values.Set("X-Amz-Signature", "deadbeef")
	values.Set("X-Amz-Date", "20240101T000000Z")
	values.Set("foo", "bar")

	qs, err := buildCanonicalQueryString(values)
	if err != nil {
		t.Fatalf("buildCanonicalQueryString: %v", err)
	}
	if strings.Contains(qs, "X-Amz-Signature") {
		t.Fatalf("canonical query string must not contain X-Amz-Signature, got %q", qs)
	}
	if !strings.Contains(qs, "X-Amz-Algorithm=AWS4-HMAC-SHA256") {
		t.Fatalf("expected X-Amz-Algorithm in canonical query, got %q", qs)
	}
	if !strings.Contains(qs, "foo=bar") {
		t.Fatalf("expected foo=bar in canonical query, got %q", qs)
	}
}
