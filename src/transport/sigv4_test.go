package transport

import (
	"net/http"
	"net/url"
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
