package transport

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
)

func TestAppendS3MetaHeadersGatesChecksums(t *testing.T) {
	headers := map[string]string{
		"content-type":             "application/octet-stream",
		"x-amz-checksum-crc32":     "AAAAAA==",
		"x-amz-checksum-algorithm": "CRC32",
		"x-amz-meta-owner":         "team-a",
		"cache-control":            "max-age=60",
	}

	plain := string(appendS3MetaHeaders(nil, headers, "", false))
	if strings.Contains(plain, "x-amz-checksum-") {
		t.Fatalf("plain response must not carry checksum headers, got %q", plain)
	}
	for _, want := range []string{"Content-Type: application/octet-stream", "x-amz-meta-owner: team-a", "cache-control: max-age=60"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("plain response lost %q: %q", want, plain)
		}
	}

	withMode := string(appendS3MetaHeaders(nil, headers, "", true))
	if !strings.Contains(withMode, "x-amz-checksum-crc32: AAAAAA==") {
		t.Fatalf("checksum-mode response must carry the stored checksum, got %q", withMode)
	}
}

// TestChecksumModeEnabled pins the request-header check.
func TestChecksumModeEnabled(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:4440/b/k", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if checksumModeEnabled(req) {
		t.Fatal("checksum mode must be off by default")
	}
	req.Header.Set("X-Amz-Checksum-Mode", "ENABLED")
	if !checksumModeEnabled(req) {
		t.Fatal("checksum mode must be detected (case-insensitive header)")
	}
}

// TestCompositeChecksumDetection pins the multipart checksum rule: a value with
// a "-<partCount>" suffix is a checksum of the parts, not of the object (#1354).
func TestCompositeChecksumDetection(t *testing.T) {
	cases := map[string]bool{
		"AAAAAA==":           false,
		"abc==-4":            true,
		"abc==-12":           true,
		"":                   false,
		"-3":                 false,
		"abc==-":             false,
		"abc==-x":            false,
		"dGVzdC1vYmplY3Q=-1": true,
	}
	for value, want := range cases {
		if got := isCompositeChecksum(value); got != want {
			t.Fatalf("isCompositeChecksum(%q) = %v, want %v", value, got, want)
		}
	}
}

// TestCompositeChecksumIsNotEchoed proves a composite value is dropped from the
// response even in checksum mode, so a client cannot validate it against the
// object bytes.
func TestCompositeChecksumIsNotEchoed(t *testing.T) {
	headers := map[string]string{
		"x-amz-checksum-crc32":     "abc==-4",
		"x-amz-checksum-algorithm": "CRC32",
	}
	got := string(appendS3MetaHeaders(nil, headers, "", true))
	if strings.Contains(got, "abc==-4") {
		t.Fatalf("composite checksum echoed to the client: %q", got)
	}

	whole := map[string]string{"x-amz-checksum-crc32": "AAAAAA=="}
	if out := string(appendS3MetaHeaders(nil, whole, "", true)); !strings.Contains(out, "CCCCCC") && !strings.Contains(out, "AAAAAA==") {
		t.Fatalf("whole-object checksum must still be echoed, got %q", out)
	}
}

// TestTrailerChecksumSelection pins how a trailing checksum is located: a trailer
// value wins, then the declared X-Amz-Trailer, then none.
func TestTrailerChecksumSelection(t *testing.T) {
	name, value, ok := trailerChecksum(map[string]string{"x-amz-checksum-crc32": "AAAAAA=="})
	if !ok || name != "x-amz-checksum-crc32" || value != "AAAAAA==" {
		t.Fatalf("got (%q,%q,%v)", name, value, ok)
	}

	if _, _, ok = trailerChecksum(nil); ok {
		t.Fatal("no trailer must report ok=false")
	}
	if _, _, ok = trailerChecksum(map[string]string{"x-amz-meta-note": "v"}); ok {
		t.Fatal("a non-checksum trailer must report ok=false")
	}
	// Declared but never delivered: nothing to verify, not an error.
	if _, _, ok = trailerChecksum(map[string]string{"x-amz-checksum-crc32": ""}); ok {
		t.Fatal("an empty trailer value must report ok=false")
	}
}

