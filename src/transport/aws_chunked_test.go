package transport

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

// AWS SigV4 streaming documentation example (docs.aws.amazon.com/AmazonS3/
// latest/API/sigv4-streaming.html): a 66560-byte object uploaded in two chunks
// (65536 + 1024 bytes of 'a') under credentials
// AKIAIOSFODNN7EXAMPLE / wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY with request
// date 20130524T000000Z in us-east-1.
const (
	docAccessKey = "AKIAIOSFODNN7EXAMPLE"
	docSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	docDate      = "20130524T000000Z"
	docDateStamp = "20130524"
	docRegion    = "us-east-1"

	docSeedSig = "4f232c4386841ef735655705268965c44a0e4690baa4adea153f7db9fa80a0a9"
	docChunk1  = "ad80c730a21e5b8d04586a2213dd63b9a0e99e0e2307b0ade35a65485a288648"
	docChunk2  = "0055627c9e194cb4542bae2aa5492e3c1575bbb81b612b7d234b86a503ef5497"
	docTerm    = "b6c6ea8a5354eaf15b3cb7646744f4275b71ea724fed81ceb9323e279d449df9"

	docContentHash = "cd69d3887c6af9264b100d7b7602331335d9aa7e3bd7c30cdc6d6f4bfbb3c888"
)

// docWireBody builds the aws-chunked wire body for the documentation example.
func docWireBody() []byte {
	var buf bytes.Buffer
	buf.WriteString("10000;chunk-signature=" + docChunk1 + "\r\n")
	buf.Write(bytes.Repeat([]byte{'a'}, 65536))
	buf.WriteString("\r\n")
	buf.WriteString("400;chunk-signature=" + docChunk2 + "\r\n")
	buf.Write(bytes.Repeat([]byte{'a'}, 1024))
	buf.WriteString("\r\n")
	buf.WriteString("0;chunk-signature=" + docTerm + "\r\n\r\n")
	return buf.Bytes()
}

func docSigningCtx() *streamingSigningCtx {
	return &streamingSigningCtx{
		signingKey: deriveSigningKey(docSecretKey, docDateStamp, docRegion),
		amzDate:    docDate,
		scope:      docDateStamp + "/" + docRegion + "/s3/aws4_request",
		seedSig:    docSeedSig,
	}
}

func readAllBuffered(t *testing.T, r *awsChunkedReader) ([]byte, error) {
	t.Helper()
	buf := make([]byte, 7777) // odd buffer to force multi-chunk de-framing
	var out bytes.Buffer
	for {
		n, err := r.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			if err == io.EOF {
				return out.Bytes(), nil
			}
			return out.Bytes(), err
		}
	}
}

func TestAWSChunkedReaderSignedDocVector(t *testing.T) {
	r := newAWSChunkedReader(bufio.NewReader(bytes.NewReader(docWireBody())), docSigningCtx(), false, 1<<30)

	data, err := readAllBuffered(t, r)
	if err != nil {
		t.Fatalf("signed decode failed: %v", err)
	}
	want := bytes.Repeat([]byte{'a'}, 66560)
	if !bytes.Equal(data, want) {
		t.Fatalf("decoded content mismatch: got %d bytes want %d", len(data), len(want))
	}
	if r.DecodedSize() != 66560 {
		t.Errorf("DecodedSize = %d, want 66560", r.DecodedSize())
	}
	if r.ContentHash() != docContentHash {
		t.Errorf("ContentHash = %s, want %s", r.ContentHash(), docContentHash)
	}
}

func TestAWSChunkedReaderSignedCorruptedChunkSignature(t *testing.T) {
	wire := strings.ReplaceAll(
		"10000;chunk-signature=ad80c730a21e5b8d04586a2213dd63b9a0e99e0e2307b0ade35a65485a288648\r\n"+
			strings.Repeat("a", 65536)+"\r\n0;chunk-signature="+docTerm+"\r\n\r\n",
		docChunk1,
		"0000000000000000000000000000000000000000000000000000000000000000")

	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), docSigningCtx(), false, 1<<30)
	if _, err := readAllBuffered(t, r); !errors.Is(err, errStreamingSignatureMismatch) {
		t.Fatalf("expected signature mismatch, got %v", err)
	}
}

func TestAWSChunkedReaderTruncatedChunkData(t *testing.T) {
	wire := "10000;chunk-signature=" + docChunk1 + "\r\n" + strings.Repeat("a", 100) + "\r\n"
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), docSigningCtx(), false, 1<<30)
	if _, err := readAllBuffered(t, r); err == nil {
		t.Fatal("expected error on truncated chunk data")
	}
}

