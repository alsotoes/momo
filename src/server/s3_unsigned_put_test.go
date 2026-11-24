package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alsotoes/momo/src/common"
	"github.com/alsotoes/momo/src/storage"
	"github.com/alsotoes/momo/src/transport"
)

// unsignedTestToken is the cluster token used by the unsigned-PUT tests. // notsecret
const unsignedTestToken = "a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6" // notsecret

// newUnsignedTestStore creates a real CAS store in a temp dir.
func newUnsignedTestStore(t *testing.T) storage.Store {
	t.Helper()
	store, err := storage.NewCASStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// driveUnsignedS3Put mirrors the daemon sequencing for a plain S3 client PUT
// that declares no content hash (e.g. curl -T / UNSIGNED-PAYLOAD). It returns
// the metadata the gateway derived and the error from the ingest path, plus the
// raw bytes the client saw.
func driveUnsignedS3Put(t *testing.T, name, body string, store storage.Store, declaredLen int64) (common.FileMetadata, string, error) {
	t.Helper()

	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	done := make(chan putResult, 1)
	go func() { done <- serveUnsignedS3Put(server, store) }()

	length := strconv.Itoa(len(body))
	if declaredLen >= 0 {
		length = strconv.FormatInt(declaredLen, 10)
	}
	req := "PUT /test-bucket/" + name + " HTTP/1.1\r\n" +
		"Host: 127.0.0.1:4440\r\n" +
		"Authorization: Bearer " + unsignedTestToken + "\r\n" +
		"Content-Length: " + length + "\r\n\r\n" + body
	if _, err := client.Write([]byte(req)); err != nil {
		t.Fatalf("client write: %v", err)
	}

	var sb strings.Builder
	buf := make([]byte, 8192)
	_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		n, err := client.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}

	r := <-done
	return r.meta, sb.String(), r.err
}

// putResult carries what the server side derived for an unsigned PUT.
type putResult struct {
	meta common.FileMetadata
	err  error
}

// serveUnsignedS3Put runs the daemon sequencing for one external-client PUT.
func serveUnsignedS3Put(server net.Conn, store storage.Store) putResult {
	comm := transport.NewS3Communicator(server)
	comm.SetStore(store)
	if _, _, err := comm.HandshakeServer([]byte(common.PadString(unsignedTestToken, common.AuthTokenLength))); err != nil {
		return putResult{err: err}
	}
	// The gateway must not send interim momo framing to an external client, so
	// these two are no-ops and the only response is the S3 one.
	if err := comm.SendReplicationMode(common.ReplicationNone); err != nil {
		return putResult{err: err}
	}
	meta, err := comm.ReceiveMetadata()
	if err != nil {
		return putResult{err: err}
	}
	if err := comm.SendMetadataStatus(transport.MetadataStatusSendPayload); err != nil {
		return putResult{err: err}
	}
	ingestErr := getFile(comm, store, meta.Name, meta.Hash, meta.Size, "")
	if ingestErr == nil {
		persistS3Meta(store, meta.Name, meta)
		_ = comm.SendACK(0)
	}
	return putResult{meta: meta, err: ingestErr}
}

// TestS3UnsignedPutDerivesContentHash proves a PUT with no
// X-Amz-Content-Sha256 is stored under the SHA-256 of its bytes instead of
// being refused for an empty hash (issue #1346). Before the fix the daemon
// rejected it and the client saw a connection reset.
func TestS3UnsignedPutDerivesContentHash(t *testing.T) {
	store := newUnsignedTestStore(t)
	body := "unsigned payload that must reach disk"

	meta, response, err := driveUnsignedS3Put(t, "unsigned.txt", body, store, -1)
	if err != nil {
		t.Fatalf("unsigned PUT must succeed, got %v (response %q)", err, response)
	}

	sum := sha256.Sum256([]byte(body))
	want := hex.EncodeToString(sum[:])
	if meta.Hash != want {
		t.Fatalf("derived hash = %q, want %q", meta.Hash, want)
	}
	if meta.Size != int64(len(body)) {
		t.Fatalf("derived size = %d, want %d", meta.Size, len(body))
	}

	stored, _, getErr := store.Get(meta.Name)
	if getErr != nil {
		t.Fatalf("object not stored: %v", getErr)
	}
	defer func() { _ = stored.Close() }()
	data := new(strings.Builder)
	if _, cErr := io.Copy(data, stored); cErr != nil {
		t.Fatalf("read stored object: %v", cErr)
	}
	if data.String() != body {
		t.Fatalf("stored %q, want %q", data.String(), body)
	}

	// The client must see exactly one response, and it must be an S3 one.
	if strings.Contains(response, "X-Momo-Replication-Mode") {
		t.Fatalf("external client received interim momo framing: %q", response)
	}
	if !strings.Contains(response, "200 OK") || !strings.Contains(response, "ETag: \""+want+"\"") {
		t.Fatalf("expected an S3 200 with the content ETag, got %q", response)
	}
}

// TestS3UnsignedPutRejectsOversizedDeclaredLength proves an over-limit upload is
// refused with an S3 error response before the body is read.
func TestS3UnsignedPutRejectsOversizedDeclaredLength(t *testing.T) {
	store := newUnsignedTestStore(t)

	_, response, err := driveUnsignedS3Put(t, "huge.bin", "", store, common.MaxFileSize+1)
	if err == nil {
		t.Fatal("expected the gateway to refuse an oversized upload")
	}
	if !strings.Contains(response, "413") || !strings.Contains(response, "EntityTooLarge") {
		t.Fatalf("expected an S3 413 EntityTooLarge response, got %q", response)
	}
}
