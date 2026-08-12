package transport

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"strconv"
	"strings"
	"syscall"
)

// aws-chunked streaming payload constants (issue #773).
const (
	// s3StreamingSignedPayload is the X-Amz-Content-Sha256 literal for signed
	// aws-chunked uploads (SigV4 per-chunk signatures).
	s3StreamingSignedPayload = "STREAMING-AWS4-HMAC-SHA256-PAYLOAD"
	// s3StreamingSignedTrailer is the signed aws-chunked variant that appends
	// checksum trailers after the terminating chunk.
	s3StreamingSignedTrailer = "STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER"
	// s3StreamingUnsignedTrailer is the unsigned aws-chunked variant that
	// appends checksum trailers; chunk signatures are empty.
	s3StreamingUnsignedTrailer = "STREAMING-UNSIGNED-PAYLOAD-TRAILER"
	// s3StreamingContentEncoding is the Content-Encoding value used whenever an
	// aws-chunked body is transmitted.
	s3StreamingContentEncoding = "aws-chunked"
	// s3StreamingDecodedContentLength is the header carrying the true decoded
	// payload size on aws-chunked uploads.
	s3StreamingDecodedContentLength = "X-Amz-Decoded-Content-Length"
	// s3StreamingTrailerHeader lists the trailing-header names declared by the
	// client on trailer variants.
	s3StreamingTrailerHeader = "X-Amz-Trailer"

	// maxAWSChunkSize is the maximum data bytes a single aws-chunked chunk may
	// carry (AWS S3 maximum chunk size).
	maxAWSChunkSize = 8 * 1024 * 1024
	// maxAWSChunkHeaderLine bounds the length of a chunk header line to prevent
	// unbounded line reads from a malicious peer (Rule 35).
	maxAWSChunkHeaderLine = 1024
	// maxAWSChunkTrailers bounds the cumulative trailing-header block size.
	maxAWSChunkTrailers = 64 * 1024

	awsChunkSigField            = "chunk-signature="
	awsChunkEmptySHA256         = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	awsChunkStringToSignPayload = "AWS4-HMAC-SHA256-PAYLOAD"
)

// errStreamingSignatureMismatch is returned by the de-framer when a per-chunk
// SigV4 signature fails to verify. It maps to S3 403 SignatureDoesNotMatch.
var errStreamingSignatureMismatch = syscall.EACCES

// streamingMode classifies an aws-chunked upload.
type streamingMode int

const (
	streamingNone streamingMode = iota
	streamingSigned
	streamingSignedTrailer
	streamingUnsignedTrailer
)

// streamingModeOf classifies the aws-chunked framing variant from the request
// headers. Content-Encoding: aws-chunked (with an unrecognized or absent
// STREAMING literal, e.g. under UNSIGNED-PAYLOAD) still de-frames the body,
// simply without per-chunk verification.
func streamingModeOf(sha256Literal, contentEncoding string) streamingMode {
	enc := strings.Contains(contentEncoding, s3StreamingContentEncoding)
	switch sha256Literal {
	case s3StreamingSignedPayload:
		return streamingSigned
	case s3StreamingSignedTrailer:
		return streamingSignedTrailer
	case s3StreamingUnsignedTrailer:
		return streamingUnsignedTrailer
	default:
		if enc {
			return streamingUnsignedTrailer
		}
		return streamingNone
	}
}

// isStreamingLiteral reports whether a hash literal requests aws-chunked
// framing.
func isStreamingLiteral(hash string) bool {
	switch hash {
	case s3StreamingSignedPayload, s3StreamingSignedTrailer, s3StreamingUnsignedTrailer:
		return true
	}
	return strings.HasPrefix(hash, "STREAMING-")
}

// streamingSigningCtx carries the per-chunk SigV4 verification material
// derived from the already-verified request signature (issue #773).
type streamingSigningCtx struct {
	signingKey []byte
	amzDate    string
	scope      string
	seedSig    string
}

