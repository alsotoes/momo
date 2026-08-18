package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"github.com/alsotoes/momo/src/common"
)

const (
	maxS3KeyLen      = 1024
	maxS3BucketLen   = 128
	maxS3EndpointLen = 1024
)

// S3BlobStore implements BlobStore using an S3-compatible API.
// It uses a zero-dependency SigV4 HTTP client (no AWS SDK required).
// Object key = content hash. Metadata (refcounts, tombstones) stays
// in local bbolt via the CASStore wrapper.
type S3BlobStore struct {
	client    *http.Client
	endpoint  string
	region    string
	bucket    string
	accessKey string
	secretKey string
	pathStyle bool
}

// NewS3BlobStore creates a new S3-backed BlobStore from storage config.
func NewS3BlobStore(cfg common.ConfigurationStorage) (*S3BlobStore, error) {
	if cfg.S3Endpoint == "" {
		return nil, fmt.Errorf("s3_endpoint is required: %w", syscall.EINVAL)
	}
	if cfg.S3Bucket == "" {
		return nil, fmt.Errorf("s3_bucket is required: %w", syscall.EINVAL)
	}
	if cfg.S3AccessKey == "" {
		return nil, fmt.Errorf("s3_access_key is required: %w", syscall.EINVAL)
	}
	if cfg.S3SecretKey == "" {
		return nil, fmt.Errorf("s3_secret_key is required: %w", syscall.EINVAL)
	}

	endpoint := strings.TrimRight(cfg.S3Endpoint, "/")
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil {
		return nil, fmt.Errorf("s3_endpoint is not a valid URL: %v: %w", parseErr, syscall.EINVAL)
	}
	switch parsed.Scheme {
	case "https":
		// TLS always allowed.
	case "http":
		if !cfg.S3Insecure {
			return nil, fmt.Errorf("s3_endpoint uses cleartext http://; require https:// or set s3_insecure=true to allow insecure plaintext transport: %w", syscall.EINVAL)
		}
		log.Printf("WARNING: s3_insecure=true: S3 endpoint %q uses cleartext http://; credentials and blob content are transmitted without TLS", endpoint)
	case "":
		return nil, fmt.Errorf("s3_endpoint must include a scheme (https:// or http://): %w", syscall.EINVAL)
	default:
		return nil, fmt.Errorf("s3_endpoint scheme %q is unsupported; use https:// (or http:// with s3_insecure=true): %w", parsed.Scheme, syscall.EINVAL)
	}

	region := cfg.S3Region
	if region == "" {
		region = "us-east-1"
	}

	// Use a custom Transport with dial/response-header timeouts instead of an
	// overall client timeout. http.Client.Timeout also covers the entire body
	// read, which would abort large blob downloads (e.g. 1 GiB at 50 Mbit/s
	// takes ~170s) even when the connection is healthy.
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
	}

	return &S3BlobStore{
		client: &http.Client{
			Transport: transport,
		},
		endpoint:  endpoint,
		region:    region,
		bucket:    cfg.S3Bucket,
		accessKey: cfg.S3AccessKey,
		secretKey: cfg.S3SecretKey,
		pathStyle: cfg.S3PathStyle,
	}, nil
}

func (s *S3BlobStore) Close() error {
	if transport, ok := s.client.Transport.(*http.Transport); ok {
		transport.CloseIdleConnections()
	}
	return nil
}

// PutBlob uploads a blob to S3 using HTTP PUT with SigV4 authentication.
// The content is signed with SIGNED_PAYLOAD (issue #776): the body is spooled
// to a temp file while its SHA-256 is computed, then uploaded with a real
// Content-Length and a payload hash that binds the signature to the content.
// Spooling to disk (rather than buffering in memory) keeps the operation
// bounded-memory (OOM/DoS prevention). The spool file is always removed.
// Oversized blobs are rejected before any upload occurs.
func (s *S3BlobStore) PutBlob(hash string, content io.Reader) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Panic recovered in S3BlobStore.PutBlob: %v", r)
			err = fmt.Errorf("panic in S3BlobStore.PutBlob: %v: %w", r, syscall.EIO)
		}
	}()

	spill, err := os.CreateTemp("", "momo-s3-put-*")
	if err != nil {
		return fmt.Errorf("s3: failed to create spool file: %w", syscall.EIO)
	}
	spillPath := spill.Name()
	defer func() {
		_ = spill.Close()
		_ = os.Remove(spillPath)
	}()

	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(spill, hasher), io.LimitReader(content, common.MaxFileSize+1))
	if copyErr != nil {
		return fmt.Errorf("s3: failed to spool blob: %w", copyErr)
	}
	if written > common.MaxFileSize {
		return fmt.Errorf("s3: blob exceeds MaxFileSize (%d bytes): %w", common.MaxFileSize, syscall.EFBIG)
	}
	if _, seekErr := spill.Seek(0, io.SeekStart); seekErr != nil {
		return fmt.Errorf("s3: failed to rewind spool file: %w", syscall.EIO)
	}

	payloadHash := hex.EncodeToString(hasher.Sum(nil))
	req, err := s.newRequest("PUT", hash, spill, payloadHash)
	if err != nil {
		return err
	}
	req.ContentLength = written

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3: PUT request failed: %w", syscall.ECONNREFUSED)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3: PUT failed with status %d: %w", resp.StatusCode, syscall.EIO)
	}

	return nil
}

