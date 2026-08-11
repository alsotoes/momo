package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type sigV4Components struct {
	AccessKey     string
	DateStamp     string
	Region        string
	SignedHeaders string
	Signature     string
	AmzDate       string
}

func parseSigV4AuthHeader(authHeader string) (sigV4Components, bool) {
	if !strings.HasPrefix(authHeader, "AWS4-HMAC-SHA256 ") {
		return sigV4Components{}, false
	}

	rest := strings.TrimPrefix(authHeader, "AWS4-HMAC-SHA256 ")
	parts := strings.Split(rest, ", ")
	if len(parts) < 3 {
		return sigV4Components{}, false
	}

	var c sigV4Components
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "Credential=") {
			cred := strings.TrimPrefix(part, "Credential=")
			credParts := strings.Split(cred, "/")
			if len(credParts) < 5 {
				return sigV4Components{}, false
			}
			c.AccessKey = credParts[0]
			c.DateStamp = credParts[1]
			c.Region = credParts[2]
		} else if strings.HasPrefix(part, "SignedHeaders=") {
			c.SignedHeaders = strings.TrimPrefix(part, "SignedHeaders=")
		} else if strings.HasPrefix(part, "Signature=") {
			c.Signature = strings.TrimPrefix(part, "Signature=")
		}
	}

	if c.AccessKey == "" || c.Signature == "" || c.SignedHeaders == "" {
		return sigV4Components{}, false
	}
	return c, true
}

func sigV4Escape(s string, encodeSlash bool) (string, error) {
	// 🛡️ Rule 35: Validate input string bounds to prevent potential memory exhaustion via excessive capacity growth.
	// 🛡️ Rule 37: Truncation without error breaks canonical signatures. Reject the input if it exceeds limits.
	if len(s) > 1024 {
		return "", fmt.Errorf("sigV4 escape input length exceeds bounds (1024 bytes): %w", syscall.EINVAL)
	}

	hexCount := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' {
			continue
		}
		if !encodeSlash && c == '/' {
			continue
		}
		hexCount++
	}

	if hexCount == 0 {
		return s, nil
	}

	var sb strings.Builder
	sb.Grow(len(s) + 2*hexCount)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~' || (!encodeSlash && c == '/') {
			sb.WriteByte(c)
		} else {
			sb.WriteByte('%')
			sb.WriteByte("0123456789ABCDEF"[c>>4])
			sb.WriteByte("0123456789ABCDEF"[c&15])
		}
	}
	return sb.String(), nil
}

func buildCanonicalRequest(req *http.Request, signedHeaders, payloadHash string) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in buildCanonicalRequest: %v", r)
			err = fmt.Errorf("panic in buildCanonicalRequest: %v: %w", r, syscall.EIO)
		}
	}()

	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	canonicalURIEscaped, uriErr := encodeCanonicalURI(canonicalURI)
	if uriErr != nil {
		return "", uriErr
	}

	canonicalQueryString, qsErr := buildCanonicalQueryString(req.URL.Query())
	if qsErr != nil {
		return "", qsErr
	}

	canonicalHeaders := buildCanonicalHeaders(req, signedHeaders)

	return req.Method + "\n" +
		canonicalURIEscaped + "\n" +
		canonicalQueryString + "\n" +
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash, nil
}

func encodeCanonicalURI(uri string) (string, error) {
	return sigV4Escape(uri, false)
}

func buildCanonicalQueryString(values url.Values) (string, error) {
	keys := make([]string, 0, len(values))
	for k := range values {
		if k == "X-Amz-Signature" {
			continue // the signature itself is never part of the canonical query
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('&')
		}
		encodedKey, err := sigV4Escape(k, true)
		if err != nil {
			return "", err
		}
		sb.WriteString(encodedKey)
		sb.WriteByte('=')
		for j, v := range values[k] {
			if j > 0 {
				sb.WriteByte('&')
				sb.WriteString(encodedKey)
				sb.WriteByte('=')
			}
			encodedVal, err := sigV4Escape(v, true)
			if err != nil {
				return "", err
			}
			sb.WriteString(encodedVal)
		}
	}
	return sb.String(), nil
}