// awsChunkedReader decodes an aws-chunked family body while verifying
// per-chunk SigV4 signatures (signed modes) and computing the decoded SHA-256
// content hash and decoded byte count. It reads from the underlying
// bufio.Reader that already sits directly on the connection.
//
// Wire grammar (data bytes are RAW; only the size and signature are textual):
//
//	<hex-size>[;chunk-signature=<64hex>]\r\n <raw data> \r\n
//	...repeat...
//	0[;chunk-signature=<64hex>]\r\n\r\n
//	[ <name>:<value>\r\n ... \r\n ]   (trailer variants only)
//
// Signature verification follows the AWS SigV4 streaming spec and the
// aws-sdk reference implementations (aws-java-sdk-v1 AwsChunkedEncodingInputStream,
// aws-sdk-net ChunkedUploadWrapperStream, MinIO s3ChunkedReader): each chunk's
// signature is hex(HMAC-SHA256(signingKey, stringToSign)) with the chained
// string-to-sign keyed by the derived signing key and seeded with the request
// signature.
type awsChunkedReader struct {
	source *bufio.Reader

	// signing context; nil key means unsigned mode (no per-chunk verification)
	signingKey []byte
	amzDate    string
	scope      string
	prevSig    string

	trailers bool // consume a trailing-header block after the terminating chunk
	maxTotal int64

	chunkHasher hash.Hash // SHA-256 of the current chunk's data (for its signature)
	contentHash hash.Hash // SHA-256 of all decoded data (for momo content addressing)
	chunkSig    string    // declared signature of the current in-flight chunk
	remaining   int64     // unread data bytes of the current chunk
	needHeader  bool
	decoded     int64
	done        bool
	err         error
}

// newAWSChunkedReader constructs a de-framer. When ctx is nil the reader
// operates in unsigned mode (no per-chunk signature verification).
func newAWSChunkedReader(src *bufio.Reader, ctx *streamingSigningCtx, trailers bool, maxTotal int64) *awsChunkedReader {
	r := &awsChunkedReader{
		source:      src,
		trailers:    trailers,
		maxTotal:    maxTotal,
		chunkHasher: sha256.New(),
		contentHash: sha256.New(),
		needHeader:  true,
	}
	if ctx != nil {
		r.signingKey = ctx.signingKey
		r.amzDate = ctx.amzDate
		r.scope = ctx.scope
		r.prevSig = ctx.seedSig
	}
	return r
}

// ContentHash returns the hex SHA-256 of the decoded payload. Only valid after
// the reader reports io.EOF.
func (r *awsChunkedReader) ContentHash() string {
	return hex.EncodeToString(r.contentHash.Sum(nil))
}

// DecodedSize returns the number of decoded payload bytes.
func (r *awsChunkedReader) DecodedSize() int64 { return r.decoded }

// Read implements io.Reader.
func (r *awsChunkedReader) Read(p []byte) (n int, err error) {
	if r.done {
		return 0, io.EOF
	}
	for {
		if r.err != nil {
			return n, r.err
		}
		if r.needHeader {
			if err := r.readChunkHeader(); err != nil {
				r.err = err
				return n, err
			}
			r.needHeader = false
			if r.done {
				return n, io.EOF
			}
		}
		if r.remaining > 0 {
			rn, rerr := r.readChunkData(p)
			if rerr != nil {
				r.err = rerr
				return n, rerr
			}
			return n + rn, nil
		}
		// A chunk of size 0 with remaining == 0 cannot repeat; loop to header.
		r.needHeader = true
	}
}

// readChunkHeader parses the next chunk header, handling the terminating chunk.
func (r *awsChunkedReader) readChunkHeader() error {
	line, err := r.readHeaderLine()
	if err != nil {
		return err
	}
	size, sig, err := parseAWSChunkHeader(line)
	if err != nil {
		return err
	}
	r.remaining = size
	r.chunkSig = sig
	r.chunkHasher.Reset()
	if size == 0 {
		// Terminating chunk: signed over empty data, then a terminator CRLF,
		// then (for trailer variants) the trailing-header block.
		if r.signingKey != nil {
			if err := r.verifyChunkHash(awsChunkEmptySHA256); err != nil {
				return err
			}
		}
		if err := r.expectCRLF(); err != nil {
			return fmt.Errorf("malformed aws-chunked terminating chunk: %w", err)
		}
		if r.trailers {
			if err := r.consumeTrailers(); err != nil {
				return err
			}
		}
		r.done = true
		return nil
	}
	return nil
}

// readChunkData reads up to one caller buffer's worth of the current chunk,
// never reading past the declared chunk size.
func (r *awsChunkedReader) readChunkData(p []byte) (int, error) {
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	rn, err := io.ReadFull(r.source, p[:n])
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return rn, fmt.Errorf("unexpected EOF in aws-chunked chunk data: %w", syscall.EBADMSG)
		}
		return rn, err
	}
	r.remaining -= int64(rn)
	r.chunkHasher.Write(p[:rn])
	r.contentHash.Write(p[:rn])
	r.decoded += int64(rn)
	if r.decoded > r.maxTotal {
		return rn, fmt.Errorf("aws-chunked decoded payload exceeds maximum size: %w", syscall.EOVERFLOW)
	}
	if r.remaining == 0 {
		if err := r.expectCRLF(); err != nil {
			return rn, fmt.Errorf("malformed aws-chunked chunk terminator: %w", err)
		}
		if r.signingKey != nil {
			digest := r.chunkHasher.Sum(nil)
			if verr := r.verifyChunkHash(hex.EncodeToString(digest)); verr != nil {
				return rn, verr
			}
		}
		r.chunkHasher.Reset()
	}
	return rn, nil
}

