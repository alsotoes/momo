package transport

import (
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/storage"
)

// helperPipeComm wires an S3Communicator over net.Pipe for the direct helper
// tests that emit an error response.
func helperPipeComm(t *testing.T) (*S3Communicator, net.Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	comm := NewS3Communicator(server)
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	return comm, client, server
}

// captureDuring runs fn while reading the wire, returning whatever the helper
// wrote (error responses) without blocking on net.Pipe.
func captureDuring(t *testing.T, c net.Conn, fn func()) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() { ch <- readUntilDeadline(t, c, 500*time.Millisecond) }()
	fn()
	return <-ch
}

func buildLockHeaderRequest(t *testing.T, hdrs map[string]string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1/bucket/key", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	return req
}

func requireErrorResponse(t *testing.T, err error, resp, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(resp, wantCode) {
		t.Fatalf("expected %s response, got:\n%s", wantCode, resp)
	}
}

type checksumCase struct {
	name     string
	hdrs     map[string]string
	wantCode string
	wantErr  bool
}

func TestVerifyMultipartChecksum(t *testing.T) {
	data := []byte("worm-bytes")
	sum := sha256.Sum256(data)
	correct := base64.StdEncoding.EncodeToString(sum[:])
	wrong := correct[:len(correct)-2] + "AA"

	cases := []checksumCase{
		{name: "no_checksum"},
		{name: "unsupported_algorithm", hdrs: map[string]string{"X-Amz-Checksum-Algorithm": "MD5"}, wantCode: "InvalidRequest", wantErr: true},
		{name: "mismatch", hdrs: map[string]string{"X-Amz-Checksum-Sha256": wrong}, wantCode: "BadDigest", wantErr: true},
		{name: "match", hdrs: map[string]string{"X-Amz-Checksum-Sha256": correct}},
		{name: "compute_only", hdrs: map[string]string{"X-Amz-Checksum-Algorithm": "SHA256"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runChecksumCase(t, tc, data) })
	}
}

func runChecksumCase(t *testing.T, tc checksumCase, data []byte) {
	comm, client, _ := helperPipeComm(t)
	comm.SetStore(&mockStore{})
	req := buildLockHeaderRequest(t, tc.hdrs)
	if !tc.wantErr {
		if err := comm.verifyMultipartChecksum(req, data, "key"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}
	var verr error
	resp := captureDuring(t, client, func() { verr = comm.verifyMultipartChecksum(req, data, "key") })
	requireErrorResponse(t, verr, resp, tc.wantCode)
}

type lockHeaderRequestCase struct {
	name     string
	hdrs     map[string]string
	version  bool
	wantCode string
	wantErr  bool
	wantReq  bool
}

func TestMultipartLockHeaders(t *testing.T) {
	cases := []lockHeaderRequestCase{
		{name: "no_headers"},
		{name: "invalid_mode", hdrs: map[string]string{"X-Amz-Object-Lock-Mode": "BOGUS"}, wantCode: "InvalidArgument", wantErr: true},
		{name: "unversioned", hdrs: map[string]string{"X-Amz-Object-Lock-Legal-Hold": "ON"}, wantCode: "InvalidRequest", wantErr: true},
		{name: "versioned_hold", hdrs: map[string]string{"X-Amz-Object-Lock-Legal-Hold": "ON"}, version: true, wantReq: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runLockHeaderCase(t, tc) })
	}
}

func runLockHeaderCase(t *testing.T, tc lockHeaderRequestCase) {
	comm, client, _ := helperPipeComm(t)
	comm.SetStore(lockTestStore(t, tc.version))
	req := buildLockHeaderRequest(t, tc.hdrs)
	if tc.wantErr {
		var lerr error
		resp := captureDuring(t, client, func() {
			_, _, lerr = comm.multipartLockHeaders(req, "bucket", "key")
		})
		requireErrorResponse(t, lerr, resp, tc.wantCode)
		return
	}
	st, requested, err := comm.multipartLockHeaders(req, "bucket", "key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requested != tc.wantReq {
		t.Fatalf("requested=%v want %v (state %+v)", requested, tc.wantReq, st)
	}
	if requested != st.LegalHoldOn {
		t.Fatalf("lock state not parsed: %+v", st)
	}
}

// lockTestStore returns a mock store, or a real CASStore with bucket versioning
// enabled when the case needs the versioned path.
func lockTestStore(t *testing.T, versioned bool) storage.Store {
	t.Helper()
	if !versioned {
		return &mockStore{}
	}
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewCASStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetBucketVersioning("bucket", storage.VersioningEnabled); err != nil {
		t.Fatalf("enable versioning: %v", err)
	}
	return store
}