func buildCanonicalHeaders(req *http.Request, signedHeadersStr string) string {
	headerNames := strings.Split(signedHeadersStr, ";")
	sort.Strings(headerNames)

	var sb strings.Builder
	for _, h := range headerNames {
		sb.WriteString(h)
		sb.WriteByte(':')
		val := req.Header.Get(h)
		if h == "host" {
			val = req.Host
		}
		val = strings.TrimSpace(val)
		sb.WriteString(val)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func deriveSigningKey(secretKey, dateStamp, region string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

func computeSignature(signingKey []byte, stringToSign string) string {
	return hexHMAC(signingKey, stringToSign)
}

func buildStringToSign(canonicalRequest, amzDate, dateStamp, region string) string {
	hashedCanonicalRequest := hexSHA256([]byte(canonicalRequest))
	credentialScope := dateStamp + "/" + region + "/s3/aws4_request"
	return "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + hashedCanonicalRequest
}

func hexSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func hexHMAC(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

var emptyStringSHA256 = hexSHA256(nil)

// s3UnsignedPayload is the payload hash used by presigned S3 uploads, which do
// not carry an X-Amz-Content-Sha256 header (aws-sdk-go-v2 / aws-cli presign).
const s3UnsignedPayload = "UNSIGNED-PAYLOAD"

var sigV4MaxSkew = 15 * time.Minute // max allowed clock skew for X-Amz-Date (replay attack prevention)

// isPresignedSigV4 reports whether the request authenticates via query-string
// SigV4 parameters (a presigned URL) rather than the Authorization header.
func isPresignedSigV4(req *http.Request) bool {
	return req.URL.Query().Get("X-Amz-Algorithm") == "AWS4-HMAC-SHA256"
}

// parseSigV4QueryAuth parses presigned-URL SigV4 auth parameters from the query
// string: X-Amz-Algorithm, X-Amz-Credential, X-Amz-Date, X-Amz-Expires,
// X-Amz-SignedHeaders, and X-Amz-Signature.
func parseSigV4QueryAuth(req *http.Request) (sigV4Components, bool) {
	q := req.URL.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		return sigV4Components{}, false
	}
	cred := q.Get("X-Amz-Credential")
	credParts := strings.Split(cred, "/")
	if len(credParts) < 5 {
		return sigV4Components{}, false
	}
	c := sigV4Components{
		AccessKey:     credParts[0],
		DateStamp:     credParts[1],
		Region:        credParts[2],
		SignedHeaders: q.Get("X-Amz-SignedHeaders"),
		Signature:     q.Get("X-Amz-Signature"),
		AmzDate:       q.Get("X-Amz-Date"),
	}
	if c.AccessKey == "" || c.Signature == "" || c.SignedHeaders == "" || c.AmzDate == "" {
		return sigV4Components{}, false
	}
	return c, true
}

func verifySigV4Timestamp(amzDate string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in verifySigV4Timestamp: %v", r)
			ok = false
		}
	}()

	parsedTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		log.Printf("AUDIT: SigV4 rejected unparseable X-Amz-Date %q: %v", amzDate, err)
		return false
	}
	now := time.Now().UTC()
	skew := now.Sub(parsedTime)
	if skew < 0 {
		skew = -skew
	}
	if skew > sigV4MaxSkew {
		log.Printf("AUDIT: SigV4 rejected stale/future X-Amz-Date %q (skew %v > %v)", amzDate, skew, sigV4MaxSkew)
		return false
	}
	return true
}

// verifySigV4Expiry reports whether a presigned request is still within its
// X-Amz-Date + X-Amz-Expires validity window. Requests without a parsable
// date/expires pair are treated as expired (defensive default per Rule 35).
// The expiry is additionally bounds-checked to 0 < ttlSec <= 86400 (24h, the
// AWS presign maximum) to prevent extreme duration calculations (Rule 35).
func verifySigV4Expiry(req *http.Request) bool {
	q := req.URL.Query()
	amzDate := q.Get("X-Amz-Date")
	expires := q.Get("X-Amz-Expires")
	if amzDate == "" || expires == "" {
		return false
	}
	signTime, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return false
	}
	ttlSec, err := strconv.Atoi(expires)
	if err != nil || ttlSec <= 0 || ttlSec > 86400 {
		return false
	}
	return time.Now().UTC().Before(signTime.Add(time.Duration(ttlSec) * time.Second))
}

// verifySigV4Signature validates the SigV4 signature and timestamp freshness.
// Returns bool (consistent with hmac.Equal); the caller in HandshakeServer
// maps false → syscall.EACCES per Rule 10 (POSIX Error Mapping).
// Supports both the Authorization header form (Authorization: AWS4-HMAC-SHA256
// Credential=...) and the presigned query-string form (X-Amz-Algorithm,
// X-Amz-Credential, X-Amz-Date, X-Amz-Expires, X-Amz-SignedHeaders,
// X-Amz-Signature). The query form has no bearer-token body, so it signs an
// UNSIGNED-PAYLOAD payload hash; some clients (aws-cli/boto3 presign) always
// use UNSIGNED-PAYLOAD, others (older SDKs, hand-rolled signers) use the
// empty-body SHA256 for GET/HEAD/DELETE — both are accepted.
// X-Amz-Signature is always excluded from the canonical query string.
func verifySigV4Signature(req *http.Request, authHeader, secretKey string) bool {
	if isPresignedSigV4(req) {
		components, ok := parseSigV4QueryAuth(req)
		if !ok {
			return false
		}
		if !verifySigV4Expiry(req) {
			return false
		}
		if payloadHash := req.Header.Get("X-Amz-Content-Sha256"); payloadHash != "" {
			return verifySigV4SignatureWithPayload(req, components, secretKey, payloadHash)
		}
		// No explicit payload hash: accept either S3 presign convention.
		return verifySigV4SignatureWithPayload(req, components, secretKey, s3UnsignedPayload) ||
			verifySigV4SignatureWithPayload(req, components, secretKey, emptyStringSHA256)
	}

	components, ok := parseSigV4AuthHeader(authHeader)
	if !ok {
		return false
	}

	amzDate := req.Header.Get("X-Amz-Date")
	if amzDate == "" {
		amzDate = components.AmzDate
	}

	if !verifySigV4Timestamp(amzDate) {
		return false
	}

	components.AmzDate = amzDate

	payloadHash := req.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = emptyStringSHA256
	}

	return verifySigV4SignatureWithPayload(req, components, secretKey, payloadHash)
}

// verifySigV4SignatureWithPayload computes the expected SigV4 signature for the
// given payload hash and compares it (constant-time) against the request's.
func verifySigV4SignatureWithPayload(req *http.Request, components sigV4Components, secretKey, payloadHash string) bool {
	canonicalRequest, err := buildCanonicalRequest(req, components.SignedHeaders, payloadHash)
	if err != nil {
		log.Printf("AUDIT: Failed to build canonical request during SigV4 verification: %v", err)
		return false
	}

	stringToSign := buildStringToSign(canonicalRequest, components.AmzDate, components.DateStamp, components.Region)
	signingKey := deriveSigningKey(secretKey, components.DateStamp, components.Region)
	expectedSig := computeSignature(signingKey, stringToSign)

	return hmac.Equal([]byte(expectedSig), []byte(components.Signature))
}