// verifyChunkHash verifies the current chunk signature against a chunk-data
// hash (hex), chains the signature forward, and returns the 403-mapped error
// on mismatch.
func (r *awsChunkedReader) verifyChunkHash(chunkHashHex string) error {
	stringToSign := awsChunkStringToSignPayload + "\n" +
		r.amzDate + "\n" +
		r.scope + "\n" +
		r.prevSig + "\n" +
		awsChunkEmptySHA256 + "\n" +
		chunkHashHex
	expected := hex.EncodeToString(hmacSHA256(r.signingKey, stringToSign))
	if !hmac.Equal([]byte(expected), []byte(r.chunkSig)) {
		return fmt.Errorf("%w: aws-chunked chunk signature mismatch", errStreamingSignatureMismatch)
	}
	r.prevSig = expected
	return nil
}

// readHeaderLine reads a single newline-terminated chunk header line, bounded
// to maxAWSChunkHeaderLine bytes.
func (r *awsChunkedReader) readHeaderLine() (string, error) {
	buf := make([]byte, 0, 128)
	for {
		frag, err := r.source.ReadSlice('\n')
		buf = append(buf, frag...)
		if err == bufio.ErrBufferFull {
			if len(buf) > maxAWSChunkHeaderLine {
				return "", fmt.Errorf("aws-chunked header line exceeds %d bytes: %w", maxAWSChunkHeaderLine, syscall.EBADMSG)
			}
			continue
		}
		if err != nil && errors.Is(err, io.EOF) && len(frag) == 0 {
			return "", fmt.Errorf("unexpected EOF in aws-chunked header: %w", syscall.EBADMSG)
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(frag) > 0 {
				return "", fmt.Errorf("truncated aws-chunked header line: %w", syscall.EBADMSG)
			}
			return "", err
		}
		if len(buf) > maxAWSChunkHeaderLine {
			return "", fmt.Errorf("aws-chunked header line exceeds %d bytes: %w", maxAWSChunkHeaderLine, syscall.EBADMSG)
		}
		buf = buf[:len(buf)-1] // strip '\n'
		if len(buf) > 0 && buf[len(buf)-1] == '\r' {
			buf = buf[:len(buf)-1]
		}
		return string(buf), nil
	}
}

// parseAWSChunkHeader parses "hex-size[;chunk-signature=<sig>][;ext=v]".
func parseAWSChunkHeader(line string) (int64, string, error) {
	parts := strings.Split(line, ";")
	size, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 16, 64)
	if err != nil || size < 0 {
		return 0, "", fmt.Errorf("malformed aws-chunked chunk size %q: %w", line, syscall.EBADMSG)
	}
	if size > maxAWSChunkSize {
		return 0, "", fmt.Errorf("aws-chunked chunk size %d exceeds maximum %d: %w", size, maxAWSChunkSize, syscall.EBADMSG)
	}
	sig := ""
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, awsChunkSigField) {
			sig = strings.TrimPrefix(part, awsChunkSigField)
			if len(sig) > 64 {
				return 0, "", fmt.Errorf("malformed aws-chunked signature length: %w", syscall.EBADMSG)
			}
		}
	}
	return size, sig, nil
}

// expectCRLF consumes a chunk terminator "\r\n".
func (r *awsChunkedReader) expectCRLF() error {
	var two [2]byte
	if _, err := io.ReadFull(r.source, two[:]); err != nil {
		return fmt.Errorf("unexpected EOF reading aws-chunked terminator: %w", syscall.EBADMSG)
	}
	if two != [2]byte{'\r', '\n'} {
		return fmt.Errorf("malformed aws-chunked terminator: %w", syscall.EBADMSG)
	}
	return nil
}

// consumeTrailers reads and discards the trailing-header block ("name:value"
// lines terminated by a blank line), bounded to maxAWSChunkTrailers bytes.
func (r *awsChunkedReader) consumeTrailers() error {
	var total int
	for {
		line, err := r.readHeaderLine()
		if err != nil {
			return err
		}
		total += len(line) + 2
		if total > maxAWSChunkTrailers {
			return fmt.Errorf("aws-chunked trailer block exceeds %d bytes: %w", maxAWSChunkTrailers, syscall.EBADMSG)
		}
		if line == "" {
			return nil // blank line terminates the trailer block
		}
	}
}