func TestAWSChunkedReaderMissingTerminatingChunk(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("400;chunk-signature=" + docChunk2 + "\r\n")
	buf.Write(bytes.Repeat([]byte{'a'}, 1024))
	buf.WriteString("\r\n")
	r := newAWSChunkedReader(bufio.NewReader(&buf), docSigningCtx(), false, 1<<30)
	if _, err := readAllBuffered(t, r); err == nil {
		t.Fatal("expected error when terminating chunk is missing")
	}
}

func TestAWSChunkedReaderUnsignedTrailer(t *testing.T) {
	wire := "4\r\naaaa\r\n0\r\n\r\n" +
		"x-amz-checksum-crc32c:sOO8/Q==\r\n" +
		"x-amz-trailer-signature:deadbeef\r\n\r\n"
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), nil, true, 1<<30)
	data, err := readAllBuffered(t, r)
	if err != nil {
		t.Fatalf("unsigned trailer decode failed: %v", err)
	}
	if string(data) != "aaaa" {
		t.Fatalf("decoded content %q, want %q", data, "aaaa")
	}
	// Trailing signature text must have been consumed so a following read hits
	// EOF rather than leaking trailer bytes.
	if extra, err := r.Read(make([]byte, 16)); err != io.EOF || extra != 0 {
		t.Fatalf("expected EOF after trailers, got n=%d err=%v", extra, err)
	}
}

func TestAWSChunkedReaderUnsignedWithoutTerminatorCRLF(t *testing.T) {
	// A terminating header of 0 followed by a single CRLF but no trailing
	// blank-line CRLF must be rejected.
	wire := "0\r\n"
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), nil, false, 1<<30)
	if _, err := readAllBuffered(t, r); err == nil {
		t.Fatal("expected error for missing terminal blank line")
	}
}

func TestAWSChunkedReaderExceedsMaxTotal(t *testing.T) {
	// A valid unsigned single chunk larger than maxTotal must fail with EOVERFLOW
	// once the decoded total exceeds the bound even if the chunk completes.
	wire := "400\r\n" + strings.Repeat("a", 1024) + "\r\n0\r\n\r\n"
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), nil, false, 512)
	if _, err := readAllBuffered(t, r); !errors.Is(err, syscall.EOVERFLOW) {
		t.Fatalf("expected EOVERFLOW, got %v", err)
	}
}

func TestAWSChunkedReaderChunkTooLarge(t *testing.T) {
	wire := "9000000\r\n" // 0x9000000 = 150994944 > maxAWSChunkSize
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), nil, false, 1<<30)
	if _, err := readAllBuffered(t, r); !errors.Is(err, syscall.EBADMSG) {
		t.Fatalf("expected EBADMSG for oversized chunk, got %v", err)
	}
}

func TestAWSChunkedReaderHeaderLineTooLong(t *testing.T) {
	wire := "400;chunk-signature=" + strings.Repeat("0", 2048) + "a\r\n"
	r := newAWSChunkedReader(bufio.NewReader(strings.NewReader(wire)), nil, false, 1<<30)
	if _, err := readAllBuffered(t, r); err == nil {
		t.Fatal("expected error for oversized header line")
	}
}

func TestParseAWSChunkHeader(t *testing.T) {
	size, sig, err := parseAWSChunkHeader("10000;chunk-signature=" + docChunk1)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if size != 65536 || sig != docChunk1 {
		t.Fatalf("got size=%d sig=%s", size, sig)
	}
	if _, _, err := parseAWSChunkHeader("zzz;chunk-signature=x"); err == nil {
		t.Fatal("expected error for non-hex size")
	}
}

func TestStreamingModeOf(t *testing.T) {
	cases := []struct {
		literal string
		enc     string
		want    streamingMode
	}{
		{s3StreamingSignedPayload, "aws-chunked", streamingSigned},
		{s3StreamingSignedTrailer, "aws-chunked", streamingSignedTrailer},
		{s3StreamingUnsignedTrailer, "aws-chunked", streamingUnsignedTrailer},
		{"UNSIGNED-PAYLOAD", "", streamingNone},
		{"", "aws-chunked", streamingUnsignedTrailer},
		{"65536", "aws-chunked", streamingUnsignedTrailer},
		{"", "", streamingNone},
	}
	for _, c := range cases {
		if got := streamingModeOf(c.literal, c.enc); got != c.want {
			t.Errorf("streamingModeOf(%q, %q) = %d, want %d", c.literal, c.enc, got, c.want)
		}
	}
	if !isStreamingLiteral(s3StreamingSignedPayload) {
		t.Error("STREAMING-* literal must be detected as streaming")
	}
	if isStreamingLiteral("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855") {
		t.Error("a plain payload hash must not be detected as streaming")
	}
}
