package transport

import (
	"errors"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

// TestDeclaredDecodedLength covers the parse and bounds branches that guard an
// aws-chunked upload before a single byte is read.
func TestDeclaredDecodedLength(t *testing.T) {
	comm, _ := newPipeComm(t)

	t.Run("absent", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPut, "http://x/b/k", nil)
		got, err := comm.declaredDecodedLength(req)
		if err != nil || got != -1 {
			t.Fatalf("got (%d, %v), want (-1, nil)", got, err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		comm, _ := newPipeComm(t)
		req, _ := http.NewRequest(http.MethodPut, "http://x/b/k", nil)
		req.Header.Set(s3StreamingDecodedContentLength, "not-a-number")
		if _, err := comm.declaredDecodedLength(req); err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Fatalf("expected EBADMSG, got %v", err)
		}
	})

	t.Run("negative", func(t *testing.T) {
		comm, _ := newPipeComm(t)
		req, _ := http.NewRequest(http.MethodPut, "http://x/b/k", nil)
		req.Header.Set(s3StreamingDecodedContentLength, "-5")
		if _, err := comm.declaredDecodedLength(req); err == nil || !errors.Is(err, syscall.EBADMSG) {
			t.Fatalf("expected EBADMSG, got %v", err)
		}
	})

	t.Run("over the limit", func(t *testing.T) {
		comm, _ := newPipeComm(t)
		req, _ := http.NewRequest(http.MethodPut, "http://x/b/k", nil)
		req.Header.Set(s3StreamingDecodedContentLength, "999999999999999")
		if _, err := comm.declaredDecodedLength(req); err == nil || !errors.Is(err, syscall.EOVERFLOW) {
			t.Fatalf("expected EOVERFLOW, got %v", err)
		}
	})
}

// TestStreamingContext covers the signing-material gate: a signed stream without
// SigV4 context is refused; unsigned streams pass with a trailer flag.
func TestStreamingContext(t *testing.T) {
	comm, _ := newPipeComm(t)
	req, _ := http.NewRequest(http.MethodPut, "http://x/b/k", nil)

	if _, _, err := comm.streamingContext(streamingSigned, req); err == nil || !errors.Is(err, syscall.EACCES) {
		t.Fatalf("expected EACCES for signed streaming without SigV4, got %v", err)
	}

	_, trailers, err := comm.streamingContext(streamingUnsignedTrailer, req)
	if err != nil || !trailers {
		t.Fatalf("unsigned trailer mode: trailers=%v err=%v", trailers, err)
	}

	_, trailers, err = comm.streamingContext(streamingNone, req)
	if err != nil || trailers {
		t.Fatalf("unsigned mode must not expect trailers: trailers=%v err=%v", trailers, err)
	}

	req.Header.Set(s3StreamingTrailerHeader, "x-amz-checksum-crc32")
	if _, trailers, _ = comm.streamingContext(streamingNone, req); !trailers {
		t.Fatal("an X-Amz-Trailer header must imply a trailer block")
	}
}

// TestDecodeErrorMapping pins the three failure translations and their POSIX
// errnos so a de-framing fault answers the client correctly.
func TestDecodeErrorMapping(t *testing.T) {
	comm, client := newPipeComm(t)
	// Drain the S3 error bodies the mapping writes.
	go func() {
		buf := make([]byte, 512)
		for {
			if _, err := client.Read(buf); err != nil {
				return
			}
		}
	}()

	if err := comm.decodeError(errStreamingSignatureMismatch); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("signature mismatch: expected EACCES, got %v", err)
	}
	if err := comm.decodeError(syscall.EOVERFLOW); !errors.Is(err, syscall.EOVERFLOW) {
		t.Fatalf("overflow: expected EOVERFLOW, got %v", err)
	}
	err := comm.decodeError(errors.New("garbage framing"))
	if !errors.Is(err, syscall.EBADMSG) || !strings.Contains(err.Error(), "streaming decode failed") {
		t.Fatalf("malformed body: expected EBADMSG, got %v", err)
	}
}

// TestSetStreamingReadDeadline proves both branches (declared size and the
// fallback) run without a live connection.
func TestSetStreamingReadDeadline(t *testing.T) {
	comm, _ := newPipeComm(t)
	comm.setStreamingReadDeadline(-1)
	comm.setStreamingReadDeadline(64 << 20)
}
