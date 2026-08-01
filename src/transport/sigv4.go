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
	"strings"
	"syscall"
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

func buildCanonicalRequest(req *http.Request, signedHeaders, payloadHash string) (string, error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in buildCanonicalRequest: %v", r)
		}
	}()

	canonicalURI := req.URL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalURI, err := encodeCanonicalURI(canonicalURI)
	if err != nil {
		return "", err
	}

	canonicalQueryString, err := buildCanonicalQueryString(req.URL.Query())
	if err != nil {
		return "", err
	}

	canonicalHeaders := buildCanonicalHeaders(req, signedHeaders)

	return req.Method + "\n" +
		canonicalURI + "\n" +
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

func verifySigV4Signature(req *http.Request, authHeader, secretKey string) bool {
	components, ok := parseSigV4AuthHeader(authHeader)
	if !ok {
		return false
	}

	amzDate := req.Header.Get("X-Amz-Date")
	if amzDate == "" {
		amzDate = components.AmzDate
	}

	payloadHash := req.Header.Get("X-Amz-Content-Sha256")
	if payloadHash == "" {
		payloadHash = emptyStringSHA256
	}

	canonicalRequest, err := buildCanonicalRequest(req, components.SignedHeaders, payloadHash)
	if err != nil {
		log.Printf("AUDIT: Failed to build canonical request during SigV4 verification: %v", err)
		return false
	}

	stringToSign := buildStringToSign(canonicalRequest, amzDate, components.DateStamp, components.Region)
	signingKey := deriveSigningKey(secretKey, components.DateStamp, components.Region)
	expectedSig := computeSignature(signingKey, stringToSign)

	return hmac.Equal([]byte(expectedSig), []byte(components.Signature))
}