// GetBlob downloads a blob from S3 using HTTP GET with SigV4 authentication.
func (s *S3BlobStore) GetBlob(hash string) (io.ReadCloser, error) {
	req, err := s.newRequest("GET", hash, nil, emptyStringSHA256)
	if err != nil {
		return nil, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: GET request failed: %w", syscall.ECONNREFUSED)
	}

	if resp.StatusCode == http.StatusNotFound {
		drainAndClose(resp.Body)
		return nil, syscall.ENOENT
	}
	if resp.StatusCode != http.StatusOK {
		drainAndClose(resp.Body)
		return nil, fmt.Errorf("s3: GET failed with status %d: %w", resp.StatusCode, syscall.EIO)
	}
	return resp.Body, nil
}

// DeleteBlob removes a blob from S3 using HTTP DELETE with SigV4 authentication.
// Missing blobs are silently ignored (treated as success).
func (s *S3BlobStore) DeleteBlob(hash string) error {
	req, err := s.newRequest("DELETE", hash, nil, emptyStringSHA256)
	if err != nil {
		return err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("s3: DELETE request failed: %w", syscall.ECONNREFUSED)
	}
	defer drainAndClose(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("s3: DELETE failed with status %d: %w", resp.StatusCode, syscall.EIO)
	}
	return nil
}

// drainAndClose discards any remaining response body before closing it so the
// underlying HTTP connection can be returned to the transport pool.
// Necessary for reusing secure (TLS) connections for error responses -- AWS S3
// may send a non-empty error body even for 4xx/5xx. The body is bounded with
// io.LimitReader to prevent unbounded memory consumption (Rule 4).
func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, 4096))
	_ = body.Close()
}

// newRequest builds an HTTP request for the given method and object key.
func (s *S3BlobStore) newRequest(method, key string, body io.Reader, payloadHash string) (req *http.Request, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("CRITICAL: Recovered from panic in S3BlobStore.newRequest: %v", r)
			if closer, ok := body.(io.ReadCloser); ok {
				closer.Close()
			}
			err = fmt.Errorf("panic in S3BlobStore.newRequest: %v: %w", r, syscall.EIO)
		}
	}()

	if len(key) > maxS3KeyLen || len(s.bucket) > maxS3BucketLen || len(s.endpoint) > maxS3EndpointLen {
		return nil, fmt.Errorf("s3: input length exceeds limits: %w", syscall.EINVAL)
	}

	cleanedKey := path.Clean(key)
	if cleanedKey == "." || cleanedKey == ".." || strings.HasPrefix(cleanedKey, "../") || strings.HasPrefix(cleanedKey, "/") {
		return nil, fmt.Errorf("s3: invalid key path traversal: %w", syscall.EINVAL)
	}

	var reqURL string
	if s.pathStyle {
		reqURL = s.endpoint + "/" + s.bucket + "/" + cleanedKey
	} else {
		parsedEndpoint, parseErr := url.Parse(s.endpoint)
		if parseErr != nil {
			return nil, fmt.Errorf("s3: invalid endpoint: %v: %w", parseErr, syscall.EINVAL)
		}
		parsedEndpoint.Host = s.bucket + "." + parsedEndpoint.Host
		parsedEndpoint.Path = "/" + cleanedKey
		reqURL = parsedEndpoint.String()
	}

	parsedURL, parseErr := url.Parse(reqURL)
	if parseErr != nil {
		return nil, fmt.Errorf("s3: invalid URL: %v: %w", parseErr, syscall.EINVAL)
	}

	req, newReqErr := http.NewRequest(method, reqURL, body)
	if newReqErr != nil {
		return nil, fmt.Errorf("s3: failed to create request: %v: %w", newReqErr, syscall.EINVAL)
	}

	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	req.Host = parsedURL.Host
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	signingKey := s.getSigningKey(dateStamp)
	stringToSign, signErr := s.getStringToSign(method, parsedURL, amzDate, dateStamp, payloadHash)
	if signErr != nil {
		return nil, signErr
	}
	signature := hexHMAC(signingKey, stringToSign)

	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	credentialScope := dateStamp + "/" + s.region + "/s3/aws4_request"
	authHeader := "AWS4-HMAC-SHA256 Credential=" + s.accessKey + "/" + credentialScope + ", SignedHeaders=" + signedHeaders + ", Signature=" + signature
	req.Header.Set("Authorization", authHeader)

	return req, nil
}

// getStringToSign builds the AWS SigV4 string-to-sign.
func (s *S3BlobStore) getStringToSign(method string, parsedURL *url.URL, amzDate, dateStamp, payloadHash string) (str string, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic building string to sign: %v", r)
			err = syscall.EIO
		}
	}()
	canonicalURI := parsedURL.Path
	if canonicalURI == "" {
		canonicalURI = "/"
	}

	host := parsedURL.Host

	canonicalHeaders := "host:" + host + "\nx-amz-content-sha256:" + payloadHash + "\nx-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := method + "\n" + canonicalURI + "\n\n" + canonicalHeaders + "\n" + signedHeaders + "\n" + payloadHash

	credentialScope := dateStamp + "/" + s.region + "/s3/aws4_request"
	hashedCanonicalRequest := hexSHA256([]byte(canonicalRequest))

	return "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + hashedCanonicalRequest, nil
}

// getSigningKey derives the SigV4 signing key from the secret key.
func (s *S3BlobStore) getSigningKey(dateStamp string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+s.secretKey), dateStamp)
	kRegion := hmacSHA256(kDate, s.region)
	kService := hmacSHA256(kRegion, "s3")
	return hmacSHA256(kService, "aws4_request")
}

var emptyStringSHA256 = hexSHA256(nil)

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

// Ensure S3BlobStore satisfies BlobStore at compile time.
var _ BlobStore = (*S3BlobStore)(nil)