// TestIsTrailerName pins the header-name character set, so a trailer cannot
// smuggle arbitrary bytes into a log line or a response header.
func TestIsTrailerName(t *testing.T) {
	for _, good := range []string{"x-amz-checksum-crc32", "x_amz_1"} {
		if !isTrailerName(good) {
			t.Fatalf("%q must be accepted", good)
		}
	}
	for _, bad := range []string{"", "x y", "x:y", "x\x00", strings.Repeat("a", 129)} {
		if isTrailerName(bad) {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

// TestCRC64NVMEChecksum pins S3's CRC-64/NVME support (issue #1363): clients
// default to it for trailer checksums, so an unsupported algorithm here means
// refusing their uploads outright.
func TestCRC64NVMEChecksum(t *testing.T) {
	const payload = "123456789"
	// CRC-64/NVME check value for "123456789" is 0xAE8B14860A799888.
	h := newChecksumHasher(common.ChecksumCRC64NVME)
	if h == nil {
		t.Fatal("crc64nvme must be a supported checksum algorithm")
	}
	if _, err := h.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := h.Sum(nil)
	if want := []byte{0xAE, 0x8B, 0x14, 0x86, 0x0A, 0x79, 0x98, 0x88}; !bytes.Equal(got, want) {
		t.Fatalf("crc64nvme(%q) = %x, want %x", payload, got, want)
	}
}

// TestChecksumHeaderNamesCoverEveryAlgorithm proves each supported algorithm has
// a canonical request/response header, so a client can send and read it back.
func TestChecksumHeaderNamesCoverEveryAlgorithm(t *testing.T) {
	for _, algo := range []string{
		common.ChecksumCRC32, common.ChecksumCRC32c, common.ChecksumCRC64NVME,
		common.ChecksumSHA1, common.ChecksumSHA256,
	} {
		if checksumHeaderFor(algo) == "" {
			t.Fatalf("no checksum header for %q", algo)
		}
	}
}

// TestTrailerBlockValidation pins the decoder's trailer parser: every malformed
// shape is refused instead of being silently tolerated.
func TestTrailerBlockValidation(t *testing.T) {
	decode := func(body string) error {
		r := newAWSChunkedReader(strings.NewReader(body), nil, true, 1<<20)
		_, err := io.ReadAll(r)
		return err
	}

	// A well-formed trailer block decodes.
	if err := decode("1\r\na\r\n0\r\nx-amz-checksum-crc32:AAAAAA==\r\n\r\n"); err != nil {
		t.Fatalf("valid trailer rejected: %v", err)
	}
	// No colon.
	if err := decode("1\r\na\r\n0\r\nnocolon\r\n\r\n"); err == nil {
		t.Fatal("trailer without a colon must be refused")
	}
	// Illegal characters in the name.
	if err := decode("1\r\na\r\n0\r\nx amz:1\r\n\r\n"); err == nil {
		t.Fatal("trailer with an illegal name must be refused")
	}
	// More entries than the cap.
	var b strings.Builder
	b.WriteString("1\r\na\r\n0\r\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "x-amz-meta-%d:v\r\n", i)
	}
	b.WriteString("\r\n")
	if err := decode(b.String()); err == nil {
		t.Fatal("too many trailers must be refused")
	}
}

// TestTrailerValuesAreCaptured proves the decoder keeps the trailer block so the
// gateway can verify it.
func TestTrailerValuesAreCaptured(t *testing.T) {
	r := newAWSChunkedReader(strings.NewReader("1\r\na\r\n0\r\nx-amz-checksum-crc32:Zg==\r\n\r\n"), nil, true, 1<<20)
	if _, err := io.ReadAll(r); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := r.TrailerValues()["x-amz-checksum-crc32"]; got != "Zg==" {
		t.Fatalf("captured trailer = %q, want Zg==", got)
	}
}

// TestTrailerUnsupportedAlgorithmIsRefused proves a trailer naming an algorithm we
// cannot compute fails closed rather than being ignored.
func TestTrailerUnsupportedAlgorithmIsRefused(t *testing.T) {
	payload := "abc"
	body := fmt.Sprintf("3\r\n%s\r\n0\r\nx-amz-checksum-bogus:AAAA\r\n\r\n", payload)

	client, server := net.Pipe()
	defer func() { _ = client.Close(); _ = server.Close() }()
	comm := NewS3Communicator(server)

	go func() {
		_, _ = io.WriteString(client, body)
		time.Sleep(100 * time.Millisecond)
	}()
	response := make(chan string, 1)
	go func() {
		buf := make([]byte, 1024)
		_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _ := client.Read(buf)
		response <- string(buf[:n])
	}()

	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1:4440/b/k", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Amz-Content-Sha256", s3StreamingUnsignedTrailer)
	req.Header.Set("X-Amz-Decoded-Content-Length", fmt.Sprint(len(payload)))
	req.ContentLength = int64(len(body))

	derr := comm.decodeStreamingPayload(req)
	if derr == nil {
		t.Fatal("an unsupported trailer algorithm must be refused")
	}
	if !strings.Contains(derr.Error(), "trailer checksum algorithm") {
		t.Fatalf("error must name the algorithm: %v", derr)
	}
	if resp := <-response; !strings.Contains(resp, "400 Bad Request") {
		t.Fatalf("expected a 400 answer, got %q", resp)
	}
}
